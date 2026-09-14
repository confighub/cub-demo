package manifests

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/confighub/cub-demo/internal/scenario"
)

// ExternalResourceTypes maps an external kind to the Crossplane resource type
// its template declares, in the form ConfigHub functions take as their
// resource-type argument. Template and map must agree.
var ExternalResourceTypes = map[string]string{
	"rds":    "rds.aws.upbound.io/v1beta2/Instance",
	"bucket": "s3.aws.upbound.io/v1beta1/Bucket",
	"queue":  "sqs.aws.upbound.io/v1beta1/Queue",
	"cache":  "elasticache.aws.upbound.io/v1beta1/ReplicationGroup",
}

// ExternalContext is what an external-resource template sees.
type ExternalContext struct {
	Scenario  *scenario.Scenario
	Component string
	Name      string
}

// RenderExternal renders the shared template for one external resource.
func RenderExternal(b *scenario.Bundle, component string, ext scenario.External) ([]byte, error) {
	raw, err := b.ExternalTemplate(ext.Kind)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(ext.Kind).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("external %s: %w", ext.Name, err)
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, ExternalContext{Scenario: b.Scenario, Component: component, Name: ext.Name})
	if err != nil {
		return nil, fmt.Errorf("external %s: %w", ext.Name, err)
	}
	return out.Bytes(), nil
}
