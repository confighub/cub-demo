package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/present"
)

// presenter loads the selected scenario, checks the context pin and returns
// the verbs' shared state. Every presenter verb starts here.
func presenter(dryRun bool) (*present.Presenter, error) {
	m, _, err := loadInstalled()
	if err != nil {
		return nil, err
	}
	if err := checkContext(m, m.Scenario.Context); err != nil {
		return nil, err
	}
	return &present.Presenter{Client: cub, Model: m, Out: os.Stdout, DryRun: dryRun}, nil
}

// addSelectorFlags gives a verb the three ways to pick deployment spaces.
func addSelectorFlags(c *cobra.Command, sel *present.Selector) {
	c.Flags().StringVar(&sel.Component, "component", "", "only this component's deployments")
	c.Flags().StringSliceVar(&sel.Variants, "variant", nil, "only these variants (cluster names); repeatable")
	c.Flags().StringVar(&sel.Stage, "stage", "", "only the spaces this stage of the rollout selects; needs --component (its change order in flight) or --change-order")
	c.Flags().StringVar(&sel.ChangeOrder, "change-order", "", "the change order a --stage is resolved through, as space/slug; default: --component's single change order in flight")
}
