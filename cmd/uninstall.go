package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/seed"
)

func newUninstallCmd() *cobra.Command {
	var yes, force, dryRun bool
	var concurrency int
	c := &cobra.Command{
		Use:   "uninstall",
		Short: "Delete the dataset and the installed definition from the active org",
		Long: `down, plus the <name>-scenario space holding the installed definition: nothing
of the demo remains in the org. "cub demo install" starts over.`,
		Args:              cobra.NoArgs,
		PersistentPreRunE: ensureClient,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, _, err := loadInstalled()
			if err != nil {
				return err
			}
			if err := checkContext(m, m.Scenario.Context); err != nil {
				return err
			}
			printContext(m)
			if !yes && !dryRun {
				fmt.Printf("Delete all %q entities, including the installed definition, from this org? [y/N] ", m.Name())
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
					return fmt.Errorf("aborted")
				}
			}
			s := &seed.Seeder{Client: cub, Model: m, Concurrency: concurrency, DryRun: dryRun, Out: os.Stdout}
			return s.Teardown(force, false)
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	c.Flags().BoolVar(&force, "force", false, "override delete gates (recursive_force)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be deleted, change nothing")
	c.Flags().IntVar(&concurrency, "concurrency", 8, "parallel deletions")
	return c
}
