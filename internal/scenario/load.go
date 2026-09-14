package scenario

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Bundle is a loaded scenario together with the component directories it
// references. Component files resolve from disk next to a scenario given by
// path, falling back to the embedded manifests, so a user can export the
// default scenario, edit a few manifests, and keep the rest.
type Bundle struct {
	Scenario *Scenario
	// Specs holds each component's component.yaml, by component name.
	Specs map[string]*ComponentSpec
	dirs  map[string]fs.FS
	// externals holds the shared external-resource templates by kind.
	externals map[string][]byte
	// rawScenario and rawSpecs are the bytes as loaded, kept for provenance.
	rawScenario []byte
	rawSpecs    map[string][]byte
}

// BundleFile is one file of the scenario bundle, for provenance storage.
type BundleFile struct {
	// Path is scenario.yaml or manifests/<...>, the layout Load reads.
	Path string
	Data []byte
}

// Files returns every file of the bundle: the scenario itself, each
// component's component.yaml and unit manifests, and the external-resource
// templates in use. Deterministic order: scenario first, then sorted paths.
func (b *Bundle) Files() ([]BundleFile, error) {
	out := []BundleFile{{Path: "scenario.yaml", Data: b.rawScenario}}
	var rest []BundleFile
	for _, c := range b.Scenario.AllComponents() {
		rest = append(rest, BundleFile{Path: path.Join("manifests", c.Dir, "component.yaml"), Data: b.rawSpecs[c.Name]})
		for _, u := range b.Specs[c.Name].Units {
			data, err := b.Open(c.Name, u.File)
			if err != nil {
				return nil, fmt.Errorf("component %q: %w", c.Name, err)
			}
			rest = append(rest, BundleFile{Path: path.Join("manifests", c.Dir, u.File), Data: data})
		}
	}
	for kind, data := range b.externals {
		rest = append(rest, BundleFile{Path: path.Join("manifests", "external", kind+".yaml"), Data: data})
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Path < rest[j].Path })
	return append(out, rest...), nil
}

// ExternalTemplate returns the shared template for an external-resource kind.
func (b *Bundle) ExternalTemplate(kind string) ([]byte, error) {
	raw, ok := b.externals[kind]
	if !ok {
		return nil, fmt.Errorf("no external template for kind %q", kind)
	}
	return raw, nil
}

// Open reads a file from a component's directory.
func (b *Bundle) Open(component, file string) ([]byte, error) {
	dir, ok := b.dirs[component]
	if !ok {
		return nil, fmt.Errorf("unknown component %q", component)
	}
	return fs.ReadFile(dir, file)
}

// Embedded lists the scenario names shipped in the binary.
func Embedded(assets fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(assets, "scenarios")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".yaml"); ok && !e.IsDir() {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// ReadEmbedded returns the raw YAML of an embedded scenario.
func ReadEmbedded(assets fs.FS, name string) ([]byte, error) {
	return fs.ReadFile(assets, path.Join("scenarios", name+".yaml"))
}

// Load resolves ref as an embedded scenario name or a path to a scenario
// file, parses it, loads the component specs and validates the whole.
func Load(assets fs.FS, ref string) (*Bundle, error) {
	var raw []byte
	var base string
	if data, err := ReadEmbedded(assets, ref); err == nil {
		raw = data
	} else {
		data, err := os.ReadFile(ref)
		if err != nil {
			return nil, fmt.Errorf("scenario %q is neither embedded nor a readable file: %w", ref, err)
		}
		raw = data
		base = filepath.Dir(ref)
	}

	var sc Scenario
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&sc); err != nil {
		return nil, fmt.Errorf("parse scenario %q: %w", ref, err)
	}
	if err := sc.validate(); err != nil {
		return nil, fmt.Errorf("scenario %q: %w", ref, err)
	}

	b := &Bundle{Scenario: &sc, Specs: map[string]*ComponentSpec{}, dirs: map[string]fs.FS{},
		externals: map[string][]byte{}, rawScenario: raw, rawSpecs: map[string][]byte{}}
	for _, c := range sc.AllComponents() {
		for _, e := range c.External {
			if _, ok := b.externals[e.Kind]; ok {
				continue
			}
			raw, err := readWithFallback(assets, base, path.Join("external", e.Kind+".yaml"))
			if err != nil {
				return nil, fmt.Errorf("component %q external %q: %w", c.Name, e.Name, err)
			}
			b.externals[e.Kind] = raw
		}
	}
	for _, c := range sc.AllComponents() {
		dir, err := componentDir(assets, base, c.Dir)
		if err != nil {
			return nil, fmt.Errorf("component %q: %w", c.Name, err)
		}
		specRaw, err := fs.ReadFile(dir, "component.yaml")
		if err != nil {
			return nil, fmt.Errorf("component %q: %w", c.Name, err)
		}
		var spec ComponentSpec
		d := yaml.NewDecoder(strings.NewReader(string(specRaw)))
		d.KnownFields(true)
		if err := d.Decode(&spec); err != nil {
			return nil, fmt.Errorf("component %q: parse component.yaml: %w", c.Name, err)
		}
		if err := spec.validate(c); err != nil {
			return nil, fmt.Errorf("component %q: %w", c.Name, err)
		}
		b.Specs[c.Name] = &spec
		b.dirs[c.Name] = dir
		b.rawSpecs[c.Name] = specRaw
	}
	return b, nil
}

// readWithFallback reads manifests/<rel>, preferring the scenario's disk
// directory over the embedded copy.
func readWithFallback(assets fs.FS, base, rel string) ([]byte, error) {
	if base != "" {
		onDisk := filepath.Join(base, "manifests", filepath.FromSlash(rel))
		if data, err := os.ReadFile(onDisk); err == nil {
			return data, nil
		}
	}
	return fs.ReadFile(assets, path.Join("manifests", rel))
}

// componentDir prefers <base>/manifests/<dir> on disk, then the embedded
// manifests/<dir>.
func componentDir(assets fs.FS, base, dir string) (fs.FS, error) {
	if base != "" {
		onDisk := filepath.Join(base, "manifests", filepath.FromSlash(dir))
		if st, err := os.Stat(onDisk); err == nil && st.IsDir() {
			return os.DirFS(onDisk), nil
		}
	}
	embedded := path.Join("manifests", dir)
	if st, err := fs.Stat(assets, embedded); err == nil && st.IsDir() {
		return fs.Sub(assets, embedded)
	}
	return nil, fmt.Errorf("directory %q not found on disk or embedded", dir)
}

// Export writes an embedded scenario and every component directory it
// references into dir, in the layout Load reads back from disk.
func Export(assets fs.FS, name, dir string) error {
	raw, err := ReadEmbedded(assets, name)
	if err != nil {
		return fmt.Errorf("no embedded scenario %q", name)
	}
	var sc Scenario
	if err := yaml.Unmarshal(raw, &sc); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), raw, 0o644); err != nil {
		return err
	}
	for _, c := range sc.AllComponents() {
		src := path.Join("manifests", c.Dir)
		dst := filepath.Join(dir, "manifests", filepath.FromSlash(c.Dir))
		err := fs.WalkDir(assets, src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, p)
			target := filepath.Join(dst, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			data, err := fs.ReadFile(assets, p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0o644)
		})
		if err != nil {
			return fmt.Errorf("export %s: %w", c.Dir, err)
		}
	}
	return nil
}

// AllComponents returns catalog then workloads.
func (s *Scenario) AllComponents() []Component {
	all := make([]Component, 0, len(s.Catalog)+len(s.Workloads))
	all = append(all, s.Catalog...)
	all = append(all, s.Workloads...)
	return all
}

// slugRe is the shape ConfigHub accepts for slugs; MaxSlugLen is its limit.
var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

const MaxSlugLen = 128

func checkSlug(what, s string) error {
	if !slugRe.MatchString(s) {
		return fmt.Errorf("%s %q is not a valid slug (lowercase letters, digits and dashes)", what, s)
	}
	if len(s) > MaxSlugLen {
		return fmt.Errorf("%s %q is longer than %d characters", what, s, MaxSlugLen)
	}
	return nil
}

func (s *Scenario) validate() error {
	if err := checkSlug("name", s.Name); err != nil {
		return err
	}
	if len(s.Regions) == 0 || len(s.Classes) == 0 {
		return fmt.Errorf("at least one region and one class are required")
	}
	regions := map[string]bool{}
	for _, r := range s.Regions {
		if err := checkSlug("region", r.Name); err != nil {
			return err
		}
		if regions[r.Name] {
			return fmt.Errorf("duplicate region %q", r.Name)
		}
		regions[r.Name] = true
	}
	classes := map[string]bool{}
	for _, c := range s.Classes {
		if err := checkSlug("class", c.Name); err != nil {
			return err
		}
		if classes[c.Name] {
			return fmt.Errorf("duplicate class %q", c.Name)
		}
		classes[c.Name] = true
	}
	depts := map[string]bool{"": true, "shared": true}
	for _, d := range s.Departments {
		if err := checkSlug("department", d); err != nil {
			return err
		}
		depts[d] = true
	}
	for i, r := range s.Clusters.Rules {
		if !classes[r.Class] {
			return fmt.Errorf("clusters.rules[%d]: unknown class %q", i, r.Class)
		}
		if !depts[r.Department] {
			return fmt.Errorf("clusters.rules[%d]: unknown department %q", i, r.Department)
		}
		for region := range r.Count {
			if region != "default" && !regions[region] {
				return fmt.Errorf("clusters.rules[%d]: unknown region %q", i, region)
			}
		}
	}
	for i, c := range s.Clusters.List {
		if !classes[c.Class] || !regions[c.Region] || !depts[c.Department] {
			return fmt.Errorf("clusters.list[%d]: unknown class, region or department", i)
		}
	}
	names := map[string]bool{}
	for _, c := range s.AllComponents() {
		if err := checkSlug("component", c.Name); err != nil {
			return err
		}
		if names[c.Name] {
			return fmt.Errorf("duplicate component %q", c.Name)
		}
		names[c.Name] = true
		if c.Dir == "" {
			return fmt.Errorf("component %q has no dir", c.Name)
		}
		if !depts[c.Department] {
			return fmt.Errorf("component %q: unknown department %q", c.Name, c.Department)
		}
		for j, p := range c.Placement {
			if err := checkSelector(Selector{p.Classes, p.Regions, p.Departments}, classes, regions, depts); err != nil {
				return fmt.Errorf("component %q placement[%d]: %w", c.Name, j, err)
			}
			if p.Exclude != nil {
				if err := checkSelector(*p.Exclude, classes, regions, depts); err != nil {
					return fmt.Errorf("component %q placement[%d].exclude: %w", c.Name, j, err)
				}
			}
		}
		for _, e := range c.External {
			if err := checkSlug("external resource", e.Name); err != nil {
				return fmt.Errorf("component %q: %w", c.Name, err)
			}
			switch e.Kind {
			case "rds", "bucket", "queue", "cache":
			default:
				return fmt.Errorf("component %q: unknown external kind %q", c.Name, e.Kind)
			}
		}
	}
	for i, sk := range s.Story.Skews {
		if !names[sk.Component] {
			return fmt.Errorf("story.skews[%d]: unknown component %q", i, sk.Component)
		}
		if err := checkSelector(sk.Where, classes, regions, depts); err != nil {
			return fmt.Errorf("story.skews[%d]: %w", i, err)
		}
	}
	for i, co := range s.Workflows.ChangeOrders {
		if !names[co.Component] {
			return fmt.Errorf("workflows.changeOrders[%d]: unknown component %q", i, co.Component)
		}
		if err := checkSlug("change order slug", co.Slug); err != nil {
			return fmt.Errorf("workflows.changeOrders[%d]: %w", i, err)
		}
		if co.Unit == "" || co.Function == "" {
			return fmt.Errorf("workflows.changeOrders[%d]: unit and function are required", i)
		}
		if co.LandedThrough != "bases" && !classes[co.LandedThrough] {
			return fmt.Errorf("workflows.changeOrders[%d]: landedThrough %q is neither 'bases' nor a class", i, co.LandedThrough)
		}
	}
	firstClass := s.Classes[0].Name
	for stage, reqs := range s.Workflows.Prerequisites {
		if stage != "final" && !classes[stage] {
			return fmt.Errorf("workflows.prerequisites: unknown stage %q (a class name or 'final')", stage)
		}
		if stage == firstClass && len(reqs) > 0 {
			return fmt.Errorf("workflows.prerequisites: the first class (%s) cannot carry gates; the stage before it holds only bases", firstClass)
		}
		for _, r := range reqs {
			switch CanonicalPrerequisite(r) {
			case "Released", "Healthy":
			default:
				return fmt.Errorf("workflows.prerequisites.%s: unknown prerequisite %q (released, healthy)", stage, r)
			}
		}
	}
	for name, steps := range s.Plays {
		if err := checkSlug("play", name); err != nil {
			return err
		}
		if len(steps) == 0 {
			return fmt.Errorf("play %q has no steps", name)
		}
		for i, st := range s.Plays[name] {
			switch {
			case st.Run != "" && st.Call != "":
				return fmt.Errorf("play %q step %d: run and call are mutually exclusive", name, i)
			case st.Run == "" && st.Call == "":
				return fmt.Errorf("play %q step %d: one of run or call is required", name, i)
			case st.Run != "" && st.With != nil:
				return fmt.Errorf("play %q step %d: with belongs to call steps", name, i)
			}
		}
	}
	pct := s.Story.LiveStatus.DegradedPercent + s.Story.LiveStatus.OutOfSyncPercent + s.Story.LiveStatus.ProgressingPercent
	if pct > 100 || s.Story.Unreleased.Percent > 100 {
		return fmt.Errorf("story percentages exceed 100")
	}
	return nil
}

func checkSelector(sel Selector, classes, regions, depts map[string]bool) error {
	for _, c := range sel.Classes {
		if c != "all" && !classes[c] {
			return fmt.Errorf("unknown class %q", c)
		}
	}
	for _, r := range sel.Regions {
		if !regions[r] {
			return fmt.Errorf("unknown region %q", r)
		}
	}
	for _, d := range sel.Departments {
		if !depts[d] {
			return fmt.Errorf("unknown department %q", d)
		}
	}
	return nil
}

func (spec *ComponentSpec) validate(c Component) error {
	if len(spec.Units) == 0 {
		return fmt.Errorf("component.yaml lists no units")
	}
	seen := map[string]bool{}
	for _, u := range spec.Units {
		if err := checkSlug("unit", u.Slug); err != nil {
			return err
		}
		if u.File == "" {
			return fmt.Errorf("unit %q has no file", u.Slug)
		}
		if seen[u.Slug] {
			return fmt.Errorf("duplicate unit %q", u.Slug)
		}
		seen[u.Slug] = true
	}
	for _, e := range c.External {
		if seen[e.Name] {
			return fmt.Errorf("external resource %q collides with a unit slug", e.Name)
		}
		seen[e.Name] = true
	}
	return nil
}
