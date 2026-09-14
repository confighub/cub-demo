package present

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/confighub/sdk/core/livestatus"
	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
)

// liveStatusSource is what the faked argobot signs its reports with; the same
// value the seeder paints, so a re-seed and a live report are indistinguishable.
const liveStatusSource = "cub-demo/argocd"

func observation(sp *goclient.Space) (livestatus.Status, bool) {
	raw := sp.Annotations[livestatus.Annotation]
	if raw == "" {
		return livestatus.Status{}, false
	}
	var st livestatus.Status
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return livestatus.Status{}, false
	}
	return st, true
}

func observedAt(st livestatus.Status) time.Time {
	t, err := time.Parse(time.RFC3339, st.ObservedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

func healthy(message string) livestatus.Status {
	return livestatus.Status{Source: liveStatusSource, SyncStatus: "Synced", HealthStatus: "Healthy",
		OperationPhase: "Succeeded", Message: message, ObservedAt: now()}
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func (p *Presenter) paint(sp *goclient.Space, st livestatus.Status) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	patch, err := json.Marshal(map[string]any{"Annotations": map[string]string{livestatus.Annotation: string(raw)}})
	if err != nil {
		return err
	}
	return p.Client.PatchSpace(sp.SpaceID, patch)
}

// Argobot plays the delivery system: for every selected deployment space whose
// latest release postdates its last observation, it writes the live-status
// annotation argobot would write after a successful sync. Spaces already
// reported on are left alone unless force; spaces with no release are named
// and skipped, because argobot has nothing to observe there. It never
// publishes: releasing is a decision the presenter makes.
func (p *Presenter) Argobot(sel Selector, force bool) error {
	spaces, rollout, err := p.Resolve(sel)
	if err != nil {
		return err
	}
	if len(spaces) == 0 {
		fmt.Fprintln(p.Out, "no deployment spaces match")
		return nil
	}
	ids := make([]string, 0, len(spaces))
	for _, sp := range spaces {
		ids = append(ids, sp.SpaceID.String())
	}
	releases, err := p.Client.ListReleasesAll(fmt.Sprintf("SpaceID IN (%s)", quoteList(ids)))
	if err != nil {
		return err
	}
	latest := map[string]*goclient.Release{}
	for _, r := range releases {
		key := r.SpaceID.String()
		if cur := latest[key]; cur == nil || r.CreatedAt.After(cur.CreatedAt) {
			latest[key] = r
		}
	}
	reported := 0
	for _, sp := range spaces {
		rel := latest[sp.SpaceID.String()]
		if rel == nil {
			fmt.Fprintf(p.Out, "  %s: no release yet, nothing to observe\n", sp.Slug)
			continue
		}
		// ObservedAt is second-granular; a report made in the same second as the
		// release counts as after it.
		if st, ok := observation(sp); ok && !force && !observedAt(st).Before(rel.CreatedAt.Truncate(time.Second)) {
			fmt.Fprintf(p.Out, "  %s: release %d already reported %s at %s\n", sp.Slug, rel.ReleaseNum, st.HealthStatus, st.ObservedAt)
			continue
		}
		msg := fmt.Sprintf("release %d rolled out", rel.ReleaseNum)
		if rollout != nil {
			msg = fmt.Sprintf("%s: rolled out (release %d)", rollout.ChangeOrder.Slug, rel.ReleaseNum)
		}
		if p.DryRun {
			fmt.Fprintf(p.Out, "  would report Synced/Healthy on %s (release %d)\n", sp.Slug, rel.ReleaseNum)
			continue
		}
		if err := p.paint(sp, healthy(msg)); err != nil {
			return fmt.Errorf("%s: %w", sp.Slug, err)
		}
		fmt.Fprintf(p.Out, "  reported Synced/Healthy on %s (release %d)\n", sp.Slug, rel.ReleaseNum)
		reported++
	}
	if !p.DryRun {
		fmt.Fprintf(p.Out, "argobot reported on %d of %d spaces\n", reported, len(spaces))
	}
	return nil
}

// Incident paints a Degraded (or OutOfSync / Progressing) observation on the
// selected spaces, with a message the story can read out.
func (p *Presenter) Incident(sel Selector, health, message string) error {
	st := livestatus.Status{Source: liveStatusSource, SyncStatus: "Synced", OperationPhase: "Succeeded", ObservedAt: now()}
	switch health {
	case "Degraded", "":
		st.HealthStatus = "Degraded"
	case "Progressing":
		st.HealthStatus = "Progressing"
		st.OperationPhase = "Running"
	case "OutOfSync":
		st.HealthStatus = "Healthy"
		st.SyncStatus = "OutOfSync"
	default:
		return fmt.Errorf("unknown health %q: want Degraded, Progressing or OutOfSync", health)
	}
	return p.paintAll(sel, st, message, func(component string) string {
		switch st.HealthStatus + "/" + st.SyncStatus {
		case "Progressing/Synced":
			return component + ": rollout in progress"
		case "Healthy/OutOfSync":
			return component + ": live state differs from the released bundle"
		}
		return component + ": rollout stalled, 1 pod CrashLoopBackOff"
	})
}

// Heal paints Synced/Healthy on the selected spaces: the explicit counterpart
// to an incident.
func (p *Presenter) Heal(sel Selector, message string) error {
	return p.paintAll(sel, healthy(""), message, func(component string) string { return component + ": healthy" })
}

func (p *Presenter) paintAll(sel Selector, st livestatus.Status, message string, defaultMessage func(component string) string) error {
	spaces, _, err := p.Resolve(sel)
	if err != nil {
		return err
	}
	if len(spaces) == 0 {
		fmt.Fprintln(p.Out, "no deployment spaces match")
		return nil
	}
	for _, sp := range spaces {
		st.Message = message
		if st.Message == "" {
			st.Message = defaultMessage(sp.Labels["Component"])
		}
		st.ObservedAt = now()
		label := st.SyncStatus + "/" + st.HealthStatus
		if p.DryRun {
			fmt.Fprintf(p.Out, "  would paint %s on %s: %s\n", label, sp.Slug, st.Message)
			continue
		}
		if err := p.paint(sp, st); err != nil {
			return fmt.Errorf("%s: %w", sp.Slug, err)
		}
		fmt.Fprintf(p.Out, "  painted %s on %s: %s\n", label, sp.Slug, st.Message)
	}
	return nil
}
