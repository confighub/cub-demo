package scenario

import (
	"fmt"
	"path"
	"testing/fstest"
)

// PathAnnotation is where the provenance phase records a bundle file's path on
// the unit that stores it, so the bundle can be read back exactly as seeded.
const PathAnnotation = "cub-demo.confighub.com/path"

// LoadFromFiles loads a bundle from its files keyed by bundle path — the shape
// Bundle.Files produces and the provenance phase stores in the org — so the
// verbs run against the scenario as it was seeded, not as the binary embeds
// it. "scenario.yaml" is required; the rest are the manifests.
func LoadFromFiles(name string, files map[string][]byte) (*Bundle, error) {
	raw, ok := files["scenario.yaml"]
	if !ok {
		return nil, fmt.Errorf("scenario %s: the org's bundle has no scenario.yaml", name)
	}
	mem := fstest.MapFS{path.Join("scenarios", name+".yaml"): &fstest.MapFile{Data: raw}}
	for p, data := range files {
		if p == "scenario.yaml" {
			continue
		}
		mem[p] = &fstest.MapFile{Data: data}
	}
	b, err := Load(mem, name)
	if err != nil {
		return nil, fmt.Errorf("scenario %s from the org: %w", name, err)
	}
	return b, nil
}
