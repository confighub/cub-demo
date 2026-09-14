package seed

import (
	"fmt"
	"sort"
	"time"

	"github.com/confighub/sdk/core/livestatus"
	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/cubclient"
	"github.com/confighub/cub-demo/internal/cubexec"
	"github.com/confighub/cub-demo/internal/scenario"
)

// stories seeds the in-flight change orders: make the change in the root
// base, create the change order under the component's workflow, then walk it
// stage by stage to landedThrough — releasing and reporting healthy after
// each deployment stage so the next stage's gates hold. Every step is
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
	existing, err := s.Client.ChangeOrderBySlug(root.SpaceID, co.Slug)
	if err != nil {
		return err
	}
	// A change order in a terminal state has nothing left to walk, and since
	// v0.4.6 promoting a completed workflow is an error rather than a no-op.
	switch {
	case existing != nil && (existing.State == "Released" || existing.State == "Aborted" ||
		existing.State == "Restored" || existing.State == "RestoreReleased"):
		fmt.Fprintf(s.Out, "  %s is %s; nothing to walk\n", coRef, existing.State)
		return nil
	case existing == nil:
		_, err := cubexec.Run("changeorder", "create", "--space", cm.RootSpace, co.Slug,
			"--description", co.Description,
			"--change-workflow", s.Model.Home+"/"+workflowSlug(cm))
		if err != nil {
			return err
		}
		fmt.Fprintf(s.Out, "  created change order %s\n", coRef)
	}

	// Walk the stages in order through landedThrough. Promotion is cub's own
	// logic; after each deployment stage the promoted spaces are released and
	// reported healthy, which is exactly what the next stage's gates read.
	for _, st := range s.Model.WorkflowStages(cm) {
		if _, err := cubexec.Run("variant", "promote", "--change-order", coRef, "--target-stage", st.Name); err != nil {
			return fmt.Errorf("promote stage %s: %w", st.Name, err)
		}
		if st.Class != "" {
			if err := s.releaseStage(cm, st.Class, co); err != nil {
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

// releaseStage publishes and reports healthy every deployment of one class of
// the component, skipping spaces with nothing unreleased.
func (s *Seeder) releaseStage(cm *scenario.ComponentModel, class string, co scenario.ChangeOrder) error {
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
	if err := forEach(s, ids, func(id uuid.UUID) error { return s.Client.PublishRelease(id, labels) }); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	healthy := livestatus.Status{Source: liveStatusSource, SyncStatus: "Synced", HealthStatus: "Healthy",
		OperationPhase: "Succeeded", ObservedAt: now}
	patch, err := statusPatch(healthy)
	if err != nil {
		return err
	}
	whereSp := fmt.Sprintf("%s AND Labels.Component = '%s' AND Labels.Stage = '%s' AND Labels.Role = 'deployment'",
		demoWhere(s.Model), cm.Name, class)
	return s.Client.BulkPatchSpaces(whereSp, patch)
}

// degradeStage paints the first co.Degrade deployments (sorted, so it is
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
	now := time.Now().UTC().Format(time.RFC3339)
	for _, slug := range slugs {
		sp := s.space(slug)
		if sp == nil {
			continue
		}
		st := livestatus.Status{Source: liveStatusSource, SyncStatus: "Synced", HealthStatus: "Degraded",
			OperationPhase: "Succeeded", Message: co.Slug + ": rollout stalled, 1 pod CrashLoopBackOff", ObservedAt: now}
		patch, err := statusPatch(st)
		if err != nil {
			return err
		}
		if err := s.Client.PatchSpace(sp.SpaceID, patch); err != nil {
			return err
		}
		fmt.Fprintf(s.Out, "  degraded %s (blocks the next stage's healthy gate)\n", slug)
	}
	return nil
}
