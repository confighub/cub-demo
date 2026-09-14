// Package seed creates the expanded model in a ConfigHub org, phase by phase.
// Every phase is idempotent: it starts from an index of what already exists
// (selected by the DemoName label) and only creates what is missing, so "up"
// can be interrupted and re-run.
package seed

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/confighub/cub-demo/internal/cubclient"
	"github.com/confighub/cub-demo/internal/scenario"
)

// LabelDemoName marks every entity the seeder creates; it is the teardown key.
const LabelDemoName = "DemoName"

// Phases in creation order.
var Phases = []string{"home", "clusters", "bases", "protections", "deployments", "releases", "livestatus", "views", "workflows", "stories"}

// Seeder drives the phases against one org.
type Seeder struct {
	Client      *cubclient.Client
	Model       *scenario.Model
	Bundle      *scenario.Bundle
	Concurrency int
	DryRun      bool
	Out         io.Writer

	mu sync.Mutex
	// workerID is resolved by the home phase (or lazily by clusters) and used
	// for every cluster target.
	workerID uuid.UUID
	// spaces is the index of existing demo spaces by slug, loaded at the start
	// of Run and updated as spaces are created.
	spaces map[string]*goclient.Space
}

// Run executes the named phases in canonical order (an empty list means all).
func (s *Seeder) Run(phases []string) error {
	if s.Concurrency <= 0 {
		s.Concurrency = 8
	}
	want := map[string]bool{}
	for _, p := range phases {
		want[p] = true
	}
	if err := s.loadIndex(); err != nil {
		return err
	}
	for _, p := range Phases {
		if len(want) > 0 && !want[p] {
			continue
		}
		fmt.Fprintf(s.Out, "Phase %s:\n", p)
		var err error
		switch p {
		case "home":
			err = s.home()
		case "clusters":
			err = s.clusters()
		case "bases":
			err = s.bases()
		case "deployments":
			err = s.deployments()
		case "protections":
			err = s.protect(protectClassBases)
		case "releases":
			err = s.releases()
		case "livestatus":
			err = s.livestatusPhase()
		case "views":
			err = s.views()
		case "workflows":
			err = s.workflows()
		case "stories":
			err = s.stories()
		}
		if err != nil {
			return fmt.Errorf("phase %s: %w", p, err)
		}
	}
	return nil
}

// loadIndex fetches every existing space of this demo.
func (s *Seeder) loadIndex() error {
	spaces, err := s.Client.ListSpaces(demoWhere(s.Model))
	if err != nil {
		return fmt.Errorf("index existing spaces: %w", err)
	}
	s.spaces = map[string]*goclient.Space{}
	for _, sp := range spaces {
		s.spaces[sp.Slug] = sp
	}
	return nil
}

func demoWhere(m *scenario.Model) string {
	return fmt.Sprintf("Labels.%s = '%s'", LabelDemoName, m.Scenario.Name)
}

func (s *Seeder) space(slug string) *goclient.Space {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spaces[slug]
}

func (s *Seeder) remember(sp *goclient.Space) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spaces[sp.Slug] = sp
}

// ensureSpace creates the space if the index does not have it.
func (s *Seeder) ensureSpace(slug, display string, labels map[string]string) (*goclient.Space, bool, error) {
	if sp := s.space(slug); sp != nil {
		return sp, false, nil
	}
	if s.DryRun {
		return nil, true, nil
	}
	sp, err := s.Client.EnsureSpace(goclient.Space{Slug: slug, DisplayName: display, Labels: labels})
	if err != nil {
		return nil, false, err
	}
	// AllowExists returns whatever owns the slug. Adopting a space that is not
	// this scenario's would put demo entities inside somebody else's space (and
	// teardown would not clean them up), so a foreign owner is a hard error.
	// The create response cannot be trusted for this check: an allow-exists hit
	// merges the REQUEST's labels into the returned entity (confighub bug found
	// 2026-09-01 when this guard silently adopted a foreign space), so ownership
	// is verified with a fresh read.
	fresh, err := s.Client.SpaceBySlug(slug)
	if err != nil {
		return nil, false, err
	}
	if fresh == nil {
		return nil, false, fmt.Errorf("space %q vanished after create", slug)
	}
	if owner := fresh.Labels[LabelDemoName]; owner != s.Model.Scenario.Name {
		return nil, false, fmt.Errorf("space %q already exists and belongs to %s, not scenario %q; pick non-colliding names", slug, describeOwner(owner), s.Model.Scenario.Name)
	}
	sp = fresh
	s.remember(sp)
	return sp, true, nil
}

// forEach runs fn over items on the bounded pool, collecting every error
// rather than stopping at the first, so one failed item does not hide the
// rest and a re-run has less left to do.
func forEach[T any](s *Seeder, items []T, fn func(T) error) error {
	g := new(errgroup.Group)
	g.SetLimit(s.Concurrency)
	var mu sync.Mutex
	var errs []error
	for _, it := range items {
		it := it
		g.Go(func() error {
			if err := fn(it); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait()
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	sort.Strings(msgs)
	if len(msgs) > 5 {
		msgs = append(msgs[:5], fmt.Sprintf("... and %d more", len(msgs)-5))
	}
	return fmt.Errorf("%d of %d failed:\n  %s", len(errs), len(items), joinLines(msgs))
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n  "
		}
		out += l
	}
	return out
}

// baseLabels are on every entity: the demo marker.
func (s *Seeder) baseLabels(more map[string]string) map[string]string {
	labels := map[string]string{LabelDemoName: s.Model.Scenario.Name}
	for k, v := range more {
		labels[k] = v
	}
	return labels
}

func describeOwner(owner string) string {
	if owner == "" {
		return "no demo scenario"
	}
	return "scenario " + strconv.Quote(owner)
}

var uuidNil = uuid.Nil
