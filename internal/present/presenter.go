// Package present runs a scenario's plays against a seeded org: the runner
// for the named step sequences a scenario declares, and the generic
// primitives (steps.go) those steps may call. The actors a demo impersonates
// — a CI pipeline, a delivery bot reporting live status, an incident and its
// repair — are plays the scenario composes from the primitives; their names
// and beats live in the scenario file, never here.
//
// Every primitive reads its nouns from the scenario the caller selected: a
// component's base space, its workflow, its deployments. The one
// convention relied on is the seeder's: a component has at most one change
// order in flight, so "the rollout for catalog-api" is a lookup, not a flag.
package present

import (
	"fmt"
	"io"
	"sort"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/cub-demo/internal/cubclient"
	"github.com/confighub/cub-demo/internal/scenario"
	"github.com/confighub/cub-demo/internal/seed"
)

// DemoEnv carries the demo's name to verbs a play runs, so a child "cub
// demo" command picks the same demo even in an org that holds several.
const DemoEnv = "CUB_DEMO_DEMO"

// Presenter is the shared state of the verbs.
type Presenter struct {
	Client *cubclient.Client
	Model  *scenario.Model
	Out    io.Writer
	DryRun bool
	// Context is the cub context the cub commands of a play run in; empty is
	// the session's. An agent runs its steps in a context of its own.
	Context string
	// ChangeSet, when set, is the ChangeSet every change-making step writes
	// inside, as space/slug, and Clearance the guarded reasons those writes
	// are cleared for, each as KEY=VALUE. Both are the agent runner's.
	ChangeSet string
	Clearance []string
	// Vars seeds the variables a step list starts with, as a primitive would
	// have exported them: the agent runner sets Cycle to the agent's cycle
	// number, so a change can vary from one cycle to the next.
	Vars map[string]string
}

// changeFlags are the flags a change-making cub command carries when the
// presenter writes inside a ChangeSet or under a clearance.
func (p *Presenter) changeFlags() []string {
	var flags []string
	if p.ChangeSet != "" {
		flags = append(flags, "--changeset", p.ChangeSet)
	}
	for _, c := range p.Clearance {
		flags = append(flags, "--clearance", c)
	}
	return flags
}

// Selector picks deployment spaces. Component names a Component entity and
// Variants is a label selector. Stage is a workflow fact and needs a change
// order to resolve — found from Component, or named with ChangeOrder as
// space/slug — because a ChangeWorkflow is not bound to a component: the
// component is the change order's, from the space it lives in. Selectors intersect; an empty selector
// is every deployment space of the scenario.
type Selector struct {
	Component   string
	Variants    []string
	Stage       string
	ChangeOrder string
}

func (p *Presenter) component(name string) (*scenario.ComponentModel, error) {
	for _, cm := range p.Model.Components {
		if cm.Name == name {
			return cm, nil
		}
	}
	return nil, fmt.Errorf("scenario %s has no component %q", p.Model.Name(), name)
}

func demoWhere(m *scenario.Model) string {
	return fmt.Sprintf("Labels.%s = '%s'", seed.LabelDemoName, m.Scenario.Name)
}

func quoteList(items []string) string {
	q := make([]string, 0, len(items))
	for _, it := range items {
		q = append(q, "'"+it+"'")
	}
	return strings.Join(q, ", ")
}

// Resolve returns the deployment spaces the selector picks, sorted by slug,
// and the rollout it resolved a stage through, if it did.
func (p *Presenter) Resolve(sel Selector) ([]*goclient.Space, *Rollout, error) {
	where := demoWhere(p.Model) + " AND Labels.Role = 'deployment'"
	if sel.Component != "" {
		if _, err := p.component(sel.Component); err != nil {
			return nil, nil, err
		}
		where += fmt.Sprintf(" AND Component.Slug = '%s'", sel.Component)
	}
	if len(sel.Variants) > 0 {
		where += fmt.Sprintf(" AND Labels.Variant IN (%s)", quoteList(sel.Variants))
	}
	spaces, err := p.Client.ListSpaces(where)
	if err != nil {
		return nil, nil, err
	}
	var rollout *Rollout
	if sel.Stage != "" {
		rollout, err = p.FindRollout(sel.Component, sel.ChangeOrder)
		if err != nil {
			return nil, nil, err
		}
		members, err := p.StageSpaces(rollout, sel.Stage)
		if err != nil {
			return nil, nil, err
		}
		keep := spaces[:0]
		for _, sp := range spaces {
			if members[sp.SpaceID] {
				keep = append(keep, sp)
			}
		}
		spaces = keep
	}
	sort.Slice(spaces, func(i, j int) bool { return spaces[i].Slug < spaces[j].Slug })
	return spaces, rollout, nil
}
