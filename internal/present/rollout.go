package present

import (
	"fmt"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// Rollout is a change order together with the space it lives in, whose
// Component is the one every stage of its workflow is narrowed by.
type Rollout struct {
	ChangeOrder *goclient.ChangeOrder
	Space       *goclient.Space // the change order's own space, the base
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
		return &Rollout{ChangeOrder: co, Space: sp}, nil
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
		return &Rollout{ChangeOrder: open[0], Space: sp}, nil
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
	var st *goclient.ChangeWorkflowStage
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
		return nil, fmt.Errorf("change order %s has no stage %q (stages: %s)", r.ChangeOrder.Slug, stage, strings.Join(names, ", "))
	}
	where := st.WhereSpace
	if r.Space.ComponentID != nil {
		// The where parser takes no parentheses; the seeder's selectors are plain
		// conjunctions, so appending is safe.
		where = fmt.Sprintf("%s AND ComponentID = '%s'", where, r.Space.ComponentID)
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

// workflowFor returns the stages the change order is promoted through: its
// own copy of the ChangeWorkflow it was created under, which is what the
// server promotes by.
func (p *Presenter) workflowFor(r *Rollout) (*goclient.ChangeWorkflowSpec, error) {
	wf := r.ChangeOrder.ChangeWorkflow
	if wf == nil || len(wf.Stages) == 0 {
		return nil, fmt.Errorf("change order %s names no ChangeWorkflow, so it has no stages", r.ChangeOrder.Slug)
	}
	return wf, nil
}
