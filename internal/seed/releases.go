package seed

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/cubclient"
	"github.com/confighub/cub-demo/internal/scenario"
)

// releases publishes a release per deployment space, then applies the story's
// skews so a deliberate few spaces show unreleased changes on top of their
// release. Two space sets are exempt from publishing:
//
//   - the story's unreleased set is never published, so those spaces read
//     "never released";
//   - skewed spaces are published only once, or a re-run would fold the skew
//     into a fresh release and erase the story.
func (s *Seeder) releases() error {
	unreleased := map[string]bool{}
	for _, slug := range s.Model.Story.Unreleased {
		unreleased[slug] = true
	}
	skewed := map[string]bool{}
	for _, sk := range s.Model.Scenario.Story.Skews {
		for _, space := range s.skewSpaces(sk) {
			skewed[space] = true
		}
	}

	// One org-wide unit query finds every deployment space with something to
	// publish; releases bundle whole spaces, so units roll up to spaces.
	units, err := s.Client.ListUnitsAll(demoWhere(s.Model) + " AND Space.Labels.Role = 'deployment'")
	if err != nil {
		return fmt.Errorf("list units: %w", err)
	}
	type need struct {
		everReleased bool
		pending      bool
	}
	bySpace := map[uuid.UUID]*need{}
	for _, u := range units {
		n := bySpace[u.SpaceID]
		if n == nil {
			n = &need{}
			bySpace[u.SpaceID] = n
		}
		if u.LastReleasedRevisionNum > 0 {
			n.everReleased = true
		}
		if u.HeadRevisionNum > u.LastReleasedRevisionNum {
			n.pending = true
		}
	}
	slugByID := map[uuid.UUID]string{}
	s.mu.Lock()
	for slug, sp := range s.spaces {
		slugByID[sp.SpaceID] = slug
	}
	s.mu.Unlock()

	var publish []uuid.UUID
	for id, n := range bySpace {
		slug := slugByID[id]
		if !n.pending || unreleased[slug] {
			continue
		}
		if skewed[slug] && n.everReleased {
			continue
		}
		publish = append(publish, id)
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would publish %d releases (%d spaces kept unreleased, %d skewed)\n", len(publish), len(unreleased), len(skewed))
		return nil
	}
	labels := s.baseLabels(nil)
	err = forEach(s, publish, func(id uuid.UUID) error {
		if err := s.Client.PublishRelease(id, labels); err != nil {
			return fmt.Errorf("release %s: %w", slugByID[id], err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "  published %d releases (%d spaces deliberately unreleased)\n", len(publish), len(unreleased))

	// Skews, after the releases they are supposed to sit on top of. Same
	// value, same data: a re-applied skew changes nothing and mints no
	// revision, so this is naturally idempotent.
	for _, sk := range s.Model.Scenario.Story.Skews {
		spaces := s.skewSpaces(sk)
		if len(spaces) == 0 {
			continue
		}
		where := fmt.Sprintf("%s AND Labels.Component = '%s' AND Slug = '%s' AND Space.Labels.Role = 'deployment'%s",
			demoWhere(s.Model), sk.Component, sk.Unit, selectorWhere(sk.Where))
		desc := fmt.Sprintf("skew %s/%s: %s %s", sk.Component, sk.Unit, sk.Function, strings.Join(sk.Args, " "))
		err := s.Client.InvokeFunctions(where, desc, []cubclient.Invocation{{Function: sk.Function, Args: sk.Args}})
		if err != nil {
			return fmt.Errorf("skew %s: %w", sk.Component, err)
		}
		fmt.Fprintf(s.Out, "  skewed %s/%s in %d spaces\n", sk.Component, sk.Unit, len(spaces))
	}
	return nil
}

// skewSpaces resolves a skew to the existing deployment spaces it touches.
func (s *Seeder) skewSpaces(sk scenario.Skew) []string {
	var out []string
	for _, cm := range s.Model.Components {
		if cm.Name != sk.Component || !cm.HasManifests {
			continue
		}
		for _, d := range cm.Deployments {
			if matchesSelector(sk.Where, d.Cluster) && (s.DryRun || s.space(d.Space) != nil) {
				out = append(out, d.Space)
			}
		}
	}
	return out
}

// selectorWhere renders a cluster selector as unit-label conditions, using the
// labels the deployments phase stamped on every deployment unit.
func selectorWhere(sel scenario.Selector) string {
	var parts []string
	add := func(label string, values []string) {
		if len(values) == 0 {
			return
		}
		quoted := make([]string, 0, len(values))
		for _, v := range values {
			quoted = append(quoted, "'"+v+"'")
		}
		parts = append(parts, fmt.Sprintf(" AND Labels.%s IN (%s)", label, strings.Join(quoted, ", ")))
	}
	add("Stage", sel.Classes)
	add("Region", sel.Regions)
	add("Department", sel.Departments)
	return strings.Join(parts, "")
}

// matchesSelector is the client-side twin of selectorWhere.
func matchesSelector(sel scenario.Selector, c *scenario.Cluster) bool {
	match := func(values []string, v string) bool {
		if len(values) == 0 {
			return true
		}
		for _, x := range values {
			if x == v {
				return true
			}
		}
		return false
	}
	dept := c.Department
	if c.Shared() {
		dept = "shared"
	}
	return match(sel.Classes, c.Class) && match(sel.Regions, c.Region) && match(sel.Departments, dept)
}
