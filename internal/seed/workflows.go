package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/cub-demo/internal/scenario"
)

// workflowHashAnnotation records the definition a workflow entity was last
// seeded with, so re-runs skip unchanged ones.
const workflowHashAnnotation = "cub-demo.confighub.com/def-hash"

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
		body := s.Model.WorkflowEntityJSON(def)
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:8])
		cur := bySlug[def.Slug]
		if cur != nil && cur.Annotations[workflowHashAnnotation] == hash {
			continue
		}
		// An update starts from the stored entity, so what the definition does
		// not carry (permissions, delete gates) survives it.
		wf := goclient.ChangeWorkflow{SpaceID: home.SpaceID, Slug: def.Slug}
		if cur != nil {
			wf = *cur
			wf.Stages, wf.Final = nil, nil
		}
		if err := json.Unmarshal(body, &wf); err != nil {
			return fmt.Errorf("workflow %s: %w", def.Slug, err)
		}
		if wf.Labels == nil {
			wf.Labels = map[string]string{}
		}
		wf.Labels[LabelDemoName] = s.Model.Scenario.Name
		if wf.Annotations == nil {
			wf.Annotations = map[string]string{}
		}
		wf.Annotations[workflowHashAnnotation] = hash
		if cur == nil {
			_, err = s.Client.CreateChangeWorkflow(home.SpaceID, wf)
		} else {
			err = s.Client.UpdateChangeWorkflow(wf)
		}
		if err != nil {
			return fmt.Errorf("workflow %s: %w", def.Slug, err)
		}
		ensured++
	}
	fmt.Fprintf(s.Out, "  ensured %d shared workflow entities in %s (%d created or updated)\n", len(defs), s.Model.Home, ensured)
	return nil
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
