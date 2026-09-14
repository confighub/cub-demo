package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/seed"
)

func newStatusCmd() *cobra.Command {
	c := &cobra.Command{
		Use:               "status",
		Short:             "Compare what exists in the org with what the scenario describes",
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
			s := &seed.Seeder{Client: cub, Model: m, Out: os.Stdout}
			gaps, err := s.Status()
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
			complete := true
			for _, g := range gaps {
				mark := "ok"
				if g.Have < g.Want {
					complete = false
					mark = "incomplete"
					if len(g.Missing) > 0 {
						mark += " (missing " + strings.Join(g.Missing, " ")
						if g.Want-g.Have > len(g.Missing) {
							mark += " ..."
						}
						mark += ")"
					}
				}
				tprintTw(tw, "  %s\t%d/%d\t%s", g.Phase, g.Have, g.Want, mark)
			}
			tw.Flush()
			if complete {
				tprint("Everything the implemented phases create exists.")
			}
			return nil
		},
	}
	return c
}

// tprintTw writes one tabwriter row.
func tprintTw(w *tabwriter.Writer, format string, args ...any) {
	fmt.Fprintf(w, strings.Trim(format, "\n")+"\n", args...)
}
