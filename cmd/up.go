package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/seed"
)

func newUpCmd() *cobra.Command {
	var phases []string
	var concurrency int
	var dryRun bool
	c := &cobra.Command{
		Use:   "up",
		Short: "Converge the org to its installed scenario",
		Long: `Converge the org of the active cub context to the scenario "cub demo install"
stored in it: create whatever the definition describes and does not yet
exist. Idempotent, so an interrupted run is resumed by running up again, and
an updated definition is reconciled the same way. --phase limits the run to
named phases (` + phaseList() + `).`,
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
			s := &seed.Seeder{Client: cub, Model: m, Bundle: b, Concurrency: concurrency, DryRun: dryRun, Out: os.Stdout}
			if err := s.Run(phases); err != nil {
				return err
			}
			tprint("Done. 'cub demo status' compares the org against the scenario.")
			return nil
		},
	}
	c.Flags().StringSliceVar(&phases, "phase", nil, "phases to run (default all): "+phaseList())
	c.Flags().IntVar(&concurrency, "concurrency", 8, "parallel API calls within a phase")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be created, change nothing")
	return c
}

func phaseList() string {
	out := ""
	for i, p := range seed.Phases {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// printContext says which server and context a mutating command acts on,
// because plugins inherit whatever the active cub context is.
func printContext(m interface{ Name() string }) {
	tprint("Scenario %s -> server %s (cub context %q)", m.Name(), cub.Server(), os.Getenv("CUB_CONTEXT"))
}

// checkContext enforces a scenario's context pin. With several demos in
// several orgs, the failure mode is running up or down against the wrong org;
// a pinned scenario refuses instead.
func checkContext(m interface {
	Name() string
}, pinned string) error {
	active := os.Getenv("CUB_CONTEXT")
	if pinned != "" && active != pinned {
		return fmt.Errorf("scenario %q is pinned to cub context %q, but the active context is %q; run 'cub context use %s' first",
			m.Name(), pinned, active, pinned)
	}
	return nil
}
