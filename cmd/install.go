package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/seed"
)

func newInstallCmd() *cobra.Command {
	var concurrency int
	var dryRun bool
	c := &cobra.Command{
		Use:   "install <scenario>",
		Short: "Store a scenario definition in the active org",
		Long: `Store a scenario definition in the org of the active cub context: the
scenario file and its manifests become units in a <name>-scenario space, and
every other verb reads them back from there. <scenario> is an embedded
scenario name ("cub demo scenario list") or a path to a scenario file.

Install writes only the definition; "cub demo up" converges the org to it.
Re-installing an edited copy lands as ordinary revisions on the bundle's
units -- the definition has history, like everything else in ConfigHub.`,
		Args:              cobra.ExactArgs(1),
		PersistentPreRunE: ensureClient,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, b, err := loadDefinition(args[0])
			if err != nil {
				return err
			}
			if err := checkContext(m, m.Scenario.Context); err != nil {
				return err
			}
			printContext(m)
			s := &seed.Seeder{Client: cub, Model: m, Bundle: b, Concurrency: concurrency, DryRun: dryRun, Out: os.Stdout}
			if err := s.StoreBundle(); err != nil {
				return err
			}
			if !dryRun {
				tprint("Installed. 'cub demo up' converges the org to it.")
			}
			return nil
		},
	}
	c.Flags().IntVar(&concurrency, "concurrency", 8, "parallel API calls")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be stored, change nothing")
	return c
}
