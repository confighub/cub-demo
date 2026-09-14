package seed

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/confighub/cub-demo/internal/cubclient"
	"github.com/confighub/cub-demo/internal/manifests"
	"github.com/confighub/cub-demo/internal/scenario"
)

// deployFnsMarker is the annotation on a component's root base recording which
// per-region/per-cluster function set has been applied to its deployments; a
// matching marker skips the whole batch on re-runs.
const deployFnsMarker = "confighub.com/demo-deployment-functions"

// deploymentFunctions applies the per-region and per-cluster variation to a
// component's deployment units: the component.yaml perRegion and perCluster
// lists, plus one generated call per external-resource kind that points
// spec.forProvider.region at the deployment's cloud region.
func (s *Seeder) deploymentFunctions(cm *scenario.ComponentModel) error {
	regionCalls := append([]scenario.FunctionCall{}, cm.Spec.PerRegion...)
	kinds := map[string][]string{}
	for _, e := range cm.External {
		kinds[e.Kind] = append(kinds[e.Kind], "'"+e.Name+"'")
	}
	for kind, names := range kinds {
		sort.Strings(names)
		regionCalls = append(regionCalls, scenario.FunctionCall{
			Function: "set-string-path",
			Args:     []string{manifests.ExternalResourceTypes[kind], "spec.forProvider.region", "{{.Region.ProviderRegion}}"},
			Where:    fmt.Sprintf("Slug IN (%s)", strings.Join(names, ", ")),
		})
	}
	if len(regionCalls) == 0 && len(cm.Spec.PerCluster) == 0 && len(cm.Spec.PerVariant) == 0 {
		return nil
	}

	root := s.space(cm.RootSpace)
	hashed := append(append([]scenario.FunctionCall{}, regionCalls...), cm.Spec.PerCluster...)
	variantNames := make([]string, 0, len(cm.Spec.PerVariant))
	for v := range cm.Spec.PerVariant {
		variantNames = append(variantNames, v)
	}
	sort.Strings(variantNames)
	for _, v := range variantNames {
		hashed = append(hashed, scenario.FunctionCall{Function: "variant:" + v})
		hashed = append(hashed, cm.Spec.PerVariant[v]...)
	}
	marker := fnsHash(hashed) + fmt.Sprintf("-%d", len(cm.Deployments))
	if root.Annotations[deployFnsMarker] == marker {
		return nil
	}

	classByName := map[string]scenario.Class{}
	for _, c := range s.Model.Scenario.Classes {
		classByName[c.Name] = c
	}
	regionByName := map[string]scenario.Region{}
	regions := map[string]bool{}
	for _, r := range s.Model.Scenario.Regions {
		regionByName[r.Name] = r
	}
	for _, d := range cm.Deployments {
		regions[d.Cluster.Region] = true
	}

	base := fmt.Sprintf("%s AND Labels.Component = '%s' AND Space.Labels.Role = 'deployment'", demoWhere(s.Model), cm.Name)
	run := func(where string, calls []scenario.FunctionCall, ctx manifests.ArgContext, desc string) error {
		byWhere := map[string][]cubclient.Invocation{}
		var order []string
		for _, call := range calls {
			args, err := manifests.RenderArgs(call.Args, ctx)
			if err != nil {
				return err
			}
			if _, seen := byWhere[call.Where]; !seen {
				order = append(order, call.Where)
			}
			byWhere[call.Where] = append(byWhere[call.Where], cubclient.Invocation{Function: call.Function, Args: args})
		}
		sort.Strings(order)
		for _, w := range order {
			full := where
			if w != "" {
				full += " AND " + w
			}
			if err := s.Client.InvokeFunctions(full, desc, byWhere[w]); err != nil {
				return err
			}
		}
		return nil
	}

	if len(regionCalls) > 0 {
		var names []string
		for r := range regions {
			names = append(names, r)
		}
		sort.Strings(names)
		for _, r := range names {
			where := fmt.Sprintf("%s AND Labels.Region = '%s'", base, r)
			desc := fmt.Sprintf("%s region settings for %s", r, cm.Name)
			if err := run(where, regionCalls, manifests.ArgContext{Scenario: s.Model.Scenario, Region: regionByName[r]}, desc); err != nil {
				return fmt.Errorf("region %s: %w", r, err)
			}
		}
	}
	if len(cm.Spec.PerCluster) > 0 {
		for _, d := range cm.Deployments {
			where := fmt.Sprintf("%s AND Labels.Cluster = '%s'", base, d.Cluster.Name)
			ctx := manifests.ArgContext{Scenario: s.Model.Scenario, Class: classByName[d.Class], Region: regionByName[d.Cluster.Region], Cluster: d.Cluster}
			desc := fmt.Sprintf("cluster settings for %s on %s", cm.Name, d.Cluster.Name)
			if err := run(where, cm.Spec.PerCluster, ctx, desc); err != nil {
				return fmt.Errorf("cluster %s: %w", d.Cluster.Name, err)
			}
		}
	}

	// One cluster's own settings, after the class and cluster ones so they win.
	for _, v := range variantNames {
		var d *scenario.Deployment
		for _, cand := range cm.Deployments {
			if cand.Cluster.Name == v {
				d = cand
			}
		}
		if d == nil {
			// Component content is shared across scenarios; a per-variant
			// override names a cluster of one scenario's world and simply does
			// not apply where that cluster does not exist.
			fmt.Fprintf(s.Out, "  %s: perVariant %q names no cluster of this scenario; skipped\n", cm.Name, v)
			continue
		}
		where := fmt.Sprintf("%s AND Labels.Cluster = '%s'", base, v)
		ctx := manifests.ArgContext{Scenario: s.Model.Scenario, Class: classByName[d.Class], Region: regionByName[d.Cluster.Region], Cluster: d.Cluster}
		desc := fmt.Sprintf("%s settings for %s", v, cm.Name)
		if err := run(where, cm.Spec.PerVariant[v], ctx, desc); err != nil {
			return fmt.Errorf("variant %s: %w", v, err)
		}
	}
	patch, _ := json.Marshal(map[string]any{"Annotations": map[string]string{deployFnsMarker: marker}})
	if err := s.Client.PatchSpace(root.SpaceID, patch); err != nil {
		return err
	}
	root.Annotations = mergeAnnotations(root.Annotations, deployFnsMarker, marker)
	return nil
}

func mergeAnnotations(a map[string]string, k, v string) map[string]string {
	out := map[string]string{}
	for key, val := range a {
		out[key] = val
	}
	out[k] = v
	return out
}

// verify fnsHash stays usable for mixed lists
