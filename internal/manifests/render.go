// Package manifests renders a component's manifest files for the root base.
// Rendering happens exactly once per unit: class, region and cluster variation
// is applied server-side by ConfigHub functions, not by re-rendering (see
// docs/DESIGN.md), so the templates only see scenario-wide values.
package manifests

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/confighub/cub-demo/internal/scenario"
)

// Context is what a manifest template sees.
type Context struct {
	// Scenario gives access to Company, Domain, Cloud and the dimension lists.
	Scenario *scenario.Scenario
	// Component is the component's name.
	Component string
	// Values are the component.yaml values.
	Values map[string]string
}

// RenderUnit renders one unit's manifest file from the component's directory.
func RenderUnit(b *scenario.Bundle, component string, unit scenario.UnitSpec) ([]byte, error) {
	raw, err := b.Open(component, unit.File)
	if err != nil {
		return nil, fmt.Errorf("unit %s: %w", unit.Slug, err)
	}
	spec := b.Specs[component]
	tmpl, err := template.New(unit.File).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("unit %s: %w", unit.Slug, err)
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, Context{Scenario: b.Scenario, Component: component, Values: spec.Values})
	if err != nil {
		return nil, fmt.Errorf("unit %s: %w", unit.Slug, err)
	}
	return out.Bytes(), nil
}

// ArgContext is what a function-call argument template sees; Scenario is
// always set, the rest according to the call's scope.
type ArgContext struct {
	Scenario *scenario.Scenario
	Class    scenario.Class
	Region   scenario.Region
	Cluster  *scenario.Cluster
}

// RenderArgs renders the argument templates of a function call.
func RenderArgs(args []string, ctx ArgContext) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		tmpl, err := template.New("arg").Option("missingkey=error").Parse(a)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, ctx); err != nil {
			return nil, err
		}
		out = append(out, buf.String())
	}
	return out, nil
}
