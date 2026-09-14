package seed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/confighub/cub-demo/internal/cubexec"
	"github.com/confighub/cub-demo/internal/scenario"
)

// workflowSlug names a component's ChangeWorkflow in the home space.
func workflowSlug(cm *scenario.ComponentModel) string { return cm.Name + "-workflow" }

// workflowHashAnnotation records the definition a workflow entity was last
// seeded with, so re-runs skip unchanged ones.
const workflowHashAnnotation = "cub-demo.confighub.com/def-hash"

// workflows creates one ChangeWorkflow entity per component in the home space
// (v0.4.15+: workflows are first-class entities; the SDK this plugin pins has
// no client for them, so creation shells out through cub). A workflow unit
// left behind by a pre-entity seeding is deleted: the server no longer reads
// them, so keeping one would only invite editing the wrong thing.
func (s *Seeder) workflows() error {
	if s.Model.Scenario.Workflows.Disabled {
		fmt.Fprintln(s.Out, "  workflows disabled by the scenario")
		return nil
	}
	home := s.space(s.Model.Home)
	if home == nil && !s.DryRun {
		return fmt.Errorf("home space %s does not exist; run the home phase first", s.Model.Home)
	}
	var comps []*scenario.ComponentModel
	for _, cm := range s.Model.Components {
		if cm.HasManifests {
			comps = append(comps, cm)
		}
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would ensure %d workflow entities in %s\n", len(comps), s.Model.Home)
		return nil
	}

	// One-time migration: drop the KRM workflow units a pre-v0.4.15 seeding
	// created under the same slugs.
	units, err := s.Client.ListUnits(home.SpaceID, "")
	if err != nil {
		return err
	}
	for _, u := range units {
		if strings.HasSuffix(u.Slug, "-workflow") && u.ToolchainType == "AppConfig/YAML" {
			if err := s.Client.DeleteUnit(home.SpaceID, u.UnitID); err != nil {
				return fmt.Errorf("delete legacy workflow unit %s: %w", u.Slug, err)
			}
			fmt.Fprintf(s.Out, "  deleted legacy workflow unit %s (workflows are entities since v0.4.15)\n", u.Slug)
		}
	}

	ensured := 0
	err = forEach(s, comps, func(cm *scenario.ComponentModel) error {
		body := s.Model.WorkflowEntityJSON(cm)
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:8])
		slug := workflowSlug(cm)

		existing, err := cubexec.Run("changeworkflow", "get", slug, "--space", s.Model.Home, "--quiet", "-o", "json")
		verb := "update"
		if err != nil {
			if !strings.Contains(existing, "not found") {
				return fmt.Errorf("workflow %s: %w", slug, err)
			}
			verb = "create"
		} else {
			var got struct {
				ChangeWorkflow struct {
					Annotations map[string]string `json:"Annotations"`
				} `json:"ChangeWorkflow"`
			}
			// The output may be the entity itself or the extended wrapper.
			var plain struct {
				Annotations map[string]string `json:"Annotations"`
			}
			if jerr := json.Unmarshal([]byte(existing), &got); jerr == nil && got.ChangeWorkflow.Annotations[workflowHashAnnotation] == hash {
				return nil
			}
			if jerr := json.Unmarshal([]byte(existing), &plain); jerr == nil && plain.Annotations[workflowHashAnnotation] == hash {
				return nil
			}
		}
		args := []string{"changeworkflow", verb, "--space", s.Model.Home, slug, "--from-stdin",
			"--label", LabelDemoName + "=" + s.Model.Scenario.Name,
			"--label", "Component=" + cm.Name,
			"--annotation", workflowHashAnnotation + "=" + hash}
		if _, err := cubexec.RunWithStdin(string(body), args...); err != nil {
			return fmt.Errorf("workflow %s: %w", slug, err)
		}
		s.mu.Lock()
		ensured++
		s.mu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "  ensured %d workflow entities in %s (%d created or updated)\n", len(comps), s.Model.Home, ensured)
	return nil
}
