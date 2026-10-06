package seed

import (
	"fmt"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
)

// Teardown deletes every space of the demo, most-dependent first, because the
// server refuses to delete what something else still refers to: deployments
// (whose units link to their class base's and whose ReleaseTargetID names a
// cluster target), then class bases (whose units link to the root's), then
// root bases, then the Component entities no space names any more, then
// cluster spaces (targets die with their space), then the home space. Space deletion is recursive,
// taking units, links and releases with it. With keepDefinition (down, reset) the <name>-scenario
// space survives, so the demo stays installed and up can re-create the
// dataset; uninstall passes false and removes the definition too.
func (s *Seeder) Teardown(force, keepDefinition bool) error {
	if err := s.loadIndex(); err != nil {
		return err
	}
	claimed := map[string]bool{}
	defSlug := s.Model.Scenario.Name + "-scenario"
	if keepDefinition {
		if s.spaces[defSlug] != nil {
			claimed[defSlug] = true
			fmt.Fprintf(s.Out, "  keeping the installed definition (%s); 'cub demo uninstall' removes it\n", defSlug)
		}
	}
	groups := []struct {
		name string
		pick func(*goclient.Space) bool
	}{
		{"deployment", func(sp *goclient.Space) bool { return sp.Labels["Role"] == "deployment" }},
		{"class base", func(sp *goclient.Space) bool { return sp.Labels["Role"] == "base" && sp.Labels["Variant"] != "base" }},
		{"root base", func(sp *goclient.Space) bool { return sp.Labels["Role"] == "base" }},
		{"components", nil},
		{"cluster", func(sp *goclient.Space) bool { return sp.Labels["Layer"] == "cluster" }},
		{"home", func(sp *goclient.Space) bool { return sp.Labels["Layer"] == "demo" }},
	}
	for _, g := range groups {
		if g.pick == nil {
			if err := s.deleteComponents(); err != nil {
				return err
			}
			continue
		}
		var batch []*goclient.Space
		for slug, sp := range s.spaces {
			if !claimed[slug] && g.pick(sp) {
				claimed[slug] = true
				batch = append(batch, sp)
			}
		}
		if len(batch) == 0 {
			continue
		}
		if s.DryRun {
			fmt.Fprintf(s.Out, "  would delete %d %s spaces\n", len(batch), g.name)
			continue
		}
		err := forEach(s, batch, func(sp *goclient.Space) error {
			err := s.Client.DeleteSpace(sp.SpaceID, force)
			// Change-order start/end tags gate deletion (server behavior since
			// 2026-09; any story-seeded base trips it). The tags are the
			// demo's own, so this one gate is passed with force rather than
			// making every teardown of a scenario with stories a two-step.
			if err != nil && !force && strings.Contains(err.Error(), "revisions tagged") {
				fmt.Fprintf(s.Out, "  %s: revisions tagged by the demo's change orders; deleting with force\n", sp.Slug)
				err = s.Client.DeleteSpace(sp.SpaceID, true)
			}
			if err != nil {
				return fmt.Errorf("delete %s: %w", sp.Slug, err)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("deleting %s: %w", g.name, err)
		}
		fmt.Fprintf(s.Out, "  deleted %d %s spaces\n", len(batch), g.name)
	}
	var leftovers []string
	for slug := range s.spaces {
		if !claimed[slug] {
			leftovers = append(leftovers, slug)
		}
	}
	if len(leftovers) > 0 {
		return fmt.Errorf("%d spaces carry the demo label but none of its layer/role labels; not deleting them: %v", len(leftovers), leftovers)
	}
	return nil
}

// deleteComponents deletes the demo's Component entities. A Component is in
// no space, so no space deletion takes it; it goes once its variants have.
func (s *Seeder) deleteComponents() error {
	components := make([]*goclient.Component, 0, len(s.components))
	for _, c := range s.components {
		components = append(components, c)
	}
	if len(components) == 0 {
		return nil
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would delete %d components\n", len(components))
		return nil
	}
	err := forEach(s, components, func(c *goclient.Component) error {
		if err := s.Client.DeleteComponent(c.ComponentID); err != nil {
			return fmt.Errorf("delete component %s: %w", c.Slug, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("deleting components: %w", err)
	}
	fmt.Fprintf(s.Out, "  deleted %d components\n", len(components))
	return nil
}

// Gap is one category of missing entities reported by Status.
type Gap struct {
	Phase   string
	Want    int
	Have    int
	Missing []string // up to ten examples
}

// Status compares what exists with what the model describes.
func (s *Seeder) Status() ([]Gap, error) {
	if err := s.loadIndex(); err != nil {
		return nil, err
	}
	var gaps []Gap

	haveHome := 0
	if s.space(s.Model.Home) != nil {
		haveHome = 1
	}
	gaps = append(gaps, gap("home space", []string{s.Model.Home}, haveHome == 1, func(slug string) bool { return s.space(slug) != nil }))

	clusterSlugs := make([]string, 0, len(s.Model.Clusters))
	for _, c := range s.Model.Clusters {
		clusterSlugs = append(clusterSlugs, c.Name)
	}
	gaps = append(gaps, gap("cluster spaces", clusterSlugs, false, func(slug string) bool { return s.space(slug) != nil }))

	// Targets are counted org-wide by the demo label rather than fetched per
	// space, which would be one request per cluster.
	targets, err := s.Client.CountTargets(demoWhere(s.Model))
	if err != nil {
		return nil, err
	}
	gaps = append(gaps, Gap{Phase: "cluster targets", Want: len(s.Model.Clusters), Have: targets})

	var components, roots, classBases, deployments []string
	wantUnits := 0
	pending := 0
	for _, cm := range s.Model.Components {
		if !cm.HasManifests {
			// Listed in the scenario ahead of its content; not a gap.
			pending++
			continue
		}
		components = append(components, cm.Name)
		roots = append(roots, cm.RootSpace)
		for _, cb := range cm.ClassBases {
			classBases = append(classBases, cb.Space)
		}
		for _, d := range cm.Deployments {
			deployments = append(deployments, d.Space)
		}
		wantUnits += len(cm.Units) * (1 + len(cm.ClassBases) + len(cm.Deployments))
	}
	exists := func(slug string) bool { return s.space(slug) != nil }
	gaps = append(gaps,
		gap("components", components, false, func(slug string) bool { return s.components[slug] != nil }),
		gap("root bases", roots, false, exists),
		gap("class bases", classBases, false, exists),
		gap("deployment spaces", deployments, false, exists))

	units, err := s.Client.CountUnits(demoWhere(s.Model))
	if err != nil {
		return nil, err
	}
	gaps = append(gaps, Gap{Phase: "units (org-wide)", Want: wantUnits, Have: units})

	releases, err := s.Client.CountReleases(demoWhere(s.Model))
	if err != nil {
		return nil, err
	}
	wantReleases := len(deployments) - overlap(deployments, s.Model.Story.Unreleased)
	gaps = append(gaps, Gap{Phase: "released deployments", Want: wantReleases, Have: releases})

	// Live status is the latest Release's, so only a released deployment can
	// have one; components the scenario leaves unreported are not counted.
	latest, err := s.Client.LatestReleases(fmt.Sprintf("Space.Labels.%s = '%s'", LabelDemoName, s.Model.Scenario.Name))
	if err != nil {
		return nil, err
	}
	reportable, reported := 0, 0
	for _, cm := range s.Model.Components {
		if !cm.HasManifests || cm.LiveStatus == "none" {
			continue
		}
		for _, d := range cm.Deployments {
			sp := s.space(d.Space)
			if sp == nil || latest[sp.SpaceID] == nil {
				continue
			}
			reportable++
			if latest[sp.SpaceID].LiveStatus != nil {
				reported++
			}
		}
	}
	gaps = append(gaps, Gap{Phase: "live status reported", Want: reportable, Have: reported})
	if pending > 0 {
		gaps = append(gaps, Gap{Phase: fmt.Sprintf("(%d components pending content)", pending)})
	}
	return gaps, nil
}

// overlap counts how many of the wanted slugs appear in the story set.
func overlap(want []string, set []string) int {
	in := map[string]bool{}
	for _, s := range set {
		in[s] = true
	}
	n := 0
	for _, w := range want {
		if in[w] {
			n++
		}
	}
	return n
}

func gap(phase string, want []string, _ bool, have func(string) bool) Gap {
	g := Gap{Phase: phase, Want: len(want)}
	for _, slug := range want {
		if have(slug) {
			g.Have++
		} else if len(g.Missing) < 10 {
			g.Missing = append(g.Missing, slug)
		}
	}
	return g
}
