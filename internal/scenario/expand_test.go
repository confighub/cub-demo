package scenario

import (
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
