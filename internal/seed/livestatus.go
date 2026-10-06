package seed

import (
	"fmt"
	"time"

	"github.com/confighub/sdk/core/livestatus"
	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
)

// LiveStatusReporter is the Reporter on every status the tool writes. The
// demo's fiction is that Argo CD syncs these clusters, and the UI renders
// status chips in the reporting system's own dialect (Argo's
// Healthy/Progressing/Degraded) when the reporter names it — so the value
// both admits the author and claims the dialect.
const LiveStatusReporter = "cub-demo/argocd"

// Observation is one live-status report in Argo CD's words: what argobot
// would read off the Application syncing a deployment space.
type Observation struct {
	Sync, Health, Phase, Message string
}

// Healthy is the report of a deployment that rolled out cleanly.
var Healthy = Observation{Sync: "Synced", Health: "Healthy", Phase: "Succeeded"}

// Status renders the observation as a Release's LiveStatus. dataSource names
// the object observed; the fiction is an Argo CD Application named after the
// deployment space.
func (o Observation) Status(dataSource string) goclient.ReleaseLiveStatus {
	st := livestatus.FromArgoCD(o.Sync, o.Health, o.Phase)
	st.Reporter = LiveStatusReporter
	st.DataSource = dataSource
	st.Message = o.Message
	st.ObservedAt = time.Now().UTC()
	return st
}

// Reported says whether a Release already carries the observation, so a
// repaint that would change nothing is skipped.
func (o Observation) Reported(rel *goclient.Release) bool {
	st := rel.LiveStatus
	return st != nil && st.ReporterSync == o.Sync && st.ReporterHealth == o.Health &&
		st.ReporterOperation == o.Phase && st.Message == o.Message
}

// livestatusPhase writes the LiveStatus a GitOps operator would report on the
// latest Release of each deployment space: Synced/Healthy everywhere, with the
// story's deliberate exceptions. Status belongs to a Release, so a space that
// was never released has none. The phase converges: a Release already
// carrying its intended status is left alone.
func (s *Seeder) livestatusPhase() error {
	want := map[string]Observation{}
	for _, cm := range s.Model.Components {
		if !cm.HasManifests || cm.LiveStatus == "none" {
			continue
		}
		for _, d := range cm.Deployments {
			want[d.Space] = Healthy
		}
	}
	exceptions := s.storyExceptions()
	for slug, o := range exceptions {
		if _, ok := want[slug]; ok {
			want[slug] = o
		}
	}
	if s.DryRun {
		fmt.Fprintf(s.Out, "  would report live status on the latest release of %d deployments, %d of them story exceptions\n", len(want), len(exceptions))
		return nil
	}
	painted, err := s.report(want)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "  live status reported on %d deployments (%d written, %d story exceptions)\n", len(want), painted, len(exceptions))
	return s.dropLegacyStatusAnnotations()
}

// report writes each observation on the latest Release of its deployment
// space, skipping spaces with no Release and Releases already carrying it.
// It returns how many it wrote.
func (s *Seeder) report(want map[string]Observation) (int, error) {
	latest, err := s.Client.LatestReleases(fmt.Sprintf("Space.Labels.%s = '%s'", LabelDemoName, s.Model.Scenario.Name))
	if err != nil {
		return 0, fmt.Errorf("list releases: %w", err)
	}
	type job struct {
		slug string
		rel  *goclient.Release
		o    Observation
	}
	var jobs []job
	for slug, o := range want {
		sp := s.space(slug)
		if sp == nil {
			continue // pending content
		}
		rel := latest[sp.SpaceID]
		if rel == nil || o.Reported(rel) {
			continue
		}
		jobs = append(jobs, job{slug, rel, o})
	}
	err = forEach(s, jobs, func(j job) error {
		if err := s.Client.SetReleaseLiveStatus(j.rel.SpaceID, j.rel.ReleaseID, j.o.Status(j.slug)); err != nil {
			return fmt.Errorf("%s live status: %w", j.slug, err)
		}
		return nil
	})
	return len(jobs), err
}

// storyExceptions builds the unhealthy minority the story calls for, by
// deployment space.
func (s *Seeder) storyExceptions() map[string]Observation {
	out := map[string]Observation{}
	add := func(slugs []string, o func(component string) Observation) {
		for _, slug := range slugs {
			if d := s.Model.Deployment(slug); d != nil {
				out[slug] = o(d.Component)
			}
		}
	}
	add(s.Model.Story.Degraded, func(c string) Observation {
		return Observation{Sync: "Synced", Health: "Degraded", Phase: "Succeeded", Message: c + ": 1 of its pods is in CrashLoopBackOff"}
	})
	add(s.Model.Story.OutOfSync, func(c string) Observation {
		return Observation{Sync: "OutOfSync", Health: "Healthy", Phase: "Succeeded", Message: c + ": live state differs from the released bundle"}
	})
	add(s.Model.Story.Progressing, func(c string) Observation {
		return Observation{Sync: "Synced", Health: "Progressing", Phase: "Running", Message: c + ": rollout in progress"}
	})
	return out
}

// legacyStatusAnnotation is where live status lived before it moved onto the
// Release (server v0.8).
const legacyStatusAnnotation = "confighub.com/live-status"

// dropLegacyStatusAnnotations clears the Space annotation an org seeded
// before v0.8 still carries; nothing reads it any more.
func (s *Seeder) dropLegacyStatusAnnotations() error {
	stale := false
	s.mu.Lock()
	for _, sp := range s.spaces {
		if _, ok := sp.Annotations[legacyStatusAnnotation]; ok {
			stale = true
			delete(sp.Annotations, legacyStatusAnnotation)
		}
	}
	s.mu.Unlock()
	if !stale {
		return nil
	}
	patch := []byte(`{"Annotations":{"` + legacyStatusAnnotation + `":null}}`)
	if err := s.Client.BulkPatchSpaces(demoWhere(s.Model)+" AND Labels.Role = 'deployment'", patch); err != nil {
		return fmt.Errorf("clear legacy live-status annotations: %w", err)
	}
	fmt.Fprintln(s.Out, "  cleared the pre-v0.8 live-status annotations from the deployment spaces")
	return nil
}
