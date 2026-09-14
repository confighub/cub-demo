package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/scenario"
)

func newScenarioCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "scenario",
		Short: "List, show or export the embedded scenarios",
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List the embedded scenarios",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				names, err := scenario.Embedded(assets)
				if err != nil {
					return err
				}
				for _, n := range names {
					b, err := scenario.Load(assets, n)
					if err != nil {
						return err
					}
					m, err := scenario.Expand(b)
					if err != nil {
						return err
					}
					tprint("%-12s %s — %d clusters, %d components, %d spaces", n, b.Scenario.Company,
						m.Totals.Clusters, len(m.Components), m.Totals.Spaces)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "show <name>",
			Short: "Print an embedded scenario's YAML",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				raw, err := scenario.ReadEmbedded(assets, args[0])
				if err != nil {
					return fmt.Errorf("no embedded scenario %q", args[0])
				}
				_, err = os.Stdout.Write(raw)
				return err
			},
		},
		&cobra.Command{
			Use:   "export <name> <dir>",
			Short: "Copy an embedded scenario and its manifests into a directory to edit",
			Long: `Write <dir>/<name>.yaml and <dir>/manifests/... so the scenario can be
edited, previewed with "cub demo plan <dir>/<name>.yaml" and installed with
"cub demo install <dir>/<name>.yaml". Component directories that are removed
from the export fall back to the embedded copies.`,
			Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := scenario.Export(assets, args[0], args[1]); err != nil {
					return err
				}
				tprint("exported %s to %s", args[0], args[1])
				return nil
			},
		},
	)
	return c
}
