package present

import (
	"os"
	"strings"
	"testing"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/cub-demo/internal/scenario"
	"github.com/confighub/cub-demo/internal/seed"
)

func TestBump(t *testing.T) {
	cases := []struct{ tag, how, want string }{
		{"5.3.0", "minor", "5.4.0"},
		{"5.3.0", "", "5.4.0"},
		{"5.3.7", "patch", "5.3.8"},
		{"5.3.7", "major", "6.0.0"},
		{"v1.17.0", "minor", "v1.18.0"},
		{"v1.17.0", "patch", "v1.17.1"},
	}
	for _, c := range cases {
		got, err := bump(c.tag, c.how)
		if err != nil {
			t.Fatalf("bump(%q,%q): %v", c.tag, c.how, err)
		}
		if got != c.want {
			t.Errorf("bump(%q,%q) = %q, want %q", c.tag, c.how, got, c.want)
		}
	}
	for _, bad := range [][2]string{{"latest", "minor"}, {"5.3", "patch"}, {"5.3.0", "sideways"}} {
		if _, err := bump(bad[0], bad[1]); err == nil {
			t.Errorf("bump(%q,%q) should fail", bad[0], bad[1])
		}
	}
}

func TestFindContainerImage(t *testing.T) {
	doc := map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
		"containers": []any{
			map[string]any{"name": "sidecar", "image": "registry/x/sidecar:1.0"},
			map[string]any{"name": "api", "image": "registry.meridian.example/catalog-api/api:5.3.0"},
		}}}}}
	if got := findContainerImage(doc, "api"); got != "registry.meridian.example/catalog-api/api:5.3.0" {
		t.Errorf("got %q", got)
	}
	if got := findContainerImage(doc, "nope"); got != "" {
		t.Errorf("got %q for a missing container", got)
	}
}

func TestParseProtections(t *testing.T) {
	rp, err := seed.ParseProtections([]string{
		"apps/v1/Deployment:catalog-api/catalog-api-api:spec.template.spec.containers.?name=api.resources.limits.memory",
		"apps/v1/Deployment:catalog-api/catalog-api-api:spec.replicas",
		"v1/ConfigMap:catalog-api/config:data.LOG_LEVEL",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rp) != 2 {
		t.Fatalf("want 2 resources, got %d", len(rp))
	}
	if rp[0].Resource.ResourceType != "apps/v1/Deployment" || rp[0].Resource.ResourceName != "catalog-api/catalog-api-api" {
		t.Errorf("resource 0 = %+v", rp[0].Resource)
	}
	if len(rp[0].Protected) != 2 || !rp[0].Protected["spec.replicas"] {
		t.Errorf("resource 0 paths = %v", rp[0].Protected)
	}
	if _, err := seed.ParseProtections([]string{"apps/v1/Deployment:spec.replicas"}); err == nil {
		t.Error("two-part path should fail")
	}
}

// The e2e scenario carries the play coverage: every primitive appears in a
// play, each play renders, and unknown names fail.
func TestE2EScenarioPlays(t *testing.T) {
	b, err := scenario.Load(os.DirFS("../.."), "e2e")
	if err != nil {
		t.Fatal(err)
	}
	m, err := scenario.Expand(b)
	if err != nil {
		t.Fatal(err)
	}
	p := &Presenter{Model: m}
	names := p.Plays()
	for _, want := range []string{"ship", "argobot", "incident", "heal"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("e2e scenario lacks play %q (have %v)", want, names)
		}
	}
	called := map[string]bool{}
	for _, n := range names {
		if _, err := p.Describe(n); err != nil {
			t.Errorf("describe %s: %v", n, err)
		}
		for _, st := range m.Scenario.Plays[n] {
			if st.Call == "" {
				continue
			}
			if _, ok := primitives[st.Call]; !ok {
				t.Errorf("play %s calls unknown primitive %q", n, st.Call)
			}
			called[st.Call] = true
		}
	}
	for prim := range primitives {
		if !called[prim] {
			t.Errorf("no e2e play exercises primitive %q", prim)
		}
	}
	if _, err := p.Describe("no-such-move"); err == nil {
		t.Error("unknown play should fail")
	}
}

func TestPlayContextTemplates(t *testing.T) {
	b, err := scenario.Load(os.DirFS("../.."), "e2e")
	if err != nil {
		t.Fatal(err)
	}
	m, err := scenario.Expand(b)
	if err != nil {
		t.Fatal(err)
	}
	ctx := PlayContext{Scenario: m.Scenario, Home: m.Home, Context: m.Scenario.Context, model: m}

	got, err := ctx.render(`--space {{.Base "e2e-catalog-api"}} --change-workflow {{.Workflow "e2e-catalog-api"}}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "--space e2e-catalog-api-base --change-workflow " + m.Home + "/" + scenario.StandardWorkflowSlug
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if _, err := ctx.render(`{{.Base "nope"}}`, nil); err == nil {
		t.Error("unknown component in a template should fail")
	}

	// Variables exported by an earlier step resolve; unexported names fail.
	got, err = ctx.render("e2e-catalog-api {{.Version}}", map[string]string{"Version": "5.4.0"})
	if err != nil || got != "e2e-catalog-api 5.4.0" {
		t.Errorf("render with vars: %q, %v", got, err)
	}
	if _, err := ctx.render("{{.Version}}", nil); err == nil {
		t.Error("an unexported variable should fail the template")
	}
}

func TestParamsValidation(t *testing.T) {
	w := params{"component": "x", "typo": "y"}
	if err := w.allow("observe", "component"); err == nil || !strings.Contains(err.Error(), "typo") {
		t.Errorf("allow should name the unknown key, got %v", err)
	}
	if got := (params{"variants": []any{"a", "b"}}).list("variants"); len(got) != 2 || got[1] != "b" {
		t.Errorf("list = %v", got)
	}
	if got := (params{"variants": "solo"}).list("variants"); len(got) != 1 || got[0] != "solo" {
		t.Errorf("scalar list = %v", got)
	}
}

func TestChangeOrderStepDerivesSlug(t *testing.T) {
	b, err := scenario.Load(os.DirFS("../.."), "e2e")
	if err != nil {
		t.Fatal(err)
	}
	m, err := scenario.Expand(b)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	p := &Presenter{Model: m, Out: &out, DryRun: true}
	vars := map[string]string{"Version": "5.4.0"}
	if err := stepChangeOrder(p, vars, params{"component": "e2e-catalog-api"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "e2e-catalog-api-5-4-0") {
		t.Errorf("derived slug missing from: %s", out.String())
	}
	if err := stepChangeOrder(p, map[string]string{}, params{"component": "e2e-catalog-api"}); err == nil {
		t.Error("no slug and no Version should fail")
	}
}

func TestPickOpen(t *testing.T) {
	co := func(slug, state string) *goclient.ChangeOrder { return &goclient.ChangeOrder{Slug: slug, State: state} }
	cases := []struct {
		name string
		in   []*goclient.ChangeOrder
		want []string
	}{
		{"none", nil, nil},
		{"one in progress", []*goclient.ChangeOrder{co("a", "InProgress")}, []string{"a"}},
		{"released alone stays in flight", []*goclient.ChangeOrder{co("a", "Released")}, []string{"a"}},
		{"released steps aside for the mover", []*goclient.ChangeOrder{co("a", "Released"), co("b", "New")}, []string{"b"}},
		{"aborted is gone", []*goclient.ChangeOrder{co("a", "Aborted"), co("b", "Released")}, []string{"b"}},
		{"two movers is ambiguous", []*goclient.ChangeOrder{co("a", "InProgress"), co("b", "New")}, []string{"a", "b"}},
	}
	for _, c := range cases {
		got := pickOpen(c.in)
		var slugs []string
		for _, g := range got {
			slugs = append(slugs, g.Slug)
		}
		if strings.Join(slugs, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: got %v want %v", c.name, slugs, c.want)
		}
	}
}
