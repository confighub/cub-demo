package scenario

import (
	"encoding/json"
	"fmt"
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

// WorkflowStages generates the bases-first stage list for a component: one
// stage holding every class base (they are siblings of the root, so the
// unordered wave is safe), then one stage per class's deployments in class
// order. Class bases are never deployed, so only the deployment stages carry
// released/healthy gates; the first deployment stage needs none because the
// stage before it holds only bases.
func (m *Model) WorkflowStages(cm *ComponentModel) []WorkflowStage {
	demoName := m.Scenario.Name
	var classes []string
	for _, cb := range cm.ClassBases {
		classes = append(classes, "'"+cb.Class+"'")
	}
	// The component predicate is deliberately absent: since v0.4.6 (Q26) the
	// server appends the change order's own component to every stage selector
	// and refuses definitions that name it themselves.
	stages := []WorkflowStage{{
		Name: "bases",
		WhereSpace: fmt.Sprintf("Labels.DemoName = '%s' AND Labels.Role = 'base' AND Labels.Variant IN (%s)",
			demoName, strings.Join(classes, ", ")),
	}}
	for i, cb := range cm.ClassBases {
		stage := WorkflowStage{
			Name:  cb.Class,
			Class: cb.Class,
			WhereSpace: fmt.Sprintf("Labels.DemoName = '%s' AND Labels.Role = 'deployment' AND Labels.Stage = '%s'",
				demoName, cb.Class),
		}
		if i > 0 {
			stage.Prerequisites = m.stagePrerequisites(cb.Class)
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

// WorkflowEntity is the ChangeWorkflow entity body for one component, in the
// shape `cub changeworkflow create/update --from-stdin` reads (v0.4.15+: the
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

// WorkflowEntityJSON renders the component's ChangeWorkflow entity.
func (m *Model) WorkflowEntityJSON(cm *ComponentModel) []byte {
	e := WorkflowEntity{
		DisplayName: cm.Name + " rollout line",
		Final:       WorkflowEntityFinal{Prerequisites: m.stagePrerequisites("final")},
	}
	for _, st := range m.WorkflowStages(cm) {
		e.Stages = append(e.Stages, WorkflowEntityStage{Name: st.Name, WhereSpace: st.WhereSpace, Prerequisites: st.Prerequisites})
	}
	raw, _ := json.MarshalIndent(e, "", "  ")
	return raw
}
