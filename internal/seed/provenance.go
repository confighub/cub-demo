package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/cub-demo/internal/scenario"
)

// StoreBundle stores the scenario bundle in the org itself: one unit per
// file in a <name>-scenario space. This is the whole of "cub demo install" --
// the org becomes self-describing ("what defines this demo, at which
// revision?" is answered by ConfigHub), scenario updates land as ordinary
// revisions, and every other verb reads the definition back from here. The
// manifests are Go templates but valid YAML, so they are stored as
// AppConfig/YAML.
func (s *Seeder) StoreBundle() error {
	if s.Concurrency <= 0 {
		s.Concurrency = 8
	}
	// Run initializes the space index before its phases; StoreBundle is the
	// whole of install and runs on its own, so it loads the index itself.
	if err := s.loadIndex(); err != nil {
		return err
	}
	files, err := s.Bundle.Files()
	if err != nil {
		return err
	}
	slug := s.Model.Scenario.Name + "-scenario"
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would store %d scenario files in %s\n", len(files), slug)
		return nil
	}
	sp, _, err := s.ensureSpace(slug, s.Model.Scenario.Company+" scenario",
		s.baseLabels(map[string]string{"Layer": "demo"}))
	if err != nil {
		return err
	}
	existing, err := s.Client.ListUnits(sp.SpaceID, "")
	if err != nil {
		return err
	}
	bySlug := map[string]*goclient.Unit{}
	for _, u := range existing {
		bySlug[u.Slug] = u
	}
	stored := 0
	err = forEach(s, files, func(f scenario.BundleFile) error {
		unitSlug := fileSlug(f.Path)
		unit := bySlug[unitSlug]
		if unit == nil {
			var cerr error
			unit, cerr = s.Client.CreateUnit(sp.SpaceID, goclient.Unit{
				SpaceID:       sp.SpaceID,
				Slug:          unitSlug,
				DisplayName:   strings.ReplaceAll(f.Path, "/", " | "),
				ToolchainType: "AppConfig/YAML",
				Labels:        s.baseLabels(nil),
				Annotations:   map[string]string{"cub-demo.confighub.com/path": f.Path},
			})
			if cerr != nil {
				return fmt.Errorf("%s: %w", f.Path, cerr)
			}
		}
		sum := sha256.Sum256(f.Data)
		if strings.EqualFold(unit.DataHash, hex.EncodeToString(sum[:])) {
			return nil
		}
		s.mu.Lock()
		stored++
		s.mu.Unlock()
		if err := s.Client.UploadUnitData(sp.SpaceID, unit.UnitID, f.Data, "scenario bundle: "+f.Path); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "  stored scenario bundle in %s: %d files (%d new or changed)\n", slug, len(files), stored)
	return nil
}

// fileSlug turns a bundle path into a unit slug: manifests/ is dropped, the
// extension goes, separators become dashes.
func fileSlug(path string) string {
	p := strings.TrimPrefix(path, "manifests/")
	p = strings.TrimSuffix(p, ".yaml")
	p = strings.ReplaceAll(p, "/", "-")
	return strings.ReplaceAll(p, ".", "-")
}
