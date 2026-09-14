package present

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
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

// Workflow is the component's ChangeWorkflow unit as space/slug.
func (c PlayContext) Workflow(component string) (string, error) {
	if _, err := c.Base(component); err != nil {
		return "", err
	}
	return c.Home + "/" + component + "-workflow", nil
}

// Vars is the play-scoped state primitives export for later steps: after
// bump-image, {{.Version}} and {{.PreviousVersion}} resolve in run lines and
// with: values. Unknown names fail the template, so a typo is an error, not
// an empty string.
func (c PlayContext) Version() (string, error)         { return c.varNamed("Version") }
func (c PlayContext) PreviousVersion() (string, error) { return c.varNamed("PreviousVersion") }

// Var reads any exported variable by name, for vocabulary future primitives add.
func (c PlayContext) Var(name string) (string, error) { return c.varNamed(name) }

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
	ctx := PlayContext{Scenario: p.Model.Scenario, Home: p.Model.Home, Context: p.Model.Scenario.Context, model: p.Model}
	vars := map[string]string{}
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
			cmd.Env = append(os.Environ(), "CUB_CONTEXT="+p.Model.Scenario.Context,
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
