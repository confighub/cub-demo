package present

import (
	"fmt"
	"strings"

	"encoding/json"
	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/cubexec"
)

// Rollout is a change order together with what a stage needs to be resolved
// through it: the component (its space's label) and the workflow it names.
type Rollout struct {
	ChangeOrder *goclient.ChangeOrder
	Space       *goclient.Space // the change order's own space, the base
	Component   string
}

// Workflow is the part of a ChangeWorkflow entity a stage lookup reads
// (v0.4.15+: workflows are entities, resolved from the change order's
// ChangeWorkflowID rather than a unit annotation).
type Workflow struct {
	Slug   string          `json:"Slug"`
	Stages []WorkflowStage `json:"Stages"`
}

// WorkflowStage is one stage: its name and the selector over spaces.
type WorkflowStage struct {
	Name          string   `json:"Name"`
	WhereSpace    string   `json:"WhereSpace"`
	Prerequisites []string `json:"Prerequisites"`
}

// terminal is the states a change order does not come back from. Released is
// not one of them: a workflow whose final prerequisites name healthy is still
// in flight after its last stage is released, waiting on the delivery system.
func terminal(state string) bool {
	switch state {
	case "Aborted", "Restored", "RestoreReleased":
		return true
	}
	return false
}

// pickOpen chooses the change order in flight among a component's. Non-terminal
// ones are candidates; if that leaves several, the Released ones step aside for
// the newer change that is actually moving. Still several is an error.
func pickOpen(all []*goclient.ChangeOrder) []*goclient.ChangeOrder {
	var open []*goclient.ChangeOrder
	for _, co := range all {
		if !terminal(co.State) {
			open = append(open, co)
		}
	}
	if len(open) > 1 {
		var moving []*goclient.ChangeOrder
		for _, co := range open {
			if co.State != "Released" {
				moving = append(moving, co)
			}
		}
		if len(moving) > 0 {
			open = moving
		}
	}
	return open
}

// FindRollout returns the change order a stage is resolved through. Named as
// space/slug it is looked up there; otherwise it is the component's single
// non-terminal change order, so a move never has to carry a slug that changes
// between runs. None or several is an error rather than a guess.
func (p *Presenter) FindRollout(component, ref string) (*Rollout, error) {
	if ref != "" {
		spaceSlug, slug, ok := strings.Cut(ref, "/")
		if !ok {
			return nil, fmt.Errorf("--change-order %q: want space/slug", ref)
		}
		sp, err := p.Client.SpaceBySlug(spaceSlug)
		if err != nil {
			return nil, err
		}
		if sp == nil {
			return nil, fmt.Errorf("space %q not found", spaceSlug)
		}
		co, err := p.Client.ChangeOrderBySlug(sp.SpaceID, slug)
		if err != nil {
			return nil, err
		}
		if co == nil {
			return nil, fmt.Errorf("change order %s not found", ref)
		}
		return &Rollout{ChangeOrder: co, Space: sp, Component: sp.Labels["Component"]}, nil
	}
	if component == "" {
		return nil, fmt.Errorf("a stage is a workflow fact: name --component (its change order in flight is used) or --change-order space/slug")
	}
	cm, err := p.component(component)
	if err != nil {
		return nil, err
	}
	sp, err := p.Client.SpaceBySlug(cm.RootSpace)
	if err != nil {
		return nil, err
	}
	if sp == nil {
		return nil, fmt.Errorf("%s is not seeded: base space %s not found", component, cm.RootSpace)
	}
	all, err := p.Client.ListChangeOrders(sp.SpaceID, "")
	if err != nil {
		return nil, err
	}
	open := pickOpen(all)
	switch len(open) {
	case 0:
		return nil, fmt.Errorf("%s has no change order in flight (a play with a changeorder step starts one)", component)
	case 1:
		return &Rollout{ChangeOrder: open[0], Space: sp, Component: sp.Labels["Component"]}, nil
	}
	names := make([]string, 0, len(open))
	for _, co := range open {
		names = append(names, cm.RootSpace+"/"+co.Slug)
	}
	return nil, fmt.Errorf("%s has %d change orders in flight; name one with --change-order: %s", component, len(open), strings.Join(names, ", "))
}

// StageSpaces resolves a stage the way bulk promote does: the workflow the
// change order names, the stage's whereSpace with the change order's component
// appended, intersected with the change order's in-scope list.
func (p *Presenter) StageSpaces(r *Rollout, stage string) (map[uuid.UUID]bool, error) {
	wf, err := p.workflowFor(r)
	if err != nil {
		return nil, err
	}
	var st *WorkflowStage
	for i := range wf.Stages {
		if wf.Stages[i].Name == stage {
			st = &wf.Stages[i]
		}
	}
	if st == nil {
		names := make([]string, 0, len(wf.Stages))
		for _, s := range wf.Stages {
			names = append(names, s.Name)
		}
		return nil, fmt.Errorf("workflow %s has no stage %q (stages: %s)", wf.Slug, stage, strings.Join(names, ", "))
	}
	where := st.WhereSpace
	if r.Component != "" {
		// The where parser takes no parentheses; the seeder's selectors are plain
		// conjunctions, so appending is safe.
		where = fmt.Sprintf("%s AND Labels.Component = '%s'", where, r.Component)
	}
	spaces, err := p.Client.ListSpaces(where)
	if err != nil {
		return nil, fmt.Errorf("stage %s: %w", stage, err)
	}
	inScope := map[uuid.UUID]bool{}
	for _, id := range r.ChangeOrder.InScopeSpaceIDs {
		inScope[id] = true
	}
	members := map[uuid.UUID]bool{}
	for _, sp := range spaces {
		if len(inScope) > 0 && !inScope[sp.SpaceID] {
			continue
		}
		members[sp.SpaceID] = true
	}
	return members, nil
}

// workflowFor resolves the ChangeWorkflow entity the change order is promoted
// under: the pinned SDK has no client for the entity, so the change order and
// the workflow are read through cub. The change order's own -o json carries
// ChangeWorkflowID.
func (p *Presenter) workflowFor(r *Rollout) (*Workflow, error) {
	coJSON, err := cubexec.Run("changeorder", "get", r.ChangeOrder.Slug, "--space", r.Space.Slug, "--quiet", "-o", "json")
	if err != nil {
		return nil, err
	}
	id := extractChangeWorkflowID(coJSON)
	if id == "" {
		return nil, fmt.Errorf("change order %s names no ChangeWorkflow, so it has no stages", r.ChangeOrder.Slug)
	}
	wfJSON, err := cubexec.Run("changeworkflow", "get", id, "--space", "*", "--quiet", "-o", "json")
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		ChangeWorkflow *Workflow `json:"ChangeWorkflow"`
	}
	if jerr := json.Unmarshal([]byte(wfJSON), &wrapped); jerr == nil && wrapped.ChangeWorkflow != nil && len(wrapped.ChangeWorkflow.Stages) > 0 {
		return wrapped.ChangeWorkflow, nil
	}
	var wf Workflow
	if err := json.Unmarshal([]byte(wfJSON), &wf); err != nil {
		return nil, fmt.Errorf("ChangeWorkflow %s: %w", id, err)
	}
	if len(wf.Stages) == 0 {
		return nil, fmt.Errorf("ChangeWorkflow %s has no stages", id)
	}
	return &wf, nil
}

// extractChangeWorkflowID digs ChangeWorkflowID out of a change order's -o
// json, whether the entity is at the top level or wrapped.
func extractChangeWorkflowID(coJSON string) string {
	var wrapped struct {
		ChangeOrder struct {
			ChangeWorkflowID string `json:"ChangeWorkflowID"`
		} `json:"ChangeOrder"`
		ChangeWorkflowID string `json:"ChangeWorkflowID"`
	}
	if err := json.Unmarshal([]byte(coJSON), &wrapped); err != nil {
		return ""
	}
	if wrapped.ChangeOrder.ChangeWorkflowID != "" {
		return wrapped.ChangeOrder.ChangeWorkflowID
	}
	return wrapped.ChangeWorkflowID
}
