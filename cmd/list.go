package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "list",
		Short:             "List the demo scenarios present in the active org",
		Long:              `Group the org's spaces by their DemoName label: which demos live here and how big each is.`,
		Args:              cobra.NoArgs,
		PersistentPreRunE: ensureClient,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spaces, err := cub.ListSpaces("")
			if err != nil {
				return err
			}
			type row struct{ spaces, clusters, deployments int }
			byDemo := map[string]*row{}
			unlabeled := 0
			for _, sp := range spaces {
				name := sp.Labels["DemoName"]
				if name == "" {
					unlabeled++
					continue
				}
				r := byDemo[name]
				if r == nil {
					r = &row{}
					byDemo[name] = r
				}
				r.spaces++
				switch {
				case sp.Labels["Layer"] == "cluster":
					r.clusters++
				case sp.Labels["Role"] == "deployment":
					r.deployments++
				}
			}
			if len(byDemo) == 0 {
				tprint("no demo scenarios in this org (context %q)", os.Getenv("CUB_CONTEXT"))
				return nil
			}
			names := make([]string, 0, len(byDemo))
			for n := range byDemo {
				names = append(names, n)
			}
			sort.Strings(names)
			tw := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "DEMO\tSPACES\tCLUSTERS\tDEPLOYMENTS")
			for _, n := range names {
				r := byDemo[n]
				fmt.Fprintf(tw, "%s\t%d\t%d\t%d\n", n, r.spaces, r.clusters, r.deployments)
			}
			tw.Flush()
			if unlabeled > 0 {
				tprint("(%d spaces in this org carry no DemoName label)", unlabeled)
			}
			return nil
		},
	}
}
