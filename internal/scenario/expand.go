package scenario

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strings"
)

// Model is the fully expanded scenario: every cluster, space and unit the
// seeder will create, in creation order. Everything "plan" prints and "up"
// creates comes from here, so the two can never disagree.
type Model struct {
	Scenario   *Scenario
	Home       string
	Clusters   []*Cluster
	Components []*ComponentModel
	Story      StorySelection
	Totals     Totals
}

// Cluster is one Kubernetes cluster: a space plus an OCI target.
type Cluster struct {
	Name              string
	Region            string
	ProviderRegion    string
	Class             string
	Department        string
	Index             int
	KubernetesVersion string
	NodeCount         int
}

// Shared reports whether the cluster belongs to no department.
func (c *Cluster) Shared() bool { return c.Department == "" || c.Department == "shared" }

// ComponentModel is a component's variant tree.
type ComponentModel struct {
	Name       string
	Layer      string // platform | workload
	Owner      string
	Department string
	// HasManifests reports whether every unit's manifest file is present. A
	// scenario may list components ahead of their content; the seeder skips
	// those and status reports them as pending rather than missing.
	HasManifests bool
	Spec         *ComponentSpec
	LiveStatus   string // Component.LiveStatus: "" (fleet default) or "none"
	// Units are the unit slugs of the root base: the manifest units followed
	// by one per external resource.
	Units []string
	// External are the component's cloud resources.
	External []External
	// RootSpace is the root base slug; ClassBases exist only for classes that
	// have at least one deployment.
	RootSpace   string
	ClassBases  []*ClassBase
	Deployments []*Deployment
}

// ClassBase is the per-class base variant.
type ClassBase struct {
	Class string
	Space string
}

// Deployment is a deployment variant on one cluster.
type Deployment struct {
	Space     string
	Component string
	Class     string
	Cluster   *Cluster
}

// StorySelection is the deterministic set of deployment spaces given each
// story treatment. The sets are disjoint.
type StorySelection struct {
	Degraded    []string
	OutOfSync   []string
	Progressing []string
	Unreleased  []string
}

// Totals are the entity counts "up" will produce.
type Totals struct {
	Clusters, Targets, Workers                 int
	Spaces, RootBases, ClassBases, Deployments int
	Units, Links, Releases                     int
	// FunctionCalls is the number of InvokeFunctionsOnOrg requests the
	// component specs imply.
	FunctionCalls int
}

// Expand computes the model. It is deterministic: iteration follows scenario
// order everywhere, and story selection uses a seeded generator over sorted
// input.
func Expand(b *Bundle) (*Model, error) {
	s := b.Scenario
	m := &Model{Scenario: s, Home: s.Name + "-platform"}

	clusters, err := expandClusters(s)
	if err != nil {
		return nil, err
	}
	m.Clusters = clusters

	for _, c := range s.Catalog {
		cm, err := expandComponent(s, b.Specs[c.Name], c, "platform", clusters)
		if err != nil {
			return nil, err
		}
		cm.HasManifests = hasManifests(b, cm)
		m.Components = append(m.Components, cm)
	}
	for _, c := range s.Workloads {
		cm, err := expandComponent(s, b.Specs[c.Name], c, "workload", clusters)
		if err != nil {
			return nil, err
		}
		cm.HasManifests = hasManifests(b, cm)
		m.Components = append(m.Components, cm)
	}

	m.Story = selectStory(s.Story, m.Components)
	m.Totals = total(m)
	return m, nil
}

// Name returns the scenario name (the DemoName label value).
func (m *Model) Name() string { return m.Scenario.Name }

// Deployment looks up a deployment by space slug.
func (m *Model) Deployment(space string) *Deployment {
	for _, c := range m.Components {
		for _, d := range c.Deployments {
			if d.Space == space {
				return d
			}
		}
	}
	return nil
}

// hasManifests probes every unit file of the component, including the shared
// templates its external resources need.
func hasManifests(b *Bundle, cm *ComponentModel) bool {
	for _, u := range cm.Spec.Units {
		if _, err := b.Open(cm.Name, u.File); err != nil {
			return false
		}
	}
	for _, e := range cm.External {
		if _, err := b.ExternalTemplate(e.Kind); err != nil {
			return false
		}
	}
	return true
}

func expandClusters(s *Scenario) ([]*Cluster, error) {
	classOrder := indexOf(classNames(s))
	regionOrder := map[string]int{}
	regionSpec := map[string]Region{}
	for i, r := range s.Regions {
		regionOrder[r.Name] = i
		regionSpec[r.Name] = r
	}
	deptOrder := map[string]int{"": 0}
	for i, d := range s.Departments {
		deptOrder[d] = i + 1
	}
	classSpec := map[string]Class{}
	for _, c := range s.Classes {
		classSpec[c.Name] = c
	}

	type key struct{ class, dept, region string }
	next := map[key]int{}
	var out []*Cluster
	add := func(class, dept, region string, index int, version string, nodes int) error {
		if dept == "shared" {
			dept = ""
		}
		k := key{class, dept, region}
		if index == 0 {
			index = next[k] + 1
		}
		if index > next[k] {
			next[k] = index
		}
		c := &Cluster{Region: region, ProviderRegion: regionSpec[region].ProviderRegion,
			Class: class, Department: dept, Index: index}
		c.Name = clusterName(c)
		if err := checkSlug("cluster", c.Name); err != nil {
			return err
		}
		if version == "" && len(s.Clusters.KubernetesVersions) > 0 {
			version = s.Clusters.KubernetesVersions[hash(c.Name)%uint32(len(s.Clusters.KubernetesVersions))]
		}
		c.KubernetesVersion = version
		if nodes == 0 {
			nodes = classSpec[class].NodeCount
		}
		c.NodeCount = nodes
		out = append(out, c)
		return nil
	}

	for _, rule := range s.Clusters.Rules {
		for _, r := range s.Regions {
			n, ok := rule.Count[r.Name]
			if !ok {
				n = rule.Count["default"]
			}
			for i := 1; i <= n; i++ {
				if err := add(rule.Class, rule.Department, r.Name, 0, "", 0); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, def := range s.Clusters.List {
		if err := add(def.Class, def.Department, def.Region, def.Index, def.KubernetesVersion, def.NodeCount); err != nil {
			return nil, err
		}
		if def.Name != "" {
			out[len(out)-1].Name = def.Name
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if classOrder[a.Class] != classOrder[b.Class] {
			return classOrder[a.Class] < classOrder[b.Class]
		}
		if deptOrder[a.Department] != deptOrder[b.Department] {
			return deptOrder[a.Department] < deptOrder[b.Department]
		}
		if regionOrder[a.Region] != regionOrder[b.Region] {
			return regionOrder[a.Region] < regionOrder[b.Region]
		}
		return a.Index < b.Index
	})
	seen := map[string]bool{}
	for _, c := range out {
		if seen[c.Name] {
			return nil, fmt.Errorf("duplicate cluster %q", c.Name)
		}
		seen[c.Name] = true
	}
	return out, nil
}

// clusterName is <region>-<class><n> for shared clusters and
// <region>-<dept>-<class><n> for departmental ones.
func clusterName(c *Cluster) string {
	if c.Shared() {
		return fmt.Sprintf("%s-%s%d", c.Region, c.Class, c.Index)
	}
	return fmt.Sprintf("%s-%s-%s%d", c.Region, c.Department, c.Class, c.Index)
}

func expandComponent(s *Scenario, spec *ComponentSpec, c Component, layer string, clusters []*Cluster) (*ComponentModel, error) {
	cm := &ComponentModel{Name: c.Name, Layer: layer, Owner: c.Owner, Department: c.Department,
		Spec: spec, LiveStatus: c.LiveStatus, RootSpace: c.Name + "-base"}
	for _, u := range spec.Units {
		cm.Units = append(cm.Units, u.Slug)
	}
	for _, e := range c.External {
		cm.Units = append(cm.Units, e.Name)
	}
	cm.External = c.External

	placed := map[string]bool{}
	for _, cl := range clusters {
		if placedOn(c.Placement, cl) {
			d := &Deployment{Space: c.Name + "-" + cl.Name, Component: c.Name, Class: cl.Class, Cluster: cl}
			if err := checkSlug("deployment space", d.Space); err != nil {
				return nil, err
			}
			cm.Deployments = append(cm.Deployments, d)
			placed[cl.Class] = true
		}
	}
	for _, class := range classNames(s) {
		if placed[class] {
			cm.ClassBases = append(cm.ClassBases, &ClassBase{Class: class, Space: c.Name + "-" + class})
		}
	}
	return cm, nil
}

// placedOn is the union of the rules; no rules means every cluster.
func placedOn(rules []PlacementRule, c *Cluster) bool {
	if len(rules) == 0 {
		return true
	}
	for _, r := range rules {
		if ruleMatches(r, c) {
			return true
		}
	}
	return false
}

func ruleMatches(r PlacementRule, c *Cluster) bool {
	if !in(r.Classes, c.Class, true) || !in(r.Regions, c.Region, false) || !in(r.Departments, deptName(c), false) {
		return false
	}
	if r.MaxIndex > 0 && c.Index > r.MaxIndex {
		return false
	}
	if r.Exclude != nil && selectorMatches(*r.Exclude, c) {
		return false
	}
	return true
}

// selectorMatches is true when every non-empty list contains the cluster's
// value; an empty selector matches nothing.
func selectorMatches(sel Selector, c *Cluster) bool {
	if len(sel.Classes) == 0 && len(sel.Regions) == 0 && len(sel.Departments) == 0 {
		return false
	}
	return in(sel.Classes, c.Class, true) && in(sel.Regions, c.Region, false) && in(sel.Departments, deptName(c), false)
}

// in reports whether v is in list; an empty list matches everything, as does
// the literal "all" for classes.
func in(list []string, v string, allowAll bool) bool {
	if len(list) == 0 {
		return true
	}
	for _, x := range list {
		if x == v || (allowAll && x == "all") {
			return true
		}
	}
	return false
}

func deptName(c *Cluster) string {
	if c.Shared() {
		return "shared"
	}
	return c.Department
}

// selectStory draws disjoint samples over the sorted deployment list with a
// generator seeded from the scenario, so the same scenario always paints the
// same spaces red.
func selectStory(st Story, comps []*ComponentModel) StorySelection {
	var all []string
	for _, c := range comps {
		for _, d := range c.Deployments {
			all = append(all, d.Space)
		}
	}
	sort.Strings(all)
	rng := rand.New(rand.NewSource(st.Seed))
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })

	take := func(pct int) []string {
		n := len(all) * pct / 100
		if n > len(all) {
			n = len(all)
		}
		picked := append([]string(nil), all[:n]...)
		all = all[n:]
		sort.Strings(picked)
		return picked
	}
	var sel StorySelection
	sel.Degraded = take(st.LiveStatus.DegradedPercent)
	sel.OutOfSync = take(st.LiveStatus.OutOfSyncPercent)
	sel.Progressing = take(st.LiveStatus.ProgressingPercent)
	sel.Unreleased = take(st.Unreleased.Percent)
	return sel
}

func total(m *Model) Totals {
	t := Totals{Clusters: len(m.Clusters), Targets: len(m.Clusters), Workers: 1}
	for _, c := range m.Components {
		units := len(c.Units)
		variants := len(c.ClassBases) + len(c.Deployments)
		t.RootBases++
		t.ClassBases += len(c.ClassBases)
		t.Deployments += len(c.Deployments)
		t.Units += units * (1 + variants)
		t.Links += units * variants
		for _, cb := range c.ClassBases {
			if len(c.Spec.PerClass[cb.Class]) > 0 {
				t.FunctionCalls++
			}
		}
		if len(c.Spec.PerRegion) > 0 {
			regions := map[string]bool{}
			for _, d := range c.Deployments {
				regions[d.Cluster.Region] = true
			}
			t.FunctionCalls += len(regions)
		}
		if len(c.Spec.PerCluster) > 0 {
			t.FunctionCalls += len(c.Deployments)
		}
	}
	t.Spaces = 1 + t.Clusters + t.RootBases + t.ClassBases + t.Deployments
	t.Releases = t.Deployments - len(m.Story.Unreleased)
	return t
}

func classNames(s *Scenario) []string {
	out := make([]string, 0, len(s.Classes))
	for _, c := range s.Classes {
		out = append(out, c.Name)
	}
	return out
}

func indexOf(list []string) map[string]int {
	m := make(map[string]int, len(list))
	for i, v := range list {
		m[v] = i
	}
	return m
}

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(s)))
	return h.Sum32()
}
