package seed

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/confighub/sdk/core/livestatus"

	"github.com/confighub/cub-demo/internal/scenario"
)

// liveStatusSource names the reporter. The demo's fiction is that Argo CD
// syncs these clusters, and the UI renders status chips in the reporting
// system's own dialect (Argo's Healthy/Progressing/Degraded rather than the
// generic house wording) when the source names it — so the source both admits
// the author and claims the dialect.
const liveStatusSource = "cub-demo/argocd"

// livestatusPhase writes the confighub.com/live-status annotation a GitOps
// operator would report: Synced/Healthy everywhere, then the story's
// deliberate exceptions. Everything is repainted on every run (the annotation
// is observed state, not config; rewriting it is what an operator would do),
// with the exceptions painted after the bulk default so they survive it.
func (s *Seeder) livestatusPhase() error {
	now := time.Now().UTC().Format(time.RFC3339)
	healthy := livestatus.Status{
		Source:         liveStatusSource,
		SyncStatus:     "Synced",
		HealthStatus:   "Healthy",
		OperationPhase: "Succeeded",
		ObservedAt:     now,
	}

	type pair struct {
		cm *scenario.ComponentModel
		cb *scenario.ClassBase
	}
	var pairs []pair
	for _, cm := range s.Model.Components {
		if !cm.HasManifests || cm.LiveStatus == "none" {
			continue
		}
		for _, cb := range cm.ClassBases {
			pairs = append(pairs, pair{cm, cb})
		}
	}
	exceptions := s.storyExceptions(now)
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would paint live status on %d (component, class) groups with %d exceptions\n", len(pairs), len(exceptions))
		return nil
	}

	patch, err := statusPatch(healthy)
	if err != nil {
		return err
	}
	err = forEach(s, pairs, func(p pair) error {
		where := fmt.Sprintf("%s AND Labels.Component = '%s' AND Labels.Stage = '%s' AND Labels.Role = 'deployment'",
			demoWhere(s.Model), p.cm.Name, p.cb.Class)
		if err := s.Client.BulkPatchSpaces(where, patch); err != nil {
			return fmt.Errorf("%s %s live status: %w", p.cm.Name, p.cb.Class, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = forEach(s, exceptions, func(e statusException) error {
		sp := s.space(e.slug)
		if sp == nil {
			return nil // pending content; nothing to paint
		}
		patch, err := statusPatch(e.status)
		if err != nil {
			return err
		}
		return s.Client.PatchSpace(sp.SpaceID, patch)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "  painted live status on %d (component, class) groups, %d story exceptions\n", len(pairs), len(exceptions))
	return nil
}

// statusException is one deliberately unhealthy deployment.
type statusException struct {
	slug   string
	status livestatus.Status
}

// storyExceptions builds the unhealthy minority the story calls for.
func (s *Seeder) storyExceptions(now string) []statusException {
	var out []statusException
	add := func(slugs []string, status func(component string) livestatus.Status) {
		for _, slug := range slugs {
			d := s.Model.Deployment(slug)
			if d == nil {
				continue
			}
			out = append(out, statusException{slug, status(d.Component)})
		}
	}
	add(s.Model.Story.Degraded, func(c string) livestatus.Status {
		return livestatus.Status{Source: liveStatusSource, SyncStatus: "Synced", HealthStatus: "Degraded",
			OperationPhase: "Succeeded", Message: c + ": 1 of its pods is in CrashLoopBackOff", ObservedAt: now}
	})
	add(s.Model.Story.OutOfSync, func(c string) livestatus.Status {
		return livestatus.Status{Source: liveStatusSource, SyncStatus: "OutOfSync", HealthStatus: "Healthy",
			OperationPhase: "Succeeded", Message: c + ": live state differs from the released bundle", ObservedAt: now}
	})
	add(s.Model.Story.Progressing, func(c string) livestatus.Status {
		return livestatus.Status{Source: liveStatusSource, SyncStatus: "Synced", HealthStatus: "Progressing",
			OperationPhase: "Running", Message: c + ": rollout in progress", ObservedAt: now}
	})
	return out
}

func statusPatch(st livestatus.Status) ([]byte, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"Annotations": map[string]string{livestatus.Annotation: string(raw)}})
}
