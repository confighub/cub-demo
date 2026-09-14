package cmd

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/confighub/cub-demo/internal/cubclient"
	"github.com/confighub/cub-demo/internal/present"
)

// version is stamped by the build (see Makefile LDFLAGS).
var version = "dev"

// Version returns the plugin version.
func Version() string { return version }

// cub is the ConfigHub API client, initialized by ensureClient for the
// subcommands that talk to the server. Offline commands (plan, scenario,
// version) never construct it.
var cub *cubclient.Client

// assets holds the embedded scenarios and manifests, handed in by main.
var assets fs.FS

// demoName is the --demo flag: which installed demo to operate on, when the
// org holds several.
var demoName string

// NewRootCmd builds the demo command tree. The plugin contributes the single
// top-level command "demo"; cub invokes this binary with the subcommand as the
// first argument (e.g. "cub demo up ..." runs "<binary> up ...").
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "demo",
		Short: "Seed a ConfigHub org with a fleet-scale demo dataset",
		Long: `Seed a ConfigHub org with a fleet-scale demo dataset.

A scenario file describes a fictional company: its regions, cluster classes
(dev/test/uat/prod), departments, the platform components every cluster runs,
the business workloads and where they are placed, and the cloud resources those
workloads need. "cub demo up" expands the scenario into ConfigHub spaces,
targets, components, variants and releases; nothing is provisioned. The
embedded default scenario is a global enterprise with ~100 clusters.

The scenario definition lives in the org itself: "install" stores it there,
and every other verb reads it back, so the org is self-describing and any
machine with this plugin can operate it.

  cub demo scenario list        browse the embedded sample scenarios
  cub demo plan <scenario>      show what a definition expands to, offline
  cub demo install <scenario>   store the definition in the active org
  cub demo up                   converge the org to the installed definition
  cub demo status               compare what exists with what it describes
  cub demo play [<play>]        run one of the scenario's plays (presenting)
  cub demo down                 delete the dataset (the definition stays installed)
  cub demo reset                down + up, back to the opening state
  cub demo uninstall            delete the dataset and the definition

To change a running demo, edit an exported copy, "install" it again (the
bundle takes new revisions), and "up" reconciles the org to it. When the org
holds several demos, --demo says which one a verb operates on.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true, // Execute prints the error once
	}
	// A verb run from inside "cub demo play" inherits the demo name through
	// the environment, so a play never breaks in a multi-demo org.
	root.PersistentFlags().StringVar(&demoName, "demo", os.Getenv(present.DemoEnv),
		"which installed demo to operate on; default: the one demo installed in the active org (or $"+present.DemoEnv+")")
	root.AddCommand(newPlanCmd(), newInstallCmd(), newUpCmd(), newStatusCmd(), newDownCmd(), newResetCmd(), newUninstallCmd(), newListCmd(), newScenarioCmd(), newVersionCmd())
	root.AddCommand(newPlayCmd())
	return root
}

// Execute runs the command tree with the embedded assets and exits non-zero
// on error.
func Execute(embedded fs.FS) {
	assets = embedded
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// ensureClient initializes the shared ConfigHub client. It is the
// PersistentPreRunE of every subcommand that talks to the server.
func ensureClient(cmd *cobra.Command, _ []string) error {
	c, err := cubclient.New(context.Background())
	if err != nil {
		return err
	}
	cub = c
	return nil
}

// tprint writes an informational line to stdout, normalizing trailing newlines.
func tprint(format string, args ...any) {
	format = strings.Trim(format, "\n") + "\n"
	fmt.Printf(format, args...)
}
