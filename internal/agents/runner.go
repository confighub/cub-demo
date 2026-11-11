package agents

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/cubexec"
	"github.com/confighub/cub-demo/internal/present"
	"github.com/confighub/cub-demo/internal/scenario"
)

// Runner drives the agents: each acts in turn whenever its interval is up,
// and what each does next is decided by where its current change stands
// (the phase), re-read from the org rather than remembered, so a runner
// started over an org with changes in flight picks them up.
type Runner struct {
	P       *present.Presenter
	Manager *Manager
	// Reviewer is who an agent asks to review a ChangeSet the workflow
	// refuses to close; empty asks nobody.
	Reviewer string
	Out      io.Writer
	Tick     time.Duration
}

type phase int

const (
	// idle: nothing in flight; the next move opens a ChangeSet.
	idle phase = iota
	// closing: a ChangeSet holds the change; the next move tries to close it.
	closing
	// queued: the ChangeSet is closed, and the rollout waits for the
	// component's change order in flight to complete before it starts.
	queued
	// rolling: a change order carries the change; the next move promotes it.
	rolling
)

// agentState is one agent's standing.
type agentState struct {
	def   scenario.Agent
	ctx   string
	base  string // the component's root base; empty for a tick agent
	every time.Duration
	next  time.Time

	phase           phase
	changeSet       string
	reviewRequested bool
	cycle           int
}

// Run acts for every agent whose interval is up, forever, or once each with
// once.
func (r *Runner) Run(once bool) error {
	states, err := r.init()
	if err != nil {
		return err
	}
	if len(states) == 0 {
		return fmt.Errorf("scenario %s declares no agents", r.P.Model.Name())
	}
	for {
		now := time.Now()
		for _, st := range states {
			if now.Before(st.next) {
				continue
			}
			if err := r.step(st); err != nil {
				r.logf(st, "error: %v", err)
			}
			st.next = time.Now().Add(st.every)
		}
		if once {
			return nil
		}
		time.Sleep(r.Tick)
	}
}

// init reads where each agent stands: an open ChangeSet of its own is
// adopted, and the cycle count continues from the ChangeSets it left behind.
func (r *Runner) init() ([]*agentState, error) {
	var states []*agentState
	for _, a := range r.P.Model.Scenario.Agents {
		every, err := time.ParseDuration(a.Every)
		if err != nil {
			return nil, fmt.Errorf("agent %s: %w", a.Name, err)
		}
		st := &agentState{def: a, ctx: r.Manager.ContextName(a), every: every}
		if a.Component != "" {
			for _, cm := range r.P.Model.Components {
				if cm.Name == a.Component {
					st.base = cm.RootSpace
				}
			}
			if st.base == "" {
				return nil, fmt.Errorf("agent %s: component %s is not in the model", a.Name, a.Component)
			}
			if err := r.adopt(st); err != nil {
				return nil, err
			}
		}
		states = append(states, st)
	}
	return states, nil
}

// adopt finds where the agent's work stands in its base: its ChangeSets and
// change orders continue the cycle numbering, an open ChangeSet is the change
// to close next, and a change order still moving is the rollout to continue.
func (r *Runner) adopt(st *agentState) error {
	prefix := st.def.Name + "-"
	cycleOf := func(slug string) int {
		if !strings.HasPrefix(slug, prefix) {
			return 0
		}
		n, err := strconv.Atoi(strings.TrimPrefix(slug, prefix))
		if err != nil {
			return 0
		}
		return n
	}
	changeSets, err := listJSON(st.ctx, "ChangeSet", "changeset", "list", "--space", st.base)
	if err != nil {
		return fmt.Errorf("agent %s: %w", st.def.Name, err)
	}
	for _, cs := range changeSets {
		slug, _ := cs["Slug"].(string)
		n := cycleOf(slug)
		if n == 0 {
			continue
		}
		if n > st.cycle {
			st.cycle = n
		}
		if state, _ := cs["State"].(string); state == "Open" {
			st.changeSet = slug
			st.phase = closing
			if requests, _ := cs["ReviewRequests"].([]any); len(requests) > 0 {
				st.reviewRequested = true
			}
		}
	}
	if st.phase == closing {
		r.logf(st, "resuming: changeset %s is open", st.changeSet)
		return nil
	}
	latestClosed := ""
	for _, cs := range changeSets {
		slug, _ := cs["Slug"].(string)
		if state, _ := cs["State"].(string); state == "Closed" && cycleOf(slug) == st.cycle {
			latestClosed = slug
		}
	}
	orders, err := listJSON(st.ctx, "ChangeOrder", "changeorder", "list", "--space", st.base)
	if err != nil {
		return fmt.Errorf("agent %s: %w", st.def.Name, err)
	}
	moving, named := 0, map[string]bool{}
	for _, co := range orders {
		slug, _ := co["Slug"].(string)
		n := cycleOf(slug)
		if n == 0 {
			continue
		}
		named[slug] = true
		if n > st.cycle {
			st.cycle = n
		}
		if state, _ := co["State"].(string); completed(co) || terminalState(state) || n < moving {
			continue
		}
		moving = n
		st.changeSet = slug
	}
	// Only the latest change is continued: an older change order still in
	// flight was overtaken, and is left where it stands rather than driven
	// against the newer one. A closed ChangeSet no change order names yet is
	// a rollout that has not started.
	switch {
	case moving > 0 && moving == st.cycle:
		st.phase = rolling
		r.logf(st, "resuming: change order %s is in flight", st.changeSet)
	case latestClosed != "" && !named[latestClosed] && st.def.Rollout:
		st.changeSet = latestClosed
		st.phase = queued
		r.logf(st, "resuming: changeset %s is closed and not rolled out yet", st.changeSet)
	default:
		st.changeSet = ""
	}
	return nil
}

// terminalState is the change order states nothing comes back from.
func terminalState(state string) bool {
	switch state {
	case "Aborted", "Restored", "RestoreReleased":
		return true
	}
	return false
}

// step is one move of one agent.
func (r *Runner) step(st *agentState) error {
	if st.base == "" {
		return r.presenter(st).RunSteps(st.def.Name, st.def.Change)
	}
	switch st.phase {
	case idle:
		return r.open(st)
	case closing:
		return r.close(st)
	case queued:
		return r.queue(st)
	case rolling:
		return r.roll(st)
	}
	return nil
}

// presenter is the shared presenter acting as the agent, writing inside its
// ChangeSet under its clearance.
func (r *Runner) presenter(st *agentState) *present.Presenter {
	p := *r.P
	p.Out = prefixed{out: r.Out, prefix: "[" + st.def.Name + "] "}
	p.Context = st.ctx
	p.Clearance = st.def.Clearance
	p.Vars = map[string]string{"Cycle": strconv.Itoa(st.cycle)}
	if st.changeSet != "" {
		p.ChangeSet = st.base + "/" + st.changeSet
	}
	return &p
}

// open starts a cycle: a ChangeSet on the base, under the review workflow
// where the component requires one, the agent's change inside it, then the
// first attempt to close it. A base another ChangeSet holds is waited for,
// which is the queue the audience sees form behind a change under review.
func (r *Runner) open(st *agentState) error {
	held, err := r.heldBy(st)
	if err != nil {
		return err
	}
	if len(held) > 0 {
		r.logf(st, "waiting: %s is held by changeset %s", st.base, strings.Join(held, ", "))
		return nil
	}
	slug := fmt.Sprintf("%s-%d", st.def.Name, st.cycle+1)
	desc := st.def.Description
	if desc == "" {
		desc = fmt.Sprintf("%s: change %d", st.def.Name, st.cycle+1)
	}
	create := []string{"changeset", "create", "--space", st.base, slug, "--description", desc}
	required, err := r.requiresWorkflow(st)
	if err != nil {
		return err
	}
	if required {
		if r.P.Model.Scenario.Workflows.Review == nil {
			return fmt.Errorf("component %s requires a ChangeWorkflow, and the scenario declares no workflows.review to open a changeset under", st.def.Component)
		}
		create = append(create, "--change-workflow", r.P.Model.Home+"/"+scenario.ReviewWorkflowSlug)
	}
	if _, err := r.cub(st, create...); err != nil {
		return err
	}
	out, err := r.cub(st, "variant", "changeset", "open", st.base, slug)
	// Someone took the base between the check and the open. A cub that opens
	// a changeset on all of the units or none says so; an older one opens it
	// on the units left, which here is none.
	taken := err != nil && strings.Contains(err.Error(), "was not opened on any unit")
	if err != nil && !taken {
		return err
	}
	if taken || strings.Contains(out, "on 0 unit(s)") {
		_, _ = r.cub(st, "changeset", "delete", "--space", st.base, slug)
		r.logf(st, "waiting: %s was taken by another changeset", st.base)
		return nil
	}
	st.cycle++
	st.changeSet = slug
	st.reviewRequested = false
	st.phase = closing
	if err := r.presenter(st).RunSteps(st.def.Name, st.def.Change); err != nil {
		return err
	}
	return r.close(st)
}

// requiresWorkflow says whether the agent's component requires a
// ChangeWorkflow, in which case its ChangeSets are opened under the review
// workflow. It is read from the Component every cycle, so a rule that
// arrives while the agents run applies to the next change.
func (r *Runner) requiresWorkflow(st *agentState) (bool, error) {
	components, err := listJSON(st.ctx, "Component", "component", "list", "--where", fmt.Sprintf("Slug = '%s'", st.def.Component))
	if err != nil {
		return false, err
	}
	for _, c := range components {
		if slug, _ := c["Slug"].(string); slug == st.def.Component {
			// RequireChangeWorkflow was named ChangeWorkflowRequired before ConfigHub renamed it.
			required, _ := c["RequireChangeWorkflow"].(bool)
			if !required {
				required, _ = c["ChangeWorkflowRequired"].(bool)
			}
			return required, nil
		}
	}
	return false, nil
}

// heldBy names the ChangeSets holding units of the base.
func (r *Runner) heldBy(st *agentState) ([]string, error) {
	units, err := listJSON(st.ctx, "Unit", "unit", "list", "--space", st.base)
	if err != nil {
		return nil, err
	}
	var ids []string
	seen := map[string]bool{}
	for _, u := range units {
		if id, _ := u["ChangeSetID"].(string); id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	changeSets, err := listJSON(st.ctx, "ChangeSet", "changeset", "list", "--space", st.base)
	if err != nil {
		return nil, err
	}
	slugs := map[string]string{}
	for _, cs := range changeSets {
		id, _ := cs["ChangeSetID"].(string)
		slug, _ := cs["Slug"].(string)
		slugs[id] = slug
	}
	held := make([]string, 0, len(ids))
	for _, id := range ids {
		if slug := slugs[id]; slug != "" {
			held = append(held, slug)
		} else {
			held = append(held, id)
		}
	}
	return held, nil
}

// close tries to close the ChangeSet. Refused by the workflow's gates, it
// asks the reviewer once and waits; closed, it rolls the change out or goes
// idle.
func (r *Runner) close(st *agentState) error {
	_, err := r.cub(st, "variant", "changeset", "close", st.base, st.changeSet)
	if err == nil {
		r.logf(st, "closed changeset %s", st.changeSet)
		if !st.def.Rollout {
			st.phase = idle
			st.changeSet = ""
			return nil
		}
		st.phase = queued
		return r.queue(st)
	}
	text := err.Error()
	if !strings.Contains(text, "gates do not hold") {
		return err
	}
	if !st.reviewRequested && r.Reviewer != "" {
		if _, err := r.cub(st, "changeset", "update", "--space", st.base, st.changeSet, "--request-review", r.Reviewer); err != nil {
			return err
		}
		st.reviewRequested = true
	}
	r.logf(st, "waiting for review of changeset %s: %s", st.changeSet, lineWith(text, "requires"))
	return nil
}

// queue starts the rollout once no other change order of the component is
// in flight: two changes rolling through the same stages at once would
// release over each other, so one component's changes go out one at a time,
// and a change that has to wait says what it waits for.
func (r *Runner) queue(st *agentState) error {
	orders, err := listJSON(st.ctx, "ChangeOrder", "changeorder", "list", "--space", st.base)
	if err != nil {
		return err
	}
	for _, co := range orders {
		slug, _ := co["Slug"].(string)
		state, _ := co["State"].(string)
		if slug == st.changeSet || completed(co) || terminalState(state) {
			continue
		}
		r.logf(st, "waiting: change order %s of %s is still rolling out", slug, st.def.Component)
		return nil
	}
	return r.startRollout(st)
}

// startRollout names the closed change as a change order ending at the
// ChangeSet's end tag, under the component's rollout workflow.
func (r *Runner) startRollout(st *agentState) error {
	cm, err := r.componentModel(st)
	if err != nil {
		return err
	}
	if _, err := r.cub(st, "changeorder", "create", "--space", st.base, st.changeSet,
		"--change-workflow", r.P.Model.Home+"/"+r.P.Model.WorkflowFor(cm).Slug,
		"--end-tag", st.base+"/"+st.changeSet+"-end",
		"--description", st.changeSet); err != nil {
		return err
	}
	st.phase = rolling
	return r.roll(st)
}

// roll advances the change order: the deployment spaces of the stage it is
// in are released pinned to it, then it is promoted into the next stage;
// refused, the gate's reason is what the agent waits on. Releasing before
// promoting, rather than after, is what lets a runner started over a change
// order already in a stage pick it up.
func (r *Runner) roll(st *agentState) error {
	ref := st.base + "/" + st.changeSet
	co, err := r.changeOrder(st)
	if err != nil {
		return err
	}
	if completed(co) {
		r.logf(st, "rollout %s complete", st.changeSet)
		st.phase = idle
		st.changeSet = ""
		return nil
	}
	if stage, _ := co["Stage"].(string); stage != "" {
		spaces, _, err := r.P.Resolve(present.Selector{Component: st.def.Component, Stage: stage, ChangeOrder: ref})
		if err != nil {
			return err
		}
		var released map[uuid.UUID]bool
		if id, err := uuid.Parse(fmt.Sprint(co["ChangeOrderID"])); err == nil {
			if released, err = r.P.ReleasedUnder(spaces, id); err != nil {
				return err
			}
		}
		for _, sp := range spaces {
			if released[sp.SpaceID] {
				continue
			}
			if _, err := r.cub(st, "release", "publish", "--revision", "ChangeOrder:"+ref, sp.Slug); err != nil {
				return err
			}
		}
	}
	args := []string{"variant", "promote", "--change-order", ref}
	for _, c := range st.def.Clearance {
		args = append(args, "--clearance", c)
	}
	if _, err := r.cub(st, args...); err != nil {
		r.logf(st, "rollout %s waiting: %s", st.changeSet, lineWith(err.Error(), "Failed:"))
		return nil
	}
	return nil
}

func (r *Runner) componentModel(st *agentState) (*scenario.ComponentModel, error) {
	for _, cm := range r.P.Model.Components {
		if cm.Name == st.def.Component {
			return cm, nil
		}
	}
	return nil, fmt.Errorf("component %s is not in the model", st.def.Component)
}

func (r *Runner) changeOrder(st *agentState) (map[string]any, error) {
	out, err := cubexec.RunIn(st.ctx, "changeorder", "get", "--space", st.base, st.changeSet, "-o", "json")
	if err != nil {
		return nil, err
	}
	var co map[string]any
	if err := json.Unmarshal([]byte(out), &co); err != nil {
		return nil, fmt.Errorf("changeorder get %s: %w", st.changeSet, err)
	}
	if inner, _ := co["ChangeOrder"].(map[string]any); inner != nil {
		co = inner
	}
	return co, nil
}

// completed reads whether the change order's workflow is done with it.
func completed(co map[string]any) bool {
	if done, _ := co["Completed"].(bool); done {
		return true
	}
	stage, _ := co["Stage"].(string)
	return stage == "Completed"
}

// cub prints and runs one cub command as the agent.
func (r *Runner) cub(st *agentState, args ...string) (string, error) {
	r.logf(st, "▶ cub %s", strings.Join(args, " "))
	out, err := cubexec.RunIn(st.ctx, args...)
	if err != nil {
		return out, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			r.logf(st, "  %s", line)
		}
	}
	return out, nil
}

func (r *Runner) logf(st *agentState, format string, args ...any) {
	fmt.Fprintf(r.Out, "%s [%s] %s\n", time.Now().Format("15:04:05"), st.def.Name, fmt.Sprintf(format, args...))
}

// listJSON runs a cub list command with JSON output and returns the entities,
// unwrapped from the {"<Kind>": {...}} envelope list output uses.
func listJSON(context, kind string, args ...string) ([]map[string]any, error) {
	out, err := cubexec.RunIn(context, append(args, "-o", "json")...)
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	for i, item := range items {
		if inner, _ := item[kind].(map[string]any); inner != nil {
			items[i] = inner
		}
	}
	return items, nil
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// lineWith is the first line of text containing the word, for a one-line
// report of a refusal: what follows cub's "Failed:" when there is one, the
// whole text, trimmed, when no line has the word.
func lineWith(text, word string) string {
	text = ansi.ReplaceAllString(text, "")
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, word) {
			line = strings.TrimSpace(line)
			if _, after, ok := strings.Cut(line, "Failed: "); ok {
				return after
			}
			return line
		}
	}
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}

// prefixed writes each line of a play's output under the agent's name.
type prefixed struct {
	out    io.Writer
	prefix string
}

func (w prefixed) Write(b []byte) (int, error) {
	text := strings.TrimRight(string(b), "\n")
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(w.out, "%s%s\n", w.prefix, line)
	}
	return len(b), nil
}
