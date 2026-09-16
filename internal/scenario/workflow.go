package scenario

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// WorkflowStage is one generated stage.
type WorkflowStage struct {
	Name          string
	WhereSpace    string
	Prerequisites []string
	// Classes carries the class the stage deploys (empty for the bases
	// stage), for the seeder's post-promotion release/status work.
	Class string
}

// StandardWorkflowSlug names the workflow of components placed on every
// class. A workflow is deliberately not tied to a component: it is bound at
// change-order creation, and the change order's own space supplies the
// component every stage selector is narrowed by, so one definition governs
// every component that rolls out the same way. What a workflow cannot span
// is a different *shape*: the server refuses to promote past a stage that
// selects no space, so a component placed on fewer classes needs a workflow
// whose stages are exactly its classes. The tool therefore generates one
// workflow per distinct class coverage; most components share this one.
const StandardWorkflowSlug = "standard-rollout"

// WorkflowDef is one generated workflow: its slug and the classes its
// deployment stages cover, in scenario order.
type WorkflowDef struct {
	Slug    string
	Display string
	Classes []Class
}

// workflowDefFor builds the def covering the given class subset.
func (m *Model) workflowDefFor(classes []Class) WorkflowDef {
	if len(classes) == len(m.Scenario.Classes) {
		return WorkflowDef{Slug: StandardWorkflowSlug, Display: "Standard rollout", Classes: classes}
	}
	names := make([]string, 0, len(classes))
	for _, cl := range classes {
		names = append(names, cl.Name)
	}
	// The server's DisplayName regex takes no commas; spaces are fine.
	return WorkflowDef{
		Slug:    "rollout-" + strings.Join(names, "-"),
		Display: "Rollout via " + strings.Join(names, " "),
		Classes: classes,
	}
}

// componentClasses is the scenario classes the component has bases for, in
// scenario order.
func (m *Model) componentClasses(cm *ComponentModel) []Class {
	has := map[string]bool{}
	for _, cb := range cm.ClassBases {
		has[cb.Class] = true
	}
	var out []Class
	for _, cl := range m.Scenario.Classes {
		if has[cl.Name] {
			out = append(out, cl)
		}
	}
	return out
}

// WorkflowFor is the workflow the component's change orders bind.
func (m *Model) WorkflowFor(cm *ComponentModel) WorkflowDef {
	return m.workflowDefFor(m.componentClasses(cm))
}

// WorkflowDefs is the distinct set of workflows the scenario's components
// need, in a deterministic order (the standard one first).
func (m *Model) WorkflowDefs() []WorkflowDef {
	seen := map[string]bool{}
	var defs []WorkflowDef
	for _, cm := range m.Components {
		if !cm.HasManifests {
			continue
		}
		def := m.WorkflowFor(cm)
		if !seen[def.Slug] {
			seen[def.Slug] = true
			defs = append(defs, def)
		}
	}
	sort.Slice(defs, func(i, j int) bool {
		if (defs[i].Slug == StandardWorkflowSlug) != (defs[j].Slug == StandardWorkflowSlug) {
			return defs[i].Slug == StandardWorkflowSlug
		}
		return defs[i].Slug < defs[j].Slug
	})
	return defs
}

// WorkflowStages generates a def's bases-first stage list: one stage holding
// the covered class bases (they are siblings of the root, so the unordered
// wave is safe), then one stage per class's deployments in class order.
// Class bases are never deployed, so only the deployment stages carry
// released/healthy gates; the first deployment stage needs none because the
// stage before it holds only bases.
func (m *Model) WorkflowStages(def WorkflowDef) []WorkflowStage {
	demoName := m.Scenario.Name
	var classes []string
	for _, cl := range def.Classes {
		classes = append(classes, "'"+cl.Name+"'")
	}
	// The component predicate is deliberately absent: since v0.4.6 (Q26) the
	// server appends the change order's own component to every stage selector
	// and refuses definitions that name it themselves.
	stages := []WorkflowStage{{
		Name: "bases",
		WhereSpace: fmt.Sprintf("Labels.DemoName = '%s' AND Labels.Role = 'base' AND Labels.Variant IN (%s)",
			demoName, strings.Join(classes, ", ")),
	}}
	for i, cl := range def.Classes {
		stage := WorkflowStage{
			Name:  cl.Name,
			Class: cl.Name,
			WhereSpace: fmt.Sprintf("Labels.DemoName = '%s' AND Labels.Role = 'deployment' AND Labels.Stage = '%s'",
				demoName, cl.Name),
		}
		if i > 0 {
			stage.Prerequisites = m.stagePrerequisites(cl.Name)
		}
		stages = append(stages, stage)
	}
	return stages
}

// stagePrerequisites is the scenario's configured gate list for a stage, or
// the Released+Healthy default, in the capitalization the ChangeWorkflow
// entity requires (scenario files may write either case).
func (m *Model) stagePrerequisites(stage string) []string {
	reqs, ok := m.Scenario.Workflows.Prerequisites[stage]
	if !ok {
		return []string{"Released", "Healthy"}
	}
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, CanonicalPrerequisite(r))
	}
	return out
}

// CanonicalPrerequisite maps a prerequisite name to the entity's spelling.
func CanonicalPrerequisite(r string) string {
	switch strings.ToLower(r) {
	case "released":
		return "Released"
	case "healthy":
		return "Healthy"
	}
	return r
}

// WorkflowEntity is the shared ChangeWorkflow entity body, in the shape
// `cub changeworkflow create/update --from-stdin` reads (v0.4.15+: the
// workflow is a first-class entity, not a KRM unit).
type WorkflowEntity struct {
	DisplayName string                `json:"DisplayName,omitempty"`
	Stages      []WorkflowEntityStage `json:"Stages"`
	Final       WorkflowEntityFinal   `json:"Final"`
}

type WorkflowEntityStage struct {
	Name          string   `json:"Name"`
	WhereSpace    string   `json:"WhereSpace"`
	Prerequisites []string `json:"Prerequisites,omitempty"`
}

type WorkflowEntityFinal struct {
	Prerequisites []string `json:"Prerequisites,omitempty"`
}

// WorkflowEntityJSON renders one workflow def's ChangeWorkflow entity.
func (m *Model) WorkflowEntityJSON(def WorkflowDef) []byte {
	e := WorkflowEntity{
		DisplayName: def.Display,
		Final:       WorkflowEntityFinal{Prerequisites: m.stagePrerequisites("final")},
	}
	for _, st := range m.WorkflowStages(def) {
		e.Stages = append(e.Stages, WorkflowEntityStage{Name: st.Name, WhereSpace: st.WhereSpace, Prerequisites: st.Prerequisites})
	}
	raw, _ := json.MarshalIndent(e, "", "  ")
	return raw
}
