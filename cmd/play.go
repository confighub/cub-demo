package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

func newPlayCmd() *cobra.Command {
	var dryRun bool
	c := &cobra.Command{
		Use:   "play [<play>]",
		Short: "Run one of the scenario's plays",
		Long: `Run one of the scenario's plays: a named sequence of steps under "plays:" in
the scenario file. A step is a shell line (run:) or a call to one of the
tool's primitives (call: observe, invoke, bump-image, changeorder; see the
scenario schema). Strings are Go templates over the scenario (Base and
Workflow resolve a component's names) plus the variables earlier steps export
(bump-image exports .Version). Steps run in order with CUB_CONTEXT pinned to
the scenario's context, and the first failure stops the run. With no name,
the plays are listed.

  cub demo play                # list
  cub demo play ci             # run the play the scenario names "ci"
  cub demo play ci --dry-run   # walk the steps, changing nothing`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The scenario may have to be found in the org, so the client is
			// needed even to list; running a move needs it anyway.
			if err := ensureClient(cmd, args); err != nil {
				return err
			}
			p, err := presenter(dryRun)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				names := p.Plays()
				if len(names) == 0 {
					tprint("scenario %s declares no plays", p.Model.Name())
					return nil
				}
				for _, name := range names {
					lines, err := p.Describe(name)
					if err != nil {
						return err
					}
					tprint("%s\n  %s", name, strings.Join(lines, "\n  "))
				}
				return nil
			}
			return p.Play(args[0])
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the rendered lines, run nothing")
	return c
}
