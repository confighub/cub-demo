package seed

import (
	"fmt"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
)

// views creates the org-wide saved filters (and a view over each) in the home
// space, so the fleet is one click away in the UI and one --filter away in the
// CLI.
func (s *Seeder) views() error {
	home := s.space(s.Model.Home)
	if home == nil && !s.DryRun {
		return fmt.Errorf("home space %s does not exist; run the home phase first", s.Model.Home)
	}
	name := s.Model.Scenario.Name
	// Column names target the CLI and the View Explorer (the two surfaces the
	// demo commits to): bare unit attributes (Slug), Space.Slug, and dynamic
	// Labels.<key> names. Grouping is the entity's own GroupBy (honored by
	// both); the ui.confighub.io/group-by annotation is set as well for the
	// units-page tabs until confighub#5255 aligns them.
	//
	// Configuration content is shown through Resource views (From/Of =
	// Resource): one row per resource, with DataPath columns walking the
	// stored config — "Replicas=path:spec.replicas". Both `cub resource list
	// --view` and the View Explorer evaluate them.
	type def struct {
		slug, display, from, where, groupBy string
		columns                             []string
	}
	spaceCols := []string{"Slug", "Labels.Component", "Labels.Stage", "Labels.Region", "Labels.Department"}
	defs := []def{
		{"fleet-clusters", "Fleet clusters", "Space",
			demoWhere(s.Model) + " AND Labels.Layer = 'cluster'", "Labels.Region",
			[]string{"Slug", "Labels.Region", "Labels.Stage", "Labels.Department"}},
		{"prod-deployments", "Prod deployments", "Space",
			demoWhere(s.Model) + " AND Labels.Role = 'deployment' AND Labels.Stage = 'prod'", "Labels.Region", spaceCols},
		{"payments-fleet", "Payments fleet", "Space",
			demoWhere(s.Model) + " AND Labels.Department = 'payments'", "Labels.Stage", spaceCols},
		{"platform-catalog", "Platform catalog", "Space",
			demoWhere(s.Model) + " AND Labels.Layer = 'platform' AND Labels.Role = 'base'", "Labels.Component", spaceCols},
		{"never-released", "Never released", "Unit",
			demoWhere(s.Model) + " AND Space.Labels.Role = 'deployment' AND LastReleasedRevisionNum = 0", "Labels.Component",
			[]string{"Slug", "Space.Slug", "Labels.Component", "Labels.Stage", "HeadRevisionNum"}},
		// The next two are Resource views over the workload Deployments: config
		// content straight from the stored data, no recorded Values needed.
		{"resource-limits", "Resource limits", "Resource",
			fmt.Sprintf("ResourceType = 'apps/v1/Deployment' AND Space.Labels.%s = '%s' AND Space.Labels.Role = 'deployment'",
				LabelDemoName, s.Model.Scenario.Name), "Space.Labels.Stage",
			[]string{"ResourceName", "Unit.Slug", "Space.Slug", "Space.Labels.Stage",
				"Replicas=path:spec.replicas",
				"CPU Limit=path:spec.template.spec.containers.0.resources.limits.cpu",
				"Mem Limit=path:spec.template.spec.containers.0.resources.limits.memory"}},
		{"images", "Container images", "Resource",
			fmt.Sprintf("ResourceType = 'apps/v1/Deployment' AND Space.Labels.%s = '%s' AND Space.Labels.Role = 'deployment'",
				LabelDemoName, s.Model.Scenario.Name), "Unit.Labels.Component",
			[]string{"ResourceName", "Unit.Slug", "Space.Slug", "Unit.Labels.Component", "Space.Labels.Stage",
				"Image=path:spec.template.spec.containers.0.image"}},
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would ensure %d filters and views in %s\n", len(defs), s.Model.Home)
		return nil
	}
	for _, d := range defs {
		// Converge: a view whose definition changed is deleted and recreated,
		// along with its same-named filter — an AllowExists filter reuse would
		// carry a stale From/Where (the server rejects an Of that mismatches
		// Filter.From). The definition hash annotation detects the change.
		defHash := fnsHashAny([]any{d.slug, d.display, d.from, d.where, d.groupBy, d.columns})
		if existing, err := s.Client.ViewBySlug(home.SpaceID, name+"-"+d.slug); err != nil {
			return err
		} else if existing != nil {
			if existing.Annotations["cub-demo.confighub.com/def-hash"] == defHash {
				continue
			}
			if err := s.Client.DeleteView(home.SpaceID, existing.ViewID); err != nil {
				return fmt.Errorf("replace view %s: %w", d.slug, err)
			}
		}
		// The filter converges on its own: a stale one (changed From or Where)
		// is replaced regardless of whether the view above still exists — a
		// partially failed earlier run can leave the filter behind without it.
		if oldFilter, err := s.Client.FilterBySlug(home.SpaceID, name+"-"+d.slug); err != nil {
			return err
		} else if oldFilter != nil && (oldFilter.From != d.from || oldFilter.Where != d.where) {
			if err := s.Client.DeleteFilter(home.SpaceID, oldFilter.FilterID); err != nil {
				return fmt.Errorf("replace filter %s: %w", d.slug, err)
			}
		}
		filter, err := s.Client.CreateFilter(home.SpaceID, goclient.Filter{
			SpaceID:     home.SpaceID,
			Slug:        name + "-" + d.slug,
			DisplayName: d.display,
			From:        d.from,
			Where:       d.where,
			Labels:      s.baseLabels(nil),
		})
		if err != nil {
			return fmt.Errorf("filter %s: %w", d.slug, err)
		}
		columns := make([]goclient.Column, 0, len(d.columns))
		for _, c := range d.columns {
			if name, path, ok := strings.Cut(c, "=path:"); ok {
				columns = append(columns, goclient.Column{Name: name, ColumnType: "DataPath",
					ColumnSource: &goclient.ColumnSource{DataPath: &goclient.AttributeSelector{Path: path}}})
			} else {
				columns = append(columns, goclient.Column{Name: c})
			}
		}
		err = s.Client.CreateView(home.SpaceID, goclient.View{
			SpaceID:     home.SpaceID,
			Slug:        name + "-" + d.slug,
			DisplayName: d.display,
			Of:          d.from,
			FilterID:    &filter.FilterID,
			Columns:     columns,
			GroupBy:     d.groupBy,
			Annotations: map[string]string{"ui.confighub.io/group-by": d.groupBy, "cub-demo.confighub.com/def-hash": defHash},
			Labels:      s.baseLabels(nil),
		})
		if err != nil {
			return fmt.Errorf("view %s: %w", d.slug, err)
		}
	}
	fmt.Fprintf(s.Out, "  ensured %d filters and views in %s\n", len(defs), s.Model.Home)
	return nil
}
