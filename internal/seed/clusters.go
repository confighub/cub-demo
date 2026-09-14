package seed

import (
	"fmt"
	"strconv"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/cub-demo/internal/scenario"
)

// TargetSlug is the single OCI target inside each cluster space, matching the
// "cub cluster up" convention of one target per cluster space.
const TargetSlug = "cluster"

// clusterLabels are the well-known labels on a cluster space: the class doubles
// as Environment and Stage, and the cluster marker matches "cub cluster up".
func clusterLabels(s *Seeder, c *scenario.Cluster) map[string]string {
	labels := s.baseLabels(map[string]string{
		"confighub.com/cluster": "true",
		"Layer":                 "cluster",
		"Cluster":               c.Name,
		"Region":                c.Region,
		"Environment":           c.Class,
		"Stage":                 c.Class,
	})
	if !c.Shared() {
		labels["Department"] = c.Department
	}
	return labels
}

// clusters creates one space and one OCI target per cluster. The target's
// facts carry the cluster's properties, so the fleet is queryable by
// Kubernetes version, cloud region, class and department.
func (s *Seeder) clusters() error {
	if s.workerID == uuidNil && !s.DryRun {
		// The home phase was skipped; resolve the worker from the org.
		home := s.space(s.Model.Home)
		if home == nil {
			return fmt.Errorf("home space %s does not exist; run the home phase first", s.Model.Home)
		}
		worker, err := s.Client.WorkerBySlug(home.SpaceID, WorkerSlug)
		if err != nil {
			return err
		}
		if worker == nil {
			return fmt.Errorf("worker %s/%s does not exist; run the home phase first", s.Model.Home, WorkerSlug)
		}
		s.workerID = worker.BridgeWorkerID
	}

	created := 0
	err := forEach(s, s.Model.Clusters, func(c *scenario.Cluster) error {
		madeSpace, madeTarget, err := s.cluster(c)
		if err != nil {
			return fmt.Errorf("cluster %s: %w", c.Name, err)
		}
		if madeSpace || madeTarget {
			s.mu.Lock()
			created++
			s.mu.Unlock()
		}
		return nil
	})
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would create %d of %d clusters (rest exist)\n", created, len(s.Model.Clusters))
		return err
	}
	fmt.Fprintf(s.Out, "  ensured %d clusters (%d created or completed)\n", len(s.Model.Clusters), created)
	return err
}

// cluster ensures one cluster's space and target.
func (s *Seeder) cluster(c *scenario.Cluster) (bool, bool, error) {
	display := fmt.Sprintf("%s %s %s%d", s.Model.Scenario.Company, c.Region, c.Class, c.Index)
	if !c.Shared() {
		display = fmt.Sprintf("%s %s %s %s%d", s.Model.Scenario.Company, c.Region, c.Department, c.Class, c.Index)
	}
	sp, madeSpace, err := s.ensureSpace(c.Name, display, clusterLabels(s, c))
	if err != nil {
		return false, false, err
	}
	if s.DryRun {
		return madeSpace, false, nil
	}

	// The target may be missing even when the space exists (an interrupted
	// earlier run), so it is checked independently.
	target, err := s.Client.TargetBySlug(sp.SpaceID, TargetSlug)
	if err != nil {
		return madeSpace, false, err
	}
	if target != nil {
		return madeSpace, false, nil
	}
	facts := map[string]string{
		"Cluster.Name":              c.Name,
		"Cluster.KubernetesVersion": c.KubernetesVersion,
		"Cluster.Class":             c.Class,
		"Cluster.NodeCount":         strconv.Itoa(c.NodeCount),
		"Cloud.Provider":            s.Model.Scenario.Cloud,
		"Cloud.Region":              c.ProviderRegion,
	}
	if !c.Shared() {
		facts["Cluster.Department"] = c.Department
	}
	_, err = s.Client.CreateTarget(sp.SpaceID, goclient.Target{
		SpaceID:        sp.SpaceID,
		Slug:           TargetSlug,
		DisplayName:    c.Name,
		BridgeWorkerID: s.workerID,
		ProviderType:   "OCI",
		ToolchainType:  "Any",
		Parameters:     "{}",
		Labels:         clusterLabels(s, c),
		Facts:          facts,
		WhereTrigger:   fmt.Sprintf("SpaceID = '%s'", sp.SpaceID),
	})
	if err != nil {
		return madeSpace, false, err
	}
	return madeSpace, true, nil
}
