package scenario

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The tests read the real embedded content from the repo root, so they check
// the shipped scenarios, not fixtures.
func load(t *testing.T, name string) *Model {
	t.Helper()
	b, err := Load(os.DirFS("../.."), name)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Expand(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMeridianShape(t *testing.T) {
	m := load(t, "meridian")
	if got := m.Totals.Clusters; got < 95 || got > 105 {
		t.Errorf("meridian should have about 100 clusters, got %d", got)
	}
	if len(m.Components) != 19 {
		t.Errorf("expected 19 components, got %d", len(m.Components))
	}
	byName := map[string]*ComponentModel{}
	for _, c := range m.Components {
		byName[c.Name] = c
	}
	// A platform component with no placement lands on every cluster.
	if n := len(byName["cert-manager"].Deployments); n != m.Totals.Clusters {
		t.Errorf("cert-manager on %d clusters, want %d", n, m.Totals.Clusters)
	}
	// velero is uat/prod only, so it has exactly those class bases.
	var classes []string
	for _, cb := range byName["velero"].ClassBases {
		classes = append(classes, cb.Class)
	}
	if !reflect.DeepEqual(classes, []string{"uat", "prod"}) {
		t.Errorf("velero class bases = %v", classes)
	}
	// Workloads are sparse: payments never lands on retail clusters.
	for _, d := range byName["payment-gateway"].Deployments {
		if d.Cluster.Department != "payments" {
			t.Errorf("payment-gateway placed on %s", d.Cluster.Name)
		}
	}
	// External resources become units.
	if !reflect.DeepEqual(byName["checkout"].Units, []string{"namespace", "api", "worker", "checkout-db", "order-events"}) {
		t.Errorf("checkout units = %v", byName["checkout"].Units)
	}
	if m.Home != "meridian-platform" {
		t.Errorf("home = %s", m.Home)
	}
}

func TestClusterNaming(t *testing.T) {
	m := load(t, "meridian")
	seen := map[string]bool{}
	for _, c := range m.Clusters {
		if seen[c.Name] {
			t.Errorf("duplicate cluster %s", c.Name)
		}
		seen[c.Name] = true
		if c.KubernetesVersion == "" || c.NodeCount == 0 {
			t.Errorf("%s has no version or node count", c.Name)
		}
	}
	want := map[string]bool{"us-east-prod3": true, "us-east-payments-prod3": true, "sa-east-dev1": true}
	for n := range want {
		if !seen[n] {
			t.Errorf("expected cluster %s", n)
		}
	}
	if seen["sa-east-payments-prod1"] {
		t.Error("payments must not have a sa-east cluster")
	}
}

func TestDeterministic(t *testing.T) {
	a, b := load(t, "meridian"), load(t, "meridian")
	if !reflect.DeepEqual(a.Story, b.Story) {
		t.Error("story selection is not deterministic")
	}
	if !reflect.DeepEqual(a.Totals, b.Totals) {
		t.Error("totals are not deterministic")
	}
	all := map[string]int{}
	for _, set := range [][]string{a.Story.Degraded, a.Story.OutOfSync, a.Story.Progressing, a.Story.Unreleased} {
		for _, s := range set {
			all[s]++
			if a.Deployment(s) == nil {
				t.Errorf("story names unknown deployment %s", s)
			}
		}
	}
	for s, n := range all {
		if n > 1 {
			t.Errorf("%s selected for %d story treatments", s, n)
		}
	}
}

func TestTotalsAddUp(t *testing.T) {
	m := load(t, "e2e")
	tt := m.Totals
	if tt.Spaces != 1+tt.Clusters+tt.RootBases+tt.ClassBases+tt.Deployments {
		t.Errorf("spaces do not add up: %+v", tt)
	}
	if tt.Releases != tt.Deployments-len(m.Story.Unreleased) {
		t.Errorf("releases do not add up: %+v", tt)
	}
	units := 0
	for _, c := range m.Components {
		units += len(c.Units) * (1 + len(c.ClassBases) + len(c.Deployments))
	}
	if tt.Units != units {
		t.Errorf("units = %d, want %d", tt.Units, units)
	}
}

func TestValidation(t *testing.T) {
	dir := t.TempDir()
	bad := dir + "/bad.yaml"
	os.WriteFile(bad, []byte(`
name: Bad Name
regions: [{name: us-east}]
classes: [{name: dev}]
`), 0o644)
	if _, err := Load(os.DirFS("../.."), bad); err == nil {
		t.Error("expected an invalid slug error")
	}
	os.WriteFile(bad, []byte(`
name: bad
regions: [{name: us-east}]
classes: [{name: dev}]
clusters: {rules: [{class: prod, count: {default: 1}}]}
`), 0o644)
	if _, err := Load(os.DirFS("../.."), bad); err == nil {
		t.Error("expected an unknown class error")
	}
	os.WriteFile(bad, []byte(`
name: bad
regions: [{name: us-east}]
classes: [{name: dev}]
catalog: [{name: cert-manager, dir: catalog/cert-manager, typo: 1}]
`), 0o644)
	if _, err := Load(os.DirFS("../.."), bad); err == nil {
		t.Error("expected an unknown field error")
	}
}

// A bundle stored in the org (Bundle.Files) must load back to the same model.
func TestLoadFromFilesRoundTrip(t *testing.T) {
	b, err := Load(os.DirFS(".."+string(os.PathSeparator)+".."), "e2e")
	if err != nil {
		t.Fatal(err)
	}
	files, err := b.Files()
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string][]byte{}
	for _, f := range files {
		byPath[f.Path] = f.Data
	}
	again, err := LoadFromFiles("e2e", byPath)
	if err != nil {
		t.Fatal(err)
	}
	m1, err := Expand(b)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := Expand(again)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Totals != m2.Totals || len(m1.Components) != len(m2.Components) || m1.Home != m2.Home {
		t.Errorf("round trip differs: %+v vs %+v", m1.Totals, m2.Totals)
	}
	if _, err := LoadFromFiles("x", map[string][]byte{}); err == nil {
		t.Error("a bundle without scenario.yaml should fail")
	}
}

func TestValidateAgents(t *testing.T) {
	base := func() *Scenario {
		return &Scenario{
			Name:      "t",
			Regions:   []Region{{Name: "r"}},
			Classes:   []Class{{Name: "dev"}},
			Catalog:   []Component{{Name: "web", Dir: "catalog/web"}},
			Workflows: Workflows{Review: &ReviewWorkflow{Components: []string{"web"}}},
			Agents: []Agent{{Name: "ci", Component: "web", Every: "30s", Rollout: true,
				Change: []PlayStep{{Call: "bump-image", With: map[string]any{"component": "web"}}}}},
		}
	}
	if err := base().validateAgents(); err != nil {
		t.Fatalf("valid agents refused: %v", err)
	}
	for name, edit := range map[string]func(s *Scenario){
		"bad interval":          func(s *Scenario) { s.Agents[0].Every = "soon" },
		"no interval":           func(s *Scenario) { s.Agents[0].Every = "" },
		"unknown component":     func(s *Scenario) { s.Agents[0].Component = "nope" },
		"rollout without base":  func(s *Scenario) { s.Agents[0].Component = "" },
		"no steps":              func(s *Scenario) { s.Agents[0].Change = nil },
		"run and call":          func(s *Scenario) { s.Agents[0].Change[0].Run = "echo" },
		"duplicate name":        func(s *Scenario) { s.Agents = append(s.Agents, s.Agents[0]) },
		"review of the unknown": func(s *Scenario) { s.Workflows.Review.Components = []string{"nope"} },
		"review listed twice":   func(s *Scenario) { s.Workflows.Review.Components = []string{"web", "web"} },
		"reviewer twice":        func(s *Scenario) { s.Workflows.Review.Reviewers = []string{"ada", "ada"} },
		"empty reviewer":        func(s *Scenario) { s.Workflows.Review.Reviewers = []string{" "} },
		"review, no workflows":  func(s *Scenario) { s.Workflows.Disabled = true },
	} {
		s := base()
		edit(s)
		if err := s.validateAgents(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	s := base()
	s.Workflows.Review = nil
	if err := s.validateAgents(); err != nil {
		t.Errorf("agents need no review workflow: %v", err)
	}
}

func TestReviewWorkflowJSON(t *testing.T) {
	m := &Model{Scenario: &Scenario{}}
	if m.ReviewWorkflowJSON(nil) != nil {
		t.Fatal("a scenario without workflows.review renders a review workflow")
	}
	m.Scenario.Workflows.Review = &ReviewWorkflow{}
	var wf struct {
		Stages                   []any
		Final                    struct{ Prerequisites []string }
		AttestationPrerequisites []struct {
			Name        string
			FromUserIDs []string
		}
	}
	if err := json.Unmarshal(m.ReviewWorkflowJSON([]string{"b", "a"}), &wf); err != nil {
		t.Fatal(err)
	}
	if len(wf.Stages) != 0 || len(wf.Final.Prerequisites) != 1 || wf.Final.Prerequisites[0] != ReviewRequirementName {
		t.Errorf("want no stages and Final gating on %s, got %+v", ReviewRequirementName, wf)
	}
	if len(wf.AttestationPrerequisites) != 1 || wf.AttestationPrerequisites[0].Name != ReviewRequirementName {
		t.Fatalf("want one requirement named %s, got %+v", ReviewRequirementName, wf.AttestationPrerequisites)
	}
	// Sorted, so the definition's hash does not depend on the order reviewers are listed in.
	if got := wf.AttestationPrerequisites[0].FromUserIDs; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("want the reviewers' IDs sorted, got %v", got)
	}
}
