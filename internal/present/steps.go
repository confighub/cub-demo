package present

import (
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/cub-demo/internal/cubexec"
	"github.com/confighub/cub-demo/internal/scenario"
)

// The primitives a play's call: steps may name. Each is generic — selectors,
// paths and statuses are parameters — so every demo-specific noun ("ci",
// "argobot", an outage) lives in the scenario's plays, not here. A primitive
// may export variables for the steps after it (bump-image exports Version and
// Tag); templates in later steps read them as {{.Version}}.
var primitives = map[string]func(p *Presenter, vars map[string]string, with params) error{
	"observe":     stepObserve,
	"invoke":      stepInvoke,
	"bump-image":  stepBumpImage,
	"changeorder": stepChangeOrder,
}

// PrimitiveNames lists the callable primitives, for validation and help.
func PrimitiveNames() []string {
	names := make([]string, 0, len(primitives))
	for n := range primitives {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// params is a call step's rendered With map: strings templated, lists kept.
type params map[string]any

func (w params) str(key string) string {
	v, _ := w[key].(string)
	return v
}

func (w params) list(key string) []string {
	switch v := w[key].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case []string:
		return v
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case nil:
		return nil
	}
	return nil
}

func (w params) bool(key string) bool {
	switch v := w[key].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

// selector reads the shared selector keys.
func (w params) selector() Selector {
	return Selector{
		Component:   w.str("component"),
		Variants:    w.list("variants"),
		Stage:       w.str("stage"),
		ChangeOrder: w.str("changeorder"),
	}
}

// allow rejects keys outside the primitive's vocabulary, so a typo in a play
// fails naming the key instead of silently doing something else.
func (w params) allow(call string, keys ...string) error {
	ok := map[string]bool{}
	for _, k := range keys {
		ok[k] = true
	}
	var bad []string
	for k := range w {
		if !ok[k] {
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("%s: unknown parameter(s) %s (accepts: %s)", call, strings.Join(bad, ", "), strings.Join(keys, ", "))
	}
	return nil
}

var selectorKeys = []string{"component", "variants", "stage", "changeorder"}

// stepObserve writes a live-status observation on the selected deployment
// spaces: the one primitive behind a delivery bot's Synced/Healthy report
// (when: release-newer), an incident (health: Degraded/Progressing/OutOfSync)
// and its repair (the healthy default).
func stepObserve(p *Presenter, vars map[string]string, with params) error {
	if err := with.allow("observe", append(selectorKeys, "when", "health", "message", "force")...); err != nil {
		return err
	}
	sel := with.selector()
	switch with.str("when") {
	case "release-newer":
		return p.Argobot(sel, with.bool("force"))
	case "":
	default:
		return fmt.Errorf("observe: when %q: the only condition is release-newer", with.str("when"))
	}
	health := with.str("health")
	if health == "" || health == "Healthy" {
		return p.Heal(sel, with.str("message"))
	}
	return p.Incident(sel, health, with.str("message"))
}

// stepInvoke runs one ConfigHub function against one unit, the generic
// change-making move: whatever the function vocabulary can express, a play
// can do to a base.
func stepInvoke(p *Presenter, vars map[string]string, with params) error {
	if err := with.allow("invoke", "space", "unit", "function", "args", "change-desc"); err != nil {
		return err
	}
	space, unit, fn := with.str("space"), with.str("unit"), with.str("function")
	if space == "" || unit == "" || fn == "" {
		return fmt.Errorf("invoke: space, unit and function are required")
	}
	args := []string{"function", "set", "--space", space, "--unit", unit}
	if d := with.str("change-desc"); d != "" {
		args = append(args, "--change-desc", d)
	}
	args = append(append(args, "--", fn), with.list("args")...)
	return p.runCub(args)
}

// stepBumpImage reads the component's current image tag from its base (the
// release: block in component.yaml names the unit and container), bumps it,
// and writes it back. Exports Version (the new tag) and PreviousVersion for
// the steps after it.
func stepBumpImage(p *Presenter, vars map[string]string, with params) error {
	if err := with.allow("bump-image", "component", "strategy", "version", "change-desc"); err != nil {
		return err
	}
	cm, err := p.component(with.str("component"))
	if err != nil {
		return err
	}
	if cm.Spec == nil || cm.Spec.Release == nil {
		return fmt.Errorf("bump-image: %s has no release: block in its component.yaml, so the image to bump is unknown", cm.Name)
	}
	rel := cm.Spec.Release
	current, err := p.currentTag(cm.RootSpace, rel.Unit, rel.Container)
	if err != nil {
		return err
	}
	version := with.str("version")
	if version == "" {
		version, err = bump(current, with.str("strategy"))
		if err != nil {
			return err
		}
	}
	desc := with.str("change-desc")
	if desc == "" {
		desc = fmt.Sprintf("%s %s", cm.Name, version)
	}
	fmt.Fprintf(p.Out, "%s runs %s; shipping %s\n", cm.Name, current, version)
	err = p.runCub([]string{"function", "set", "--space", cm.RootSpace, "--unit", rel.Unit,
		"--change-desc", desc, "--", "set-image-reference", rel.Container, ":" + version})
	if err != nil {
		return err
	}
	vars["PreviousVersion"] = current
	vars["Version"] = version
	return nil
}

// stepChangeOrder names the change: creates a change order in the component's
// base under its workflow. The slug defaults to <component>-<Version> when a
// bump-image step ran earlier in the play.
func stepChangeOrder(p *Presenter, vars map[string]string, with params) error {
	if err := with.allow("changeorder", "component", "slug", "description"); err != nil {
		return err
	}
	cm, err := p.component(with.str("component"))
	if err != nil {
		return err
	}
	slug := with.str("slug")
	if slug == "" {
		version := vars["Version"]
		if version == "" {
			return fmt.Errorf("changeorder: no slug given and no earlier bump-image step to derive one from")
		}
		slug = cm.Name + "-" + strings.ReplaceAll(strings.TrimPrefix(version, "v"), ".", "-")
	}
	desc := with.str("description")
	if desc == "" {
		desc = strings.ReplaceAll(slug, "-", " ")
	}
	if !p.DryRun {
		sp, err := p.Client.SpaceBySlug(cm.RootSpace)
		if err != nil {
			return err
		}
		if sp == nil {
			return fmt.Errorf("%s is not seeded: base space %s not found", cm.Name, cm.RootSpace)
		}
		if existing, err := p.Client.ChangeOrderBySlug(sp.SpaceID, slug); err != nil {
			return err
		} else if existing != nil {
			return fmt.Errorf("change order %s/%s already exists (%s)", cm.RootSpace, slug, existing.State)
		}
	}
	return p.runCub([]string{"changeorder", "create", "--space", cm.RootSpace, slug,
		"--description", desc, "--change-workflow", p.Model.Home + "/" + cm.Name + "-workflow"})
}

// runCub prints and runs one cub command, honoring dry-run.
func (p *Presenter) runCub(args []string) error {
	fmt.Fprintf(p.Out, "▶ cub %s\n", shellJoin(args))
	if p.DryRun {
		return nil
	}
	out, err := cubexec.Run(args...)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		fmt.Fprintln(p.Out, indent(out))
	}
	return nil
}

// renderWith templates every string in a step's With map (including strings
// inside lists) against the play context and the exported variables.
func (p *Presenter) renderWith(ctx PlayContext, vars map[string]string, with map[string]any) (params, error) {
	out := params{}
	for k, v := range with {
		rendered, err := renderValue(ctx, vars, v)
		if err != nil {
			return nil, fmt.Errorf("with.%s: %w", k, err)
		}
		out[k] = rendered
	}
	return out, nil
}

func renderValue(ctx PlayContext, vars map[string]string, v any) (any, error) {
	switch t := v.(type) {
	case string:
		return ctx.render(t, vars)
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			r, err := renderValue(ctx, vars, item)
			if err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, nil
	}
	return v, nil
}

var _ = scenario.PlayStep{}
