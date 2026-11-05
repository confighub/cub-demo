package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/scenario"
)

// workflowHashAnnotation records the definition a workflow entity was last
// seeded with, so re-runs skip unchanged ones.
const workflowHashAnnotation = "cub-demo.confighub.com/def-hash"

// reviewPolicyAnnotation marks a Component whose ChangeWorkflow policy the
// seeder set from workflows.review.components.
const reviewPolicyAnnotation = "cub-demo.confighub.com/review-policy"

// workflows ensures the scenario's shared ChangeWorkflow entities in the
// home space: one per distinct class coverage, most components sharing the
// standard one. A workflow names no component — change orders bind one at
// creation and supply the component their stage selectors are narrowed by.
// The per-component entities an early seeding created converge away: they
// are deleted unless a change order still in flight is promoted under them
// (those go on a later run, once it completes).
func (s *Seeder) workflows() error {
	if s.Model.Scenario.Workflows.Disabled {
		fmt.Fprintln(s.Out, "  workflows disabled by the scenario")
		return nil
	}
	home := s.space(s.Model.Home)
	if home == nil && !s.DryRun {
		return fmt.Errorf("home space %s does not exist; run the home phase first", s.Model.Home)
	}
	defs := s.Model.WorkflowDefs()
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would ensure %d shared workflow entities in %s\n", len(defs), s.Model.Home)
		return nil
	}
	existing, err := s.Client.ListChangeWorkflows(home.SpaceID)
	if err != nil {
		return err
	}
	if err := s.retireStaleWorkflows(defs, existing); err != nil {
		return err
	}
	bySlug := map[string]*goclient.ChangeWorkflow{}
	for _, wf := range existing {
		bySlug[wf.Slug] = wf
	}

	ensured := 0
	for _, def := range defs {
		changed, err := s.ensureWorkflow(home, def.Slug, s.Model.WorkflowEntityJSON(def), bySlug[def.Slug])
		if err != nil {
			return err
		}
		if changed {
			ensured++
		}
	}
	fmt.Fprintf(s.Out, "  ensured %d shared workflow entities in %s (%d created or updated)\n", len(defs), s.Model.Home, ensured)
	if review := s.Model.Scenario.Workflows.Review; review != nil {
		names, ids, err := s.reviewers(review)
		if err != nil {
			return err
		}
		body := s.Model.ReviewWorkflowJSON(ids)
		if _, err := s.ensureWorkflow(home, scenario.ReviewWorkflowSlug, body, bySlug[scenario.ReviewWorkflowSlug]); err != nil {
			return err
		}
		fmt.Fprintf(s.Out, "  ensured review workflow %s in %s: approval from %s\n",
			scenario.ReviewWorkflowSlug, s.Model.Home, strings.Join(names, ", "))
	}
	return s.reviewPolicy(home)
}

// reviewers resolves the people whose approval the review workflow counts to
// their usernames and user IDs: the ones the scenario lists, or the user
// seeding the demo when it lists none. Naming them is what keeps one agent
// from approving another's change.
func (s *Seeder) reviewers(review *scenario.ReviewWorkflow) (names, ids []string, err error) {
	if len(review.Reviewers) == 0 {
		me, err := s.Client.Me()
		if err != nil {
			return nil, nil, fmt.Errorf("review workflow: %w", err)
		}
		return []string{me.Username}, []string{me.UserID.String()}, nil
	}
	for _, name := range review.Reviewers {
		user, err := s.Client.UserByUsername(name)
		if err != nil {
			return nil, nil, fmt.Errorf("review workflow: reviewer %s: %w", name, err)
		}
		if user == nil {
			return nil, nil, fmt.Errorf("review workflow: reviewer %q is not a user of this organization (workflows.review.reviewers lists usernames)", name)
		}
		names = append(names, user.Username)
		ids = append(ids, user.UserID.String())
	}
	return names, ids, nil
}

// ensureWorkflow creates or updates one ChangeWorkflow entity in the home
// space from its definition, and says whether it wrote anything. The stored
// entity's annotations carry the definition's hash, which skips re-runs.
func (s *Seeder) ensureWorkflow(home *goclient.Space, slug string, body []byte, cur *goclient.ChangeWorkflow) (bool, error) {
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:8])
	if cur != nil && cur.Annotations[workflowHashAnnotation] == hash {
		return false, nil
	}
	// An update starts from the stored entity, so what the definition does
	// not carry (permissions, delete gates) survives it.
	wf := goclient.ChangeWorkflow{SpaceID: home.SpaceID, Slug: slug}
	if cur != nil {
		wf = *cur
		wf.Stages, wf.Final, wf.AttestationPrerequisites = nil, nil, nil
	}
	if err := json.Unmarshal(body, &wf); err != nil {
		return false, fmt.Errorf("workflow %s: %w", slug, err)
	}
	if wf.Labels == nil {
		wf.Labels = map[string]string{}
	}
	wf.Labels[LabelDemoName] = s.Model.Scenario.Name
	if wf.Annotations == nil {
		wf.Annotations = map[string]string{}
	}
	wf.Annotations[workflowHashAnnotation] = hash
	var err error
	if cur == nil {
		_, err = s.Client.CreateChangeWorkflow(home.SpaceID, wf)
	} else {
		err = s.Client.UpdateChangeWorkflow(wf)
	}
	if err != nil {
		return false, fmt.Errorf("workflow %s: %w", slug, err)
	}
	return true, nil
}

// reviewPolicy makes the components workflows.review names require a
// ChangeWorkflow and allow two: the review workflow, for ChangeSets on their
// units, and their rollout workflow, for change orders. The server then
// refuses a ChangeSet on such a component that names no workflow or another,
// so the review is the component's rule and not the good manners of whoever
// opens the ChangeSet. An annotation marks the policy as seeded: a component
// the scenario no longer names loses the policy this set on it, and one a
// play or a person gave the policy keeps it.
func (s *Seeder) reviewPolicy(home *goclient.Space) error {
	workflows, err := s.Client.ListChangeWorkflows(home.SpaceID)
	if err != nil {
		return err
	}
	ids := map[string]uuid.UUID{}
	for _, wf := range workflows {
		ids[wf.Slug] = wf.ChangeWorkflowID
	}
	review, seeded := ids[scenario.ReviewWorkflowSlug]
	if !seeded {
		return nil
	}
	for _, cm := range s.Model.Components {
		component := s.component(cm.Name)
		if component == nil {
			continue
		}
		var patch map[string]any
		switch {
		case s.Model.ReviewRequired(cm.Name):
			rollout, ok := ids[s.Model.WorkflowFor(cm).Slug]
			if !ok {
				return fmt.Errorf("component %s: its rollout workflow %s is not seeded", cm.Name, s.Model.WorkflowFor(cm).Slug)
			}
			allowed := []uuid.UUID{review, rollout}
			if component.ChangeWorkflowRequired && sameIDs(component.AllowedChangeWorkflowIDs, allowed) {
				continue
			}
			patch = map[string]any{"ChangeWorkflowRequired": true, "AllowedChangeWorkflowIDs": allowed,
				"Annotations": map[string]any{reviewPolicyAnnotation: "true"}}
			fmt.Fprintf(s.Out, "  component %s requires a workflow: %s for changesets, %s for change orders\n",
				cm.Name, scenario.ReviewWorkflowSlug, s.Model.WorkflowFor(cm).Slug)
		case component.Annotations[reviewPolicyAnnotation] != "":
			patch = map[string]any{"ChangeWorkflowRequired": false, "AllowedChangeWorkflowIDs": []uuid.UUID{},
				"Annotations": map[string]any{reviewPolicyAnnotation: nil}}
			fmt.Fprintf(s.Out, "  component %s no longer requires a workflow\n", cm.Name)
		default:
			continue
		}
		raw, _ := json.Marshal(patch)
		if err := s.Client.PatchComponent(component.ComponentID, raw); err != nil {
			return fmt.Errorf("component %s workflow policy: %w", cm.Name, err)
		}
		component.ChangeWorkflowRequired, _ = patch["ChangeWorkflowRequired"].(bool)
		component.AllowedChangeWorkflowIDs, _ = patch["AllowedChangeWorkflowIDs"].([]uuid.UUID)
		if component.ChangeWorkflowRequired {
			if component.Annotations == nil {
				component.Annotations = map[string]string{}
			}
			component.Annotations[reviewPolicyAnnotation] = "true"
		} else {
			delete(component.Annotations, reviewPolicyAnnotation)
		}
	}
	return nil
}

// sameIDs says whether two lists hold the same IDs, in any order.
func sameIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range a {
		if !slices.Contains(b, id) {
			return false
		}
	}
	return true
}

// retireStaleWorkflows deletes this demo's workflow entities the scenario no
// longer generates — chiefly the per-component `<component>-workflow`
// entities of the pre-shared design. A per-component workflow whose
// component still holds a change order that may move keeps its entity (that
// rollout is promoted under it) with a note; a later run retires it. A stale
// coverage workflow is kept with a note rather than deleted, because nothing
// cheap proves no rollout still moves under it.
func (s *Seeder) retireStaleWorkflows(defs []scenario.WorkflowDef, existing []*goclient.ChangeWorkflow) error {
	wanted := map[string]bool{}
	for _, def := range defs {
		wanted[def.Slug] = true
	}
	wanted[scenario.ReviewWorkflowSlug] = true
	for _, wf := range existing {
		if wanted[wf.Slug] || wf.Labels[LabelDemoName] != s.Model.Scenario.Name {
			continue
		}
		if !strings.HasSuffix(wf.Slug, "-workflow") {
			fmt.Fprintf(s.Out, "  note: workflow %s carries this demo's label but the scenario no longer generates it; delete it by hand once nothing moves under it\n", wf.Slug)
			continue
		}
		component := strings.TrimSuffix(wf.Slug, "-workflow")
		if open, err := s.componentHasOpenChangeOrder(component); err != nil {
			return err
		} else if open {
			fmt.Fprintf(s.Out, "  keeping legacy workflow %s: a change order of %s is still in flight under it; it retires once that completes\n", wf.Slug, component)
			continue
		}
		if err := s.Client.DeleteChangeWorkflow(wf.SpaceID, wf.ChangeWorkflowID); err != nil {
			return fmt.Errorf("delete legacy workflow %s: %w", wf.Slug, err)
		}
		fmt.Fprintf(s.Out, "  deleted legacy per-component workflow %s (shared workflows govern components now)\n", wf.Slug)
	}
	return nil
}

// componentHasOpenChangeOrder reports whether the component's root space
// holds a change order that may still move under its workflow. Released is
// not closed: a workflow whose final prerequisites name healthy is still in
// flight after its last stage is released.
func (s *Seeder) componentHasOpenChangeOrder(component string) (bool, error) {
	var cm *scenario.ComponentModel
	for _, c := range s.Model.Components {
		if c.Name == component {
			cm = c
		}
	}
	if cm == nil {
		return false, nil
	}
	root := s.space(cm.RootSpace)
	if root == nil {
		return false, nil
	}
	orders, err := s.Client.ListChangeOrders(root.SpaceID, "")
	if err != nil {
		return false, err
	}
	for _, co := range orders {
		switch co.State {
		case "Aborted", "Restored", "RestoreReleased":
		default:
			return true, nil
		}
	}
	return false, nil
}
