package seed

import (
	"encoding/json"
	"fmt"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/cub-demo/internal/scenario"
)

// chunk sizes: bulk create is one transaction per request, so one bad entry
// fails the whole chunk; these keep a chunk's blast radius and payload small.
const (
	spaceChunk = 40
	unitChunk  = 15
)

// deployments creates every deployment space (a clone of the component's class
// base, targeted at a cluster) in four steps: spaces, per-cluster patches
// (ReleaseTargetID + cluster labels), units, unit labels. Steps are sequential
// because each depends on the previous; work inside a step is parallel.
func (s *Seeder) deployments() error {
	// Components whose bases exist; the rest were skipped for missing
	// manifests (or the bases phase has not run).
	type pair struct {
		cm *scenario.ComponentModel
		cb *scenario.ClassBase
	}
	var pairs []pair
	skipped := 0
	for _, cm := range s.Model.Components {
		ready := cm.HasManifests
		if !s.DryRun {
			ready = ready && s.space(cm.RootSpace) != nil
			for _, cb := range cm.ClassBases {
				ready = ready && s.space(cb.Space) != nil
			}
		}
		if !ready {
			skipped++
			continue
		}
		for _, cb := range cm.ClassBases {
			pairs = append(pairs, pair{cm, cb})
		}
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would ensure deployments for %d (component, class) pairs%s\n", len(pairs), skippedNote(skipped))
		return nil
	}

	// The cluster's target is what deployment spaces release to.
	targets, err := s.Client.ListTargetsAll(demoWhere(s.Model))
	if err != nil {
		return fmt.Errorf("list targets: %w", err)
	}
	targetBySpaceID := map[string]string{}
	for _, t := range targets {
		targetBySpaceID[t.SpaceID.String()] = t.TargetID.String()
	}
	targetByCluster := map[string]string{}
	for _, c := range s.Model.Clusters {
		sp := s.space(c.Name)
		if sp == nil {
			return fmt.Errorf("cluster space %s does not exist; run the clusters phase first", c.Name)
		}
		tid, ok := targetBySpaceID[sp.SpaceID.String()]
		if !ok {
			return fmt.Errorf("cluster %s has no target; run the clusters phase first", c.Name)
		}
		targetByCluster[c.Name] = tid
	}

	// Step 1: deployment spaces, cloned from the class base with the cluster
	// name as the Variant label.
	err = forEach(s, pairs, func(p pair) error {
		var missing []string
		for _, d := range p.cm.Deployments {
			if d.Class == p.cb.Class && s.space(d.Space) == nil {
				missing = append(missing, d.Cluster.Name)
			}
		}
		base := s.space(p.cb.Space)
		for _, group := range chunks(missing, spaceChunk) {
			where := fmt.Sprintf("SpaceID = '%s'", base.SpaceID)
			variantLabels := "Variant=" + strings.Join(group, "|")
			pattern := componentVariantPattern
			allow := "true"
			patch, _ := json.Marshal(map[string]any{"Annotations": map[string]string{"UpstreamSpaceID": base.SpaceID.String()}})
			responses, err := s.Client.BulkCreateSpaces(&goclient.BulkCreateSpacesParams{
				Where: &where, VariantLabels: &variantLabels, NamePattern: &pattern, AllowExists: &allow,
			}, patch)
			if err != nil {
				return fmt.Errorf("%s %s spaces: %w", p.cm.Name, p.cb.Class, err)
			}
			for i := range responses {
				if responses[i].Space != nil {
					s.remember(responses[i].Space)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Step 2: per cluster, one bulk patch turns its clones into deployments:
	// Role, the cluster labels, and the ReleaseTargetID every unit clone will
	// inherit as its target.
	err = forEach(s, s.Model.Clusters, func(c *scenario.Cluster) error {
		labels := map[string]string{
			"Role":    "deployment",
			"Cluster": c.Name,
			"Region":  c.Region,
		}
		if !c.Shared() {
			labels["Department"] = c.Department
		}
		patch, _ := json.Marshal(map[string]any{"Labels": labels, "ReleaseTargetID": targetByCluster[c.Name]})
		where := fmt.Sprintf("%s AND Labels.Variant = '%s'", demoWhere(s.Model), c.Name)
		if err := s.Client.BulkPatchSpaces(where, patch); err != nil {
			return fmt.Errorf("cluster %s space patch: %w", c.Name, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Step 3: units, cloned from the class base into its deployment spaces.
	// The patch is null: with no TargetID given, each clone takes its space's
	// ReleaseTargetID, which step 2 just set.
	err = forEach(s, pairs, func(p pair) error {
		base := s.space(p.cb.Space)
		var slugs []string
		for _, d := range p.cm.Deployments {
			if d.Class == p.cb.Class {
				slugs = append(slugs, "'"+d.Space+"'")
			}
		}
		for _, group := range chunks(slugs, unitChunk) {
			where := fmt.Sprintf("SpaceID = '%s'", base.SpaceID)
			whereSpace := fmt.Sprintf("%s AND Labels.Component = '%s' AND Labels.Role = 'deployment' AND Slug IN (%s)",
				demoWhere(s.Model), p.cm.Name, strings.Join(group, ", "))
			allow := "true"
			include := "UpstreamUnitID,SpaceID,TargetID"
			_, err := s.Client.BulkCreateUnits(&goclient.BulkCreateUnitsParams{
				Where: &where, WhereSpace: &whereSpace, AllowExists: &allow, Include: &include,
			}, []byte("null"))
			if err != nil {
				return fmt.Errorf("%s %s units: %w", p.cm.Name, p.cb.Class, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Step 4: unit labels per cluster, so org-wide unit queries can group by
	// cluster, region and department.
	err = forEach(s, s.Model.Clusters, func(c *scenario.Cluster) error {
		labels := map[string]string{
			"Cluster":     c.Name,
			"Region":      c.Region,
			"Stage":       c.Class,
			"Environment": c.Class,
		}
		if !c.Shared() {
			labels["Department"] = c.Department
		}
		patch, _ := json.Marshal(map[string]any{"Labels": labels})
		where := fmt.Sprintf("%s AND Space.Labels.Cluster = '%s' AND Space.Labels.Role = 'deployment'", demoWhere(s.Model), c.Name)
		if err := s.Client.BulkPatchUnits(where, patch); err != nil {
			return fmt.Errorf("cluster %s unit labels: %w", c.Name, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Step 5: per-region and per-cluster variation, including pointing every
	// Crossplane resource at its deployment's cloud region.
	seen := map[string]bool{}
	var comps []*scenario.ComponentModel
	for _, p := range pairs {
		if !seen[p.cm.Name] {
			seen[p.cm.Name] = true
			comps = append(comps, p.cm)
		}
	}
	err = forEach(s, comps, func(cm *scenario.ComponentModel) error {
		if err := s.deploymentFunctions(cm); err != nil {
			return fmt.Errorf("%s deployment functions: %w", cm.Name, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "  ensured deployments for %d (component, class) pairs%s\n", len(pairs), skippedNote(skipped))
	// A clone starts unprotected, so the deployments get their protected paths
	// here, right after they exist; the class bases got theirs before the clone
	// (the protections phase), so the clones already sit at that revision.
	return s.protect(protectDeployments)
}

// chunks splits items into groups of at most n.
func chunks(items []string, n int) [][]string {
	var out [][]string
	for len(items) > n {
		out = append(out, items[:n])
		items = items[n:]
	}
	if len(items) > 0 {
		out = append(out, items)
	}
	return out
}
