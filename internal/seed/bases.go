package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/cubclient"
	"github.com/confighub/cub-demo/internal/manifests"
	"github.com/confighub/cub-demo/internal/scenario"
)

// manifestHashAnnotation records the rendered manifest a root unit was last
// seeded with; see the baseline comparison in componentBases.
const manifestHashAnnotation = "cub-demo.confighub.com/manifest-hash"

// fnsMarker is the annotation on a class base recording which per-class
// function set has been applied, so a re-run does not create pointless
// revisions by re-invoking functions that already ran.
const fnsMarker = "confighub.com/demo-functions"

// componentVariantPattern is the slug pattern for variant spaces, matching
// "cub variant create" so the trees read identically.
const componentVariantPattern = "template:{{.Labels.Component}}-{{.Labels.Variant}}"

// bases builds each component's root base (units rendered from the manifest
// files) and its class bases (server-side clones of the root, class policy
// applied by functions). A component whose manifest files are not authored yet
// is skipped and reported, so the scenario can carry components ahead of their
// content.
func (s *Seeder) bases() error {
	built, skipped := 0, 0
	err := forEach(s, s.Model.Components, func(cm *scenario.ComponentModel) error {
		ok, err := s.componentBases(cm)
		if err != nil {
			return fmt.Errorf("component %s: %w", cm.Name, err)
		}
		s.mu.Lock()
		if ok {
			built++
		} else {
			skipped++
		}
		s.mu.Unlock()
		return nil
	})
	fmt.Fprintf(s.Out, "  ensured bases for %d of %d components%s\n", built, len(s.Model.Components), skippedNote(skipped))
	return err
}

func skippedNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d skipped: manifests not authored yet)", n)
}

// componentBases returns false when the component's manifests are missing.
func (s *Seeder) componentBases(cm *scenario.ComponentModel) (bool, error) {
	if !cm.HasManifests {
		return false, nil
	}
	// Render everything first: a component seeds all of its units or none.
	// External resources render from the shared kind templates and become
	// ordinary units next to the manifest ones.
	rendered := map[string][]byte{}
	for _, u := range cm.Spec.Units {
		data, err := manifests.RenderUnit(s.Bundle, cm.Name, u)
		if err != nil {
			return false, err
		}
		rendered[u.Slug] = data
	}
	for _, e := range cm.External {
		data, err := manifests.RenderExternal(s.Bundle, cm.Name, e)
		if err != nil {
			return false, err
		}
		rendered[e.Name] = data
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would ensure %s: root + %d class bases, %d units each\n", cm.Name, len(cm.ClassBases), len(cm.Units))
		return true, nil
	}

	rootLabels := s.baseLabels(map[string]string{
		"Component": cm.Name,
		"Variant":   "base",
		"Role":      "base",
		"Layer":     cm.Layer,
	})
	if cm.Owner != "" {
		rootLabels["Owner"] = cm.Owner
	}
	if cm.Department != "" && cm.Department != "shared" {
		rootLabels["Department"] = cm.Department
	}
	root, _, err := s.ensureSpace(cm.RootSpace, cm.Name+" base", rootLabels)
	if err != nil {
		return false, err
	}

	// Root units: create the missing, re-upload the changed (by data hash).
	existing, err := s.Client.ListUnits(root.SpaceID, "")
	if err != nil {
		return false, err
	}
	bySlug := map[string]*goclient.Unit{}
	for _, u := range existing {
		bySlug[u.Slug] = u
	}
	unitLabels := s.baseLabels(map[string]string{"Component": cm.Name, "Layer": cm.Layer})
	for _, slug := range cm.Units {
		data := rendered[slug]
		unit := bySlug[slug]
		if unit == nil {
			unit, err = s.Client.CreateUnit(root.SpaceID, goclient.Unit{
				SpaceID:       root.SpaceID,
				Slug:          slug,
				DisplayName:   slug,
				ToolchainType: "Kubernetes/YAML",
				Labels:        unitLabels,
			})
			if err != nil {
				return false, fmt.Errorf("unit %s: %w", slug, err)
			}
		}
		// The manifest is the unit's baseline, not necessarily its head: story
		// change orders legitimately layer revisions on top of root units. So
		// idempotency compares the rendered manifest against the recorded
		// manifest hash, never against the head data — comparing heads made
		// every run revert the story's change and re-apply it, minting a
		// revision pair each time.
		sum := hex.EncodeToString(func() []byte { h := sha256.Sum256(data); return h[:] }())
		if unit.Annotations[manifestHashAnnotation] == sum {
			continue
		}
		if !strings.EqualFold(unit.DataHash, sum) {
			desc := fmt.Sprintf("%s manifest for %s", slug, cm.Name)
			if err := s.Client.UploadUnitData(root.SpaceID, unit.UnitID, data, desc); err != nil {
				return false, fmt.Errorf("unit %s data: %w", slug, err)
			}
		}
		patch, _ := json.Marshal(map[string]any{"Annotations": map[string]string{manifestHashAnnotation: sum}})
		where := fmt.Sprintf("UnitID = '%s'", unit.UnitID)
		if err := s.Client.BulkPatchUnits(where, patch); err != nil {
			return false, fmt.Errorf("unit %s annotation: %w", slug, err)
		}
	}

	// Class bases: one bulk clone of the root per class, stamping the class
	// labels and the upstream annotation the same way "cub variant create"
	// does.
	for _, cb := range cm.ClassBases {
		if s.space(cb.Space) != nil {
			continue
		}
		where := fmt.Sprintf("SpaceID = '%s'", root.SpaceID)
		variantLabels := fmt.Sprintf("Variant=%s,Stage=%s,Environment=%s,Role=base", cb.Class, cb.Class, cb.Class)
		pattern := componentVariantPattern
		allow := "true"
		patch, _ := json.Marshal(map[string]any{"Annotations": map[string]string{"UpstreamSpaceID": root.SpaceID.String()}})
		responses, err := s.Client.BulkCreateSpaces(&goclient.BulkCreateSpacesParams{
			Where: &where, VariantLabels: &variantLabels, NamePattern: &pattern, AllowExists: &allow,
		}, patch)
		if err != nil {
			return false, fmt.Errorf("class base %s: %w", cb.Space, err)
		}
		for _, r := range responses {
			if r.Space != nil {
				s.remember(r.Space)
			}
		}
	}

	// Class-base units: one bulk clone of the root's units into every class
	// base. The clones carry upstream lineage and UpgradeUnit links, which is
	// what makes the component tree a tree.
	classes := make([]string, 0, len(cm.ClassBases))
	for _, cb := range cm.ClassBases {
		classes = append(classes, "'"+cb.Class+"'")
	}
	if len(classes) > 0 {
		where := fmt.Sprintf("SpaceID = '%s'", root.SpaceID)
		whereSpace := fmt.Sprintf("Labels.%s = '%s' AND Labels.Component = '%s' AND Labels.Role = 'base' AND Labels.Variant IN (%s)",
			LabelDemoName, s.Model.Scenario.Name, cm.Name, strings.Join(classes, ", "))
		allow := "true"
		include := "UpstreamUnitID,SpaceID"
		_, err = s.Client.BulkCreateUnits(&goclient.BulkCreateUnitsParams{
			Where: &where, WhereSpace: &whereSpace, AllowExists: &allow, Include: &include,
		}, []byte("null"))
		if err != nil {
			return false, fmt.Errorf("class base units: %w", err)
		}
	}

	// Class policy, applied by server-side functions so each class base gets
	// real revisions with a readable change description.
	for _, cb := range cm.ClassBases {
		if err := s.classFunctions(cm, cb); err != nil {
			return false, fmt.Errorf("class %s functions: %w", cb.Class, err)
		}
	}
	return true, nil
}

// classFunctions applies the component's perClass function calls to one class
// base, once: the marker annotation records the applied set, and matching
// markers are skipped.
func (s *Seeder) classFunctions(cm *scenario.ComponentModel, cb *scenario.ClassBase) error {
	calls := cm.Spec.PerClass[cb.Class]
	if len(calls) == 0 {
		return nil
	}
	class, ok := s.classByName(cb.Class)
	if !ok {
		return fmt.Errorf("unknown class %q", cb.Class)
	}
	marker := fnsHash(calls)
	sp := s.space(cb.Space)
	if sp == nil {
		return fmt.Errorf("class base %s does not exist", cb.Space)
	}
	if sp.Annotations[fnsMarker] == marker {
		return nil
	}

	// Calls with the same unit where-clause share one request, so the
	// argument order within a where-group is preserved.
	groups := map[string][]cubclient.Invocation{}
	var order []string
	for _, call := range calls {
		args, err := manifests.RenderArgs(call.Args, manifests.ArgContext{Scenario: s.Model.Scenario, Class: class})
		if err != nil {
			return err
		}
		if _, seen := groups[call.Where]; !seen {
			order = append(order, call.Where)
		}
		groups[call.Where] = append(groups[call.Where], cubclient.Invocation{Function: call.Function, Args: args})
	}
	sort.Strings(order)
	for _, w := range order {
		where := fmt.Sprintf("SpaceID = '%s'", sp.SpaceID)
		if w != "" {
			where += " AND " + w
		}
		desc := fmt.Sprintf("%s class policy for %s", cb.Class, cm.Name)
		if err := s.Client.InvokeFunctions(where, desc, groups[w]); err != nil {
			return err
		}
	}
	patch, _ := json.Marshal(map[string]any{"Annotations": map[string]string{fnsMarker: marker}})
	return s.Client.PatchSpace(sp.SpaceID, patch)
}

func (s *Seeder) classByName(name string) (scenario.Class, bool) {
	for _, c := range s.Model.Scenario.Classes {
		if c.Name == name {
			return c, true
		}
	}
	return scenario.Class{}, false
}

// fnsHash fingerprints a function-call list for the marker annotation.
func fnsHash(calls []scenario.FunctionCall) string {
	return fnsHashAny(calls)
}

// fnsHashAny fingerprints any JSON-marshalable definition.
func fnsHashAny(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

var _ = uuid.Nil
