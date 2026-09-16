package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/confighub/cub-demo/internal/cubexec"
	"github.com/confighub/cub-demo/internal/scenario"
)

// workflowHashAnnotation records the definition a workflow entity was last
// seeded with, so re-runs skip unchanged ones.
const workflowHashAnnotation = "cub-demo.confighub.com/def-hash"

// workflows ensures the scenario's shared ChangeWorkflow entities in the
// home space: one per distinct class coverage, most components sharing the
// standard one (v0.4.15+: workflows are first-class entities; the SDK this
// plugin pins has no client for them, so creation shells out through cub).
// A workflow names no component — change orders bind one at creation and
// supply the component their stage selectors are narrowed by. Leftovers of
// earlier designs converge away: pre-entity KRM workflow units are deleted,
// and the per-component entities a pre-shared seeding created are deleted
// unless a change order still in flight is promoted under them (deleting
// those would strand the rollout; they go on a later run, once it
// completes).
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

	// One-time migration: drop the KRM workflow units a pre-v0.4.15 seeding
	// created.
	units, err := s.Client.ListUnits(home.SpaceID, "")
	if err != nil {
		return err
	}
	for _, u := range units {
		if strings.HasSuffix(u.Slug, "-workflow") && u.ToolchainType == "AppConfig/YAML" {
			if err := s.Client.DeleteUnit(home.SpaceID, u.UnitID); err != nil {
				return fmt.Errorf("delete legacy workflow unit %s: %w", u.Slug, err)
			}
			fmt.Fprintf(s.Out, "  deleted legacy workflow unit %s (workflows are entities since v0.4.15)\n", u.Slug)
		}
	}
	if err := s.retireStaleWorkflows(defs); err != nil {
		return err
	}

	ensured := 0
	for _, def := range defs {
		body := s.Model.WorkflowEntityJSON(def)
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:8])
		existing, err := cubexec.Run("changeworkflow", "get", def.Slug, "--space", s.Model.Home, "--quiet", "-o", "json")
		verb := "update"
		if err != nil {
			if !strings.Contains(existing, "not found") {
				return fmt.Errorf("workflow %s: %w", def.Slug, err)
			}
			verb = "create"
		} else if workflowAnnotations(existing)[workflowHashAnnotation] == hash {
			continue
		}
		args := []string{"changeworkflow", verb, "--space", s.Model.Home, def.Slug, "--from-stdin",
			"--label", LabelDemoName + "=" + s.Model.Scenario.Name,
			"--annotation", workflowHashAnnotation + "=" + hash}
		if _, err := cubexec.RunWithStdin(string(body), args...); err != nil {
			return fmt.Errorf("workflow %s: %w", def.Slug, err)
		}
		ensured++
	}
	fmt.Fprintf(s.Out, "  ensured %d shared workflow entities in %s (%d created or updated)\n", len(defs), s.Model.Home, ensured)
	return nil
}

// workflowAnnotations digs Annotations out of a changeworkflow's -o json,
// whether the entity is at the top level or wrapped.
func workflowAnnotations(raw string) map[string]string {
	var wrapped struct {
		ChangeWorkflow struct {
			Annotations map[string]string `json:"Annotations"`
		} `json:"ChangeWorkflow"`
		Annotations map[string]string `json:"Annotations"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err != nil {
		return nil
	}
	if len(wrapped.ChangeWorkflow.Annotations) > 0 {
		return wrapped.ChangeWorkflow.Annotations
	}
	return wrapped.Annotations
}

// retireStaleWorkflows deletes this demo's workflow entities the scenario no
// longer generates — chiefly the per-component `<component>-workflow`
// entities of the pre-shared design. A per-component workflow whose
// component still holds a change order that may move keeps its entity (that
// rollout is promoted under it) with a note; a later run retires it. A stale
// coverage workflow is kept with a note rather than deleted, because nothing
// cheap proves no rollout still moves under it.
func (s *Seeder) retireStaleWorkflows(defs []scenario.WorkflowDef) error {
	raw, err := cubexec.Run("changeworkflow", "list", "--space", s.Model.Home, "--quiet", "-o", "json")
	if err != nil {
		// A home space with no workflows yet may list nothing; only a real
		// failure matters, and the ensure step surfaces those.
		return nil
	}
	// Each list element wraps the entity ({"ChangeWorkflow": {...}}); accept
	// the flat shape too.
	type wfFields struct {
		Slug   string            `json:"Slug"`
		Labels map[string]string `json:"Labels"`
	}
	var wrapped []struct {
		ChangeWorkflow wfFields `json:"ChangeWorkflow"`
		wfFields
	}
	if jerr := json.Unmarshal([]byte(raw), &wrapped); jerr != nil {
		return nil
	}
	listed := make([]wfFields, 0, len(wrapped))
	for _, w := range wrapped {
		if w.ChangeWorkflow.Slug != "" {
			listed = append(listed, w.ChangeWorkflow)
		} else {
			listed = append(listed, w.wfFields)
		}
	}
	wanted := map[string]bool{}
	for _, def := range defs {
		wanted[def.Slug] = true
	}
	for _, wf := range listed {
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
		if _, err := cubexec.Run("changeworkflow", "delete", wf.Slug, "--space", s.Model.Home); err != nil {
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
