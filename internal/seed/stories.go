package seed

import (
	"fmt"
	"sort"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/cubclient"
	"github.com/confighub/cub-demo/internal/scenario"
)

// stories seeds the in-flight change orders: make the change in the root
// base, create the change order under the shared workflow matching the
// component's class coverage, then walk it stage by stage to landedThrough —
// releasing and reporting healthy after each deployment stage so the next
// stage's gates hold. Every step is
// idempotent: re-applying the change alters no data, an existing change order
// is reused, an already-promoted stage selects nothing, and re-releases skip.
func (s *Seeder) stories() error {
	if s.Model.Scenario.Workflows.Disabled {
		return nil
	}
	byName := map[string]*scenario.ComponentModel{}
	for _, cm := range s.Model.Components {
		byName[cm.Name] = cm
	}
	seeded := 0
	for _, co := range s.Model.Scenario.Workflows.ChangeOrders {
		cm := byName[co.Component]
		if cm == nil || !cm.HasManifests || (!s.DryRun && s.space(cm.RootSpace) == nil) {
			fmt.Fprintf(s.Out, "  skipping change order %s: component %s not seeded\n", co.Slug, co.Component)
			continue
		}
		if s.DryRun {
			fmt.Fprintf(s.Out, "  would seed change order %s on %s through stage %s\n", co.Slug, co.Component, co.LandedThrough)
			continue
		}
		if err := s.story(cm, co); err != nil {
			return fmt.Errorf("change order %s: %w", co.Slug, err)
		}
		seeded++
	}
	if !s.DryRun {
		fmt.Fprintf(s.Out, "  seeded %d change orders\n", seeded)
	}
	return nil
}

func (s *Seeder) story(cm *scenario.ComponentModel, co scenario.ChangeOrder) error {
	root := s.space(cm.RootSpace)

	// The change precedes the change order: its start/end tags are minted at
	// creation. Re-applying the same function mints nothing.
	where := fmt.Sprintf("SpaceID = '%s' AND Slug = '%s'", root.SpaceID, co.Unit)
	if err := s.Client.InvokeFunctions(where, co.Description, []cubclient.Invocation{{Function: co.Function, Args: co.Args}}); err != nil {
		return fmt.Errorf("change: %w", err)
	}

	coRef := cm.RootSpace + "/" + co.Slug
	def := s.Model.WorkflowFor(cm)
	order, err := s.Client.ChangeOrderBySlug(root.SpaceID, co.Slug)
	if err != nil {
		return err
	}
	// A change order in a terminal state has nothing left to walk, and
	// promoting a completed workflow is an error rather than a no-op.
	switch {
	case order != nil && (order.State == "Released" || order.State == "Aborted" ||
		order.State == "Restored" || order.State == "RestoreReleased"):
		fmt.Fprintf(s.Out, "  %s is %s; nothing to walk\n", coRef, order.State)
		return nil
	case order == nil:
		wf, err := s.workflow(def.Slug)
		if err != nil {
			return err
		}
		// Naming the workflow is all the scope a change order needs: the server
		// heads it for the spaces of its own space's Component.
		order, err = s.Client.CreateChangeOrder(root.SpaceID, goclient.ChangeOrder{
			SpaceID:          root.SpaceID,
			Slug:             co.Slug,
			DisplayName:      co.Slug,
			Description:      co.Description,
			ChangeWorkflowID: &wf.ChangeWorkflowID,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(s.Out, "  created change order %s\n", coRef)
	}

	// Walk the stages in order through landedThrough. The bound workflow's
	// stages are exactly the component's classes (the workflow matches its
	// coverage), so every stage selects spaces. Promotion is the server's;
	// after each deployment stage the promoted spaces are released and
	// reported healthy, which is exactly what the next stage's gates read.
	if co.LandedThrough != "bases" {
		found := false
		for _, cl := range def.Classes {
			if cl.Name == co.LandedThrough {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("landedThrough %q: %s has no %s class base", co.LandedThrough, cm.Name, co.LandedThrough)
		}
	}
	for _, st := range s.Model.WorkflowStages(def) {
		if err := s.Client.PromoteStage(order.ChangeOrderID, st.Name); err != nil {
			return err
		}
		if st.Class != "" {
			if err := s.releaseStage(cm, st.Class, order); err != nil {
				return fmt.Errorf("release stage %s: %w", st.Name, err)
			}
		}
		fmt.Fprintf(s.Out, "  %s promoted through %s\n", coRef, st.Name)
		if st.Name == co.LandedThrough {
			break
		}
	}

	// The blocked story: degrade a few of the last stage's deployments after
	// their release, so the next stage visibly refuses on the healthy gate.
	if co.Degrade > 0 && co.LandedThrough != "bases" {
		if err := s.degradeStage(cm, co); err != nil {
			return err
		}
	}
	return nil
}

// workflow returns the demo's ChangeWorkflow entity with the slug, from the
// home space.
func (s *Seeder) workflow(slug string) (*goclient.ChangeWorkflow, error) {
	home := s.space(s.Model.Home)
	if home == nil {
		return nil, fmt.Errorf("home space %s does not exist; run the home phase first", s.Model.Home)
	}
	workflows, err := s.Client.ListChangeWorkflows(home.SpaceID)
	if err != nil {
		return nil, err
	}
	for _, wf := range workflows {
		if wf.Slug == slug {
			return wf, nil
		}
	}
	return nil, fmt.Errorf("change workflow %s/%s does not exist; run the workflows phase first", s.Model.Home, slug)
}

// releaseStage publishes, for the change order, every deployment of one class
// of the component that has something unreleased, then reports the whole
// class healthy. Status is the Release's, so each new Release starts with
// none and is reported on here, the way the delivery system would after
// syncing it.
func (s *Seeder) releaseStage(cm *scenario.ComponentModel, class string, order *goclient.ChangeOrder) error {
	units, err := s.Client.ListUnitsAll(fmt.Sprintf("%s AND Labels.Component = '%s' AND Labels.Stage = '%s' AND Space.Labels.Role = 'deployment'",
		demoWhere(s.Model), cm.Name, class))
	if err != nil {
		return err
	}
	pending := map[uuid.UUID]bool{}
	for _, u := range units {
		if u.HeadRevisionNum > u.LastReleasedRevisionNum {
			pending[u.SpaceID] = true
		}
	}
	var ids []uuid.UUID
	for id := range pending {
		ids = append(ids, id)
	}
	labels := s.baseLabels(nil)
	err = forEach(s, ids, func(id uuid.UUID) error {
		_, err := s.Client.PublishRelease(id, labels, order)
		return err
	})
	if err != nil {
		return err
	}
	want := map[string]Observation{}
	for _, d := range cm.Deployments {
		if d.Class == class {
			want[d.Space] = Healthy
		}
	}
	_, err = s.report(want)
	return err
}

// degradeStage reports the first co.Degrade deployments (sorted, so it is
// deterministic) of the landedThrough class Degraded.
func (s *Seeder) degradeStage(cm *scenario.ComponentModel, co scenario.ChangeOrder) error {
	var slugs []string
	for _, d := range cm.Deployments {
		if d.Class == co.LandedThrough {
			slugs = append(slugs, d.Space)
		}
	}
	sort.Strings(slugs)
	if co.Degrade < len(slugs) {
		slugs = slugs[:co.Degrade]
	}
	want := map[string]Observation{}
	for _, slug := range slugs {
		want[slug] = Observation{Sync: "Synced", Health: "Degraded", Phase: "Succeeded",
			Message: co.Slug + ": rollout stalled, 1 pod CrashLoopBackOff"}
	}
	if _, err := s.report(want); err != nil {
		return err
	}
	for _, slug := range slugs {
		fmt.Fprintf(s.Out, "  degraded %s (blocks the next stage's healthy gate)\n", slug)
	}
	return nil
}
