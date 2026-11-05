package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/agents"
	"github.com/confighub/cub-demo/internal/cubexec"
)

func newAgentsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "agents",
		Short: "Run the scenario's autonomous agents",
		Long: `The scenario's agents, declared under "agents:": each is a service account
that repeatedly makes a change to one component inside a ChangeSet under the
review workflow, closes it when the workflow lets it, and rolls it out as a
change order. "up" creates their accounts and signs in a cub context for each;
"run" lets them act; "down" removes the accounts and contexts.

  cub demo agents up               create the agents' accounts and contexts
  cub demo agents run              let the agents act until interrupted
  cub demo agents run --once       one move each, then stop
  cub demo agents down             remove the accounts and contexts`,
		PersistentPreRunE: ensureClient,
	}
	c.AddCommand(newAgentsUpCmd(), newAgentsRunCmd(), newAgentsDownCmd())
	return c
}

func agentsManager(dryRun bool) (*agents.Manager, error) {
	p, err := presenter(dryRun)
	if err != nil {
		return nil, err
	}
	return &agents.Manager{Model: p.Model, Out: os.Stdout, Server: cub.Server(), DryRun: dryRun}, nil
}

func newAgentsUpCmd() *cobra.Command {
	var dryRun bool
	c := &cobra.Command{
		Use:   "up",
		Short: "Create the agents' service accounts and sign in a cub context for each",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := agentsManager(dryRun)
			if err != nil {
				return err
			}
			printContext(m.Model)
			return m.Up()
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print the commands, run nothing")
	return c
}

func newAgentsDownCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "down",
		Short: "Delete the agents' service accounts and cub contexts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := agentsManager(false)
			if err != nil {
				return err
			}
			printContext(m.Model)
			if !yes {
				var names []string
				for _, a := range m.Model.Scenario.Agents {
					names = append(names, m.AccountName(a))
				}
				tprint("This deletes the service accounts %s and their cub contexts. Re-run with --yes to proceed.", strings.Join(names, ", "))
				return nil
			}
			return m.Down()
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "proceed without asking")
	return c
}

func newAgentsRunCmd() *cobra.Command {
	var once bool
	var reviewer string
	var tick time.Duration
	c := &cobra.Command{
		Use:   "run",
		Short: "Let the agents act",
		Long: `Let the agents act, each whenever its interval is up, printing every cub
command they run and what they are waiting on. A change the review workflow
refuses to close is handed to --reviewer, who approves it in the UI or with
"cub variant approve <base> --all --revision ChangeSet:<base>/<changeset>";
the agent closes it on its next move. Default reviewer: the reviewers the
scenario lists under workflows.review, or the user of the active context when
it lists none. Ctrl-C stops the run; a later run picks up where the org stands.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := presenter(false)
			if err != nil {
				return err
			}
			if review := p.Model.Scenario.Workflows.Review; reviewer == "" && review != nil {
				reviewer = strings.Join(review.Reviewers, ",")
			}
			if reviewer == "" {
				reviewer, _ = cubexec.Run("context", "get", "-o", "jq=.coordinate.user")
				reviewer = strings.TrimSpace(strings.Trim(reviewer, "\""))
			}
			m := &agents.Manager{Model: p.Model, Out: os.Stdout, Server: cub.Server()}
			printContext(p.Model)
			if reviewer != "" {
				tprint("Reviews are requested from %s", reviewer)
			}
			r := &agents.Runner{P: p, Manager: m, Reviewer: reviewer, Out: os.Stdout, Tick: tick}
			if err := r.Run(once); err != nil {
				return err
			}
			if once {
				fmt.Println("Each agent made one move.")
			}
			return nil
		},
	}
	c.Flags().BoolVar(&once, "once", false, "one move per agent, then stop")
	c.Flags().StringVar(&reviewer, "reviewer", "", "who to ask to review a change the workflow refuses to close; default: the scenario's reviewers, or the active context's user")
	c.Flags().DurationVar(&tick, "tick", 2*time.Second, "how often the runner checks which agents are due")
	return c
}
