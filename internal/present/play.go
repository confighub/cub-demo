package present

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/confighub/cub-demo/internal/scenario"
)

// PlayContext is what a command line in a scenario's commands: map can
// reference. Base and Workflow resolve a component's names so a move never
// spells a slug the seeder derives.
type PlayContext struct {
	Scenario *scenario.Scenario
	Home     string
	Context  string
	model    *scenario.Model
	vars     map[string]string
}

// Base is the component's root base space.
func (c PlayContext) Base(component string) (string, error) {
	for _, cm := range c.model.Components {
		if cm.Name == component {
			return cm.RootSpace, nil
		}
	}
	return "", fmt.Errorf("no component %q", component)
}

// Workflow is the shared ChangeWorkflow the component's change orders bind,
// as space/slug: the one matching its class coverage (most components share
// the standard one).
func (c PlayContext) Workflow(component string) (string, error) {
	for _, cm := range c.model.Components {
		if cm.Name == component {
			return c.Home + "/" + c.model.WorkflowFor(cm).Slug, nil
		}
	}
	return "", fmt.Errorf("no component %q", component)
}

// Vars is the play-scoped state primitives export for later steps: after
// bump-image, {{.Version}} and {{.PreviousVersion}} resolve in run lines and
// with: values. Unknown names fail the template, so a typo is an error, not
// an empty string.
func (c PlayContext) Version() (string, error)         { return c.varNamed("Version") }
func (c PlayContext) PreviousVersion() (string, error) { return c.varNamed("PreviousVersion") }

// Var reads any exported variable by name, for vocabulary future primitives add.
func (c PlayContext) Var(name string) (string, error) { return c.varNamed(name) }

// Pick chooses one of a comma-separated list by the Cycle variable the agent
// runner exports, so an agent's change can differ from one cycle to the next:
// {{.Pick "768Mi,1Gi,1536Mi"}} is 768Mi on the first cycle, 1Gi on the
// second, and round again after the last.
func (c PlayContext) Pick(list string) (string, error) {
	items := strings.Split(list, ",")
	if len(items) == 0 || list == "" {
		return "", fmt.Errorf("Pick: an empty list")
	}
	cycle, err := c.varNamed("Cycle")
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(cycle)
	if err != nil {
		return "", fmt.Errorf("Pick: Cycle %q is not a number", cycle)
	}
	if n < 1 {
		n = 1
	}
	return strings.TrimSpace(items[(n-1)%len(items)]), nil
}

func (c PlayContext) varNamed(name string) (string, error) {
	if v, ok := c.vars[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("no variable %q has been exported by an earlier step", name)
}

// render expands one template string against the context and the exported
// variables.
func (c PlayContext) render(text string, vars map[string]string) (string, error) {
	c.vars = vars
	tmpl, err := template.New("step").Option("missingkey=error").Parse(text)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, c); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Plays lists the moves the scenario declares, sorted.
func (p *Presenter) Plays() []string {
	names := make([]string, 0, len(p.Model.Scenario.Plays))
	for name := range p.Model.Scenario.Plays {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Describe returns a play's steps unrendered, for listing: templates that
// depend on variables an earlier step exports cannot be expanded before the
// play runs.
func (p *Presenter) Describe(name string) ([]string, error) {
	steps, ok := p.Model.Scenario.Plays[name]
	if !ok {
		return nil, fmt.Errorf("scenario %s declares no play %q (have: %s)", p.Model.Name(), name, strings.Join(p.Plays(), ", "))
	}
	out := make([]string, 0, len(steps))
	for _, st := range steps {
		if st.Run != "" {
			out = append(out, st.Run)
			continue
		}
		desc := "call " + st.Call
		if len(st.With) > 0 {
			keys := make([]string, 0, len(st.With))
			for k := range st.With {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			kv := make([]string, 0, len(keys))
			for _, k := range keys {
				kv = append(kv, fmt.Sprintf("%s: %v", k, st.With[k]))
			}
			desc += " (" + strings.Join(kv, ", ") + ")"
		}
		out = append(out, desc)
	}
	return out, nil
}

// Play runs a move step by step, stopping at the first failure. Shell lines
// run through the shell with CUB_CONTEXT pinned to the scenario's context;
// call steps dispatch to the tool's primitives with their with: values
// rendered. Output streams to the terminal, since the point is to be watched.
func (p *Presenter) Play(name string) error {
	steps, ok := p.Model.Scenario.Plays[name]
	if !ok {
		return fmt.Errorf("scenario %s declares no play %q (have: %s)", p.Model.Name(), name, strings.Join(p.Plays(), ", "))
	}
	return p.RunSteps(name, steps)
}

// RunSteps runs a step list as Play runs a named play's; an agent's change is
// one. name labels errors.
func (p *Presenter) RunSteps(name string, steps []scenario.PlayStep) error {
	ctx := PlayContext{Scenario: p.Model.Scenario, Home: p.Model.Home, Context: p.Model.Scenario.Context, model: p.Model}
	vars := map[string]string{}
	for k, v := range p.Vars {
		vars[k] = v
	}
	for i, st := range steps {
		if st.Run != "" {
			line, err := ctx.render(st.Run, vars)
			if err != nil {
				return fmt.Errorf("play %s step %d: %w", name, i+1, err)
			}
			fmt.Fprintf(p.Out, "▶ %s\n", line)
			if p.DryRun {
				continue
			}
			cmd := exec.Command("sh", "-c", line)
			cubContext := p.Model.Scenario.Context
			if p.Context != "" {
				cubContext = p.Context
			}
			cmd.Env = append(os.Environ(), "CUB_CONTEXT="+cubContext,
				"CONFIGHUB_AGENT=1", DemoEnv+"="+p.Model.Name())
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Stdin = os.Stdin
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("play %s: %q: %w", name, line, err)
			}
			continue
		}
		fn, ok := primitives[st.Call]
		if !ok {
			return fmt.Errorf("play %s step %d: no primitive %q (have: %s)", name, i+1, st.Call, strings.Join(PrimitiveNames(), ", "))
		}
		with, err := p.renderWith(ctx, vars, st.With)
		if err != nil {
			return fmt.Errorf("play %s step %d (%s): %w", name, i+1, st.Call, err)
		}
		fmt.Fprintf(p.Out, "▶ %s\n", st.Call)
		if err := fn(p, vars, with); err != nil {
			return fmt.Errorf("play %s step %d (%s): %w", name, i+1, st.Call, err)
		}
	}
	return nil
}
