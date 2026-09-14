package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/seed"
)

func newResetCmd() *cobra.Command {
	var yes, force bool
	var concurrency int
	c := &cobra.Command{
		Use:   "reset",
		Short: "Tear the scenario down and seed it again: back to the opening state",
		Long: `down --yes followed by up. A chapter that finishes a rollout spends its change
order, and a re-run of up leaves finished change orders alone, so only a
teardown brings the opening state back.`,
		Args:              cobra.NoArgs,
		PersistentPreRunE: ensureClient,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, b, err := loadInstalled()
			if err != nil {
				return err
			}
			if err := checkContext(m, m.Scenario.Context); err != nil {
				return err
			}
			printContext(m)
			if !yes {
				fmt.Printf("Delete and re-create all %q entities in this org? [y/N] ", m.Name())
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
					return fmt.Errorf("aborted")
				}
			}
			down := &seed.Seeder{Client: cub, Model: m, Concurrency: concurrency, Out: os.Stdout}
			if err := down.Teardown(force, true); err != nil {
				return err
			}
			up := &seed.Seeder{Client: cub, Model: m, Bundle: b, Concurrency: concurrency, Out: os.Stdout}
			if err := up.Run(nil); err != nil {
				return err
			}
			tprint("Reset. The scenario is back in its opening state.")
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	c.Flags().BoolVar(&force, "force", false, "override delete gates on teardown (recursive_force)")
	c.Flags().IntVar(&concurrency, "concurrency", 8, "parallel API calls")
	return c
}
