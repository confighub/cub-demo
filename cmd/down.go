package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/seed"
)

func newDownCmd() *cobra.Command {
	var yes, force, dryRun bool
	var concurrency int
	c := &cobra.Command{
		Use:   "down",
		Short: "Delete the dataset the scenario created in the active org",
		Long: `Delete every space labelled with the scenario's DemoName, recursively (units,
links, releases, targets and the worker go with their spaces). Only entities
carrying the label are touched. The installed definition (the <name>-scenario
space) is kept, so "cub demo up" can re-create the dataset; "cub demo
uninstall" removes the definition too.`,
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
				fmt.Printf("Delete all %q entities from this org? [y/N] ", m.Name())
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
					return fmt.Errorf("aborted")
				}
			}
			s := &seed.Seeder{Client: cub, Model: m, Concurrency: concurrency, DryRun: dryRun, Out: os.Stdout}
			return s.Teardown(force, true)
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	c.Flags().BoolVar(&force, "force", false, "override delete gates (recursive_force)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be deleted, change nothing")
	c.Flags().IntVar(&concurrency, "concurrency", 8, "parallel deletions")
	return c
}
