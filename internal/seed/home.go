package seed

import (
	"fmt"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
)

// WorkerSlug is the shared server-hosted worker every cluster target binds to.
const WorkerSlug = "server-worker"

// home creates the home space and the server-hosted OCI worker. The worker is
// server-hosted, so it reports Ready with no process behind it; its OrgRole is
// "none" because nothing ever runs as it.
func (s *Seeder) home() error {
	m := s.Model
	sp, created, err := s.ensureSpace(m.Home, m.Scenario.Company+" platform", s.baseLabels(map[string]string{"Layer": "demo"}))
	if err != nil {
		return err
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would ensure home space %s and worker %s\n", m.Home, WorkerSlug)
		return nil
	}
	if created {
		fmt.Fprintf(s.Out, "  created home space %s\n", m.Home)
	}

	worker, err := s.Client.WorkerBySlug(sp.SpaceID, WorkerSlug)
	if err != nil {
		return err
	}
	if worker == nil {
		worker, err = s.Client.CreateWorker(sp.SpaceID, goclient.BridgeWorker{
			SpaceID:     sp.SpaceID,
			Slug:        WorkerSlug,
			DisplayName: "Demo server worker",
			OrgRole:     "none",
			Labels:      s.baseLabels(nil),
			ProvidedInfo: &goclient.WorkerInfo{
				IsServerWorker: true,
				BridgeWorkerInfo: &goclient.BridgeWorkerInfo{
					SupportedConfigTypes: []goclient.SupportedConfigType{
						{ProviderType: "OCI", ToolchainType: "Any"},
					},
				},
			},
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(s.Out, "  created worker %s/%s (server-hosted)\n", m.Home, WorkerSlug)
	}
	s.workerID = worker.BridgeWorkerID
	return nil
}
