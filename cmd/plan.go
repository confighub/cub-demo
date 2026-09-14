package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/scenario"
	"github.com/confighub/cub-demo/internal/seed"
)

func newPlanCmd() *cobra.Command {
	var output string
	var verbose bool
	c := &cobra.Command{
		Use:   "plan [<scenario>]",
		Short: "Show what a scenario expands to, without changing anything",
		Long: `Expand a scenario and print the clusters, the component variant trees and
the entity totals that "cub demo up" would create. Named (an embedded
scenario or a path to a scenario file) this is pure computation -- no
ConfigHub server is contacted, so it works before "cub auth login" and
before "cub demo install". Unnamed, it expands the demo installed in the
active org.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var m *scenario.Model
			var err error
			if len(args) == 1 {
				m, _, err = loadDefinition(args[0])
			} else {
				if err = ensureClient(cmd, args); err != nil {
					return err
				}
				m, _, err = loadInstalled()
			}
			if err != nil {
				return err
			}
			switch output {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(m)
			case "table":
				printPlan(os.Stdout, m, verbose)
				return nil
			default:
				return fmt.Errorf("unknown output %q (table, json)", output)
			}
		},
	}
	c.Flags().StringVarP(&output, "output", "o", "table", "output format: table or json")
	c.Flags().BoolVarP(&verbose, "verbose", "v", false, "list every cluster and deployment")
	return c
}

// loadDefinition loads and expands a scenario definition: an embedded
// scenario name or a path to a scenario file. Used by the two verbs that work
// on definitions -- plan (preview) and install (store it in the org).
func loadDefinition(ref string) (*scenario.Model, *scenario.Bundle, error) {
	b, err := scenario.Load(assets, ref)
	if err != nil {
		return nil, nil, err
	}
	m, err := scenario.Expand(b)
	return m, b, err
}

// loadInstalled loads and expands the scenario installed in the active org,
// read back from the bundle install stored there -- so every operating verb
// acts on what the org holds, never on what the binary embeds. The org holds
// one demo, or --demo names which.
func loadInstalled() (*scenario.Model, *scenario.Bundle, error) {
	b, err := installedBundle()
	if err != nil {
		return nil, nil, err
	}
	m, err := scenario.Expand(b)
	return m, b, err
}

// installedBundle finds the installed scenario's <name>-scenario space and
// loads the bundle stored in it.
func installedBundle() (*scenario.Bundle, error) {
	spaces, err := cub.ListSpaces("Labels.Layer = 'demo'")
	if err != nil {
		return nil, fmt.Errorf("find the org's scenario: %w", err)
	}
	ctx := os.Getenv("CUB_CONTEXT")
	// Both the home and the scenario space carry Layer=demo; the bundle lives
	// only in <name>-scenario, so bind to that slug -- picking by label alone
	// once read the home space and found no bundle in it.
	byName := map[string]*goclient.Space{}
	for _, sp := range spaces {
		if name := sp.Labels[seed.LabelDemoName]; name != "" && sp.Slug == name+"-scenario" {
			byName[name] = sp
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	name := demoName
	switch {
	case len(names) == 0:
		return nil, fmt.Errorf("no demo is installed in this org (context %q); 'cub demo install <scenario>' installs one", ctx)
	case name != "":
		if byName[name] == nil {
			return nil, fmt.Errorf("no demo %q in this org (context %q); it holds: %s", name, ctx, strings.Join(names, ", "))
		}
	case len(names) == 1:
		name = names[0]
	default:
		return nil, fmt.Errorf("this org (context %q) holds %d demos (%s); name one with --demo", ctx, len(names), strings.Join(names, ", "))
	}
	sp := byName[name]
	units, err := cub.ListUnits(sp.SpaceID, "")
	if err != nil {
		return nil, fmt.Errorf("read the %s bundle: %w", name, err)
	}
	files := map[string][]byte{}
	for _, u := range units {
		p := u.Annotations[scenario.PathAnnotation]
		if p == "" {
			continue
		}
		data, err := cub.DownloadUnitData(u.SpaceID, u.UnitID)
		if err != nil {
			return nil, fmt.Errorf("read the %s bundle: %s: %w", name, p, err)
		}
		files[p] = data
	}
	b, err := scenario.LoadFromFiles(name, files)
	if err != nil {
		// A schema the stored bundle predates parses with unknown fields, and
		// every future schema change would hit this; fail with the way out
		// rather than a bare parse error.
		return nil, fmt.Errorf("%w\nThe org's stored scenario was installed by an older cub-demo; refresh it with: cub demo install %s", err, name)
	}
	return b, nil
}

func printPlan(w io.Writer, m *scenario.Model, verbose bool) {
	s := m.Scenario
	t := m.Totals
	fmt.Fprintf(w, "Scenario %s: %s (%s)\n\n", s.Name, s.Company, s.Domain)

	fmt.Fprintf(w, "Clusters: %d across %d regions, %d classes, %d departments + shared\n",
		t.Clusters, len(s.Regions), len(s.Classes), len(s.Departments))
	tw := tabwriter.NewWriter(w, 2, 0, 2, ' ', 0)
	depts := append([]string{"shared"}, s.Departments...)
	fmt.Fprintf(tw, "  class\t%s\ttotal\n", strings.Join(depts, "\t"))
	for _, class := range s.Classes {
		cells := []string{class.Name}
		total := 0
		for _, d := range depts {
			n := 0
			for _, c := range m.Clusters {
				if c.Class == class.Name && deptOf(c) == d {
					n++
				}
			}
			total += n
			cells = append(cells, fmt.Sprint(n))
		}
		fmt.Fprintf(tw, "  %s\t%d\n", strings.Join(cells, "\t"), total)
	}
	tw.Flush()
	byRegion := map[string]int{}
	for _, c := range m.Clusters {
		byRegion[c.Region]++
	}
	var regionCells []string
	for _, r := range s.Regions {
		regionCells = append(regionCells, fmt.Sprintf("%s %d", r.Name, byRegion[r.Name]))
	}
	fmt.Fprintf(w, "  by region: %s\n\n", strings.Join(regionCells, ", "))

	fmt.Fprintf(w, "Components: %d platform, %d workloads\n", len(s.Catalog), len(s.Workloads))
	tw = tabwriter.NewWriter(w, 2, 0, 2, ' ', 0)
	classCols := make([]string, 0, len(s.Classes))
	for _, c := range s.Classes {
		classCols = append(classCols, c.Name)
	}
	fmt.Fprintf(tw, "  component\tlayer\towner\tunits\t%s\tdeployments\n", strings.Join(classCols, "\t"))
	for _, c := range m.Components {
		cells := []string{c.Name, c.Layer, c.Owner, fmt.Sprint(len(c.Units))}
		for _, class := range s.Classes {
			n := 0
			for _, d := range c.Deployments {
				if d.Class == class.Name {
					n++
				}
			}
			if n == 0 {
				cells = append(cells, "-")
			} else {
				cells = append(cells, fmt.Sprint(n))
			}
		}
		fmt.Fprintf(tw, "  %s\t%d\n", strings.Join(cells, "\t"), len(c.Deployments))
	}
	tw.Flush()
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Story: %d degraded, %d out of sync, %d progressing, %d with unreleased changes, %d skews\n\n",
		len(m.Story.Degraded), len(m.Story.OutOfSync), len(m.Story.Progressing), len(m.Story.Unreleased), len(s.Story.Skews))

	fmt.Fprintln(w, "Totals:")
	tw = tabwriter.NewWriter(w, 2, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  spaces\t%d\t(1 home + %d clusters + %d root bases + %d class bases + %d deployments)\n",
		t.Spaces, t.Clusters, t.RootBases, t.ClassBases, t.Deployments)
	fmt.Fprintf(tw, "  targets\t%d\n", t.Targets)
	fmt.Fprintf(tw, "  workers\t%d\n", t.Workers)
	fmt.Fprintf(tw, "  units\t%d\n", t.Units)
	fmt.Fprintf(tw, "  links\t%d\n", t.Links)
	fmt.Fprintf(tw, "  releases\t%d\n", t.Releases)
	fmt.Fprintf(tw, "  function calls\t%d\n", t.FunctionCalls)
	tw.Flush()
	fmt.Fprintf(w, "\nDefault org quotas are 100 spaces, 250 targets and workers, 1000 units and links;\n"+
		"raise them on the server with 'confighub admin quota set' before 'cub demo up'.\n")

	if !verbose {
		return
	}
	fmt.Fprintln(w, "\nClusters:")
	tw = tabwriter.NewWriter(w, 2, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  name\tregion\tclass\tdepartment\tk8s\tnodes")
	for _, c := range m.Clusters {
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%d\n", c.Name, c.Region, c.Class, deptOf(c), c.KubernetesVersion, c.NodeCount)
	}
	tw.Flush()
	fmt.Fprintln(w, "\nDeployments:")
	for _, c := range m.Components {
		spaces := make([]string, 0, len(c.Deployments))
		for _, d := range c.Deployments {
			spaces = append(spaces, d.Cluster.Name)
		}
		sort.Strings(spaces)
		fmt.Fprintf(w, "  %s (%s): %s\n", c.Name, c.RootSpace, strings.Join(spaces, " "))
	}
}

func deptOf(c *scenario.Cluster) string {
	if c.Shared() {
		return "shared"
	}
	return c.Department
}
