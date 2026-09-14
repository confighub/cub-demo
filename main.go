// Command cub-demo seeds a ConfigHub organization with a realistic,
// fleet-scale demo dataset, installed as a cub CLI plugin from a local build:
//
//	make plugin   (clones of this repo; wraps: cub plugin install ./bin/cub-demo)
//	cub demo plan
//	cub demo up
//
// The dataset is described by a scenario file; the embedded default is a
// global enterprise with ~100 clusters, a platform-component catalog, sparsely
// placed business workloads and Crossplane-modelled cloud resources. Nothing is
// provisioned: clusters are ConfigHub Targets on a server-hosted worker, and
// "live" state is written as the annotations a GitOps operator would report.
// See docs/DESIGN.md.
package main

import (
	"fmt"
	"os"

	"github.com/confighub/sdk/core/plugin"

	"github.com/confighub/cub-demo/cmd"
)

func main() {
	// cub invokes this binary with the hook environment set when installing or
	// upgrading the plugin. HandleHook writes cub-plugin.yaml into the plugin
	// directory and we exit before the command tree runs, so the manifest can
	// never drift from the commands actually implemented.
	manifest := plugin.Manifest{
		Name:    "demo",
		Version: cmd.Version(),
		Commands: []plugin.Command{{
			Name:    "demo",
			Summary: "Seed a ConfigHub org with a fleet-scale demo dataset",
		}},
	}
	if handled, err := plugin.HandleHook(manifest); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	cmd.Execute(assets)
}
