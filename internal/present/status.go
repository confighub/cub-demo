package present

import (
	"fmt"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/seed"
)

// latestReleases returns the newest Release of each of the spaces that has
// one. Live status is a Release's, so this is where every observation lands.
func (p *Presenter) latestReleases(spaces []*goclient.Space) (map[uuid.UUID]*goclient.Release, error) {
	ids := make([]string, 0, len(spaces))
	for _, sp := range spaces {
		ids = append(ids, sp.SpaceID.String())
	}
	return p.Client.LatestReleases(fmt.Sprintf("SpaceID IN (%s)", quoteList(ids)))
}

// Argobot plays the delivery system: for every selected deployment space
// whose latest Release nobody has reported on, it writes the LiveStatus
// argobot would write after a successful sync. A Release already reported on
// is left alone unless force; spaces with no release are named and skipped,
// because argobot has nothing to observe there. It never publishes: releasing
// is a decision the presenter makes.
func (p *Presenter) Argobot(sel Selector, force bool) error {
	spaces, rollout, err := p.Resolve(sel)
	if err != nil {
		return err
	}
	if len(spaces) == 0 {
		fmt.Fprintln(p.Out, "no deployment spaces match")
		return nil
	}
	latest, err := p.latestReleases(spaces)
	if err != nil {
		return err
	}
	reported := 0
	for _, sp := range spaces {
		rel := latest[sp.SpaceID]
		if rel == nil {
			fmt.Fprintf(p.Out, "  %s: no release yet, nothing to observe\n", sp.Slug)
			continue
		}
		if st := rel.LiveStatus; st != nil && !force {
			fmt.Fprintf(p.Out, "  %s: release %d already reported %s at %s\n", sp.Slug, rel.ReleaseNum, st.ReporterHealth, st.ObservedAt.Format("2006-01-02T15:04:05Z"))
			continue
		}
		o := seed.Healthy
		o.Message = fmt.Sprintf("release %d rolled out", rel.ReleaseNum)
		if rollout != nil {
			o.Message = fmt.Sprintf("%s: rolled out (release %d)", rollout.ChangeOrder.Slug, rel.ReleaseNum)
		}
		if p.DryRun {
			fmt.Fprintf(p.Out, "  would report Synced/Healthy on %s (release %d)\n", sp.Slug, rel.ReleaseNum)
			continue
		}
		if err := p.Client.SetReleaseLiveStatus(rel.SpaceID, rel.ReleaseID, o.Status(sp.Slug)); err != nil {
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

// Incident reports a Degraded (or OutOfSync / Progressing) observation on the
// selected spaces, with a message the story can read out.
func (p *Presenter) Incident(sel Selector, health, message string) error {
	o := seed.Observation{Sync: "Synced", Phase: "Succeeded"}
	var defaultMessage func(component string) string
	switch health {
	case "Degraded", "":
		o.Health = "Degraded"
		defaultMessage = func(c string) string { return c + ": rollout stalled, 1 pod CrashLoopBackOff" }
	case "Progressing":
		o.Health = "Progressing"
		o.Phase = "Running"
		defaultMessage = func(c string) string { return c + ": rollout in progress" }
	case "OutOfSync":
		o.Health = "Healthy"
		o.Sync = "OutOfSync"
		defaultMessage = func(c string) string { return c + ": live state differs from the released bundle" }
	default:
		return fmt.Errorf("unknown health %q: want Degraded, Progressing or OutOfSync", health)
	}
	return p.reportAll(sel, o, message, defaultMessage)
}

// Heal reports Synced/Healthy on the selected spaces: the explicit
// counterpart to an incident.
func (p *Presenter) Heal(sel Selector, message string) error {
	return p.reportAll(sel, seed.Healthy, message, func(component string) string { return component + ": healthy" })
}

// reportAll writes the observation on the latest Release of each selected
// space. A space with no release is named and skipped: there is nothing
// running there to report on.
func (p *Presenter) reportAll(sel Selector, o seed.Observation, message string, defaultMessage func(component string) string) error {
	spaces, _, err := p.Resolve(sel)
	if err != nil {
		return err
	}
	if len(spaces) == 0 {
		fmt.Fprintln(p.Out, "no deployment spaces match")
		return nil
	}
	latest, err := p.latestReleases(spaces)
	if err != nil {
		return err
	}
	components, err := p.componentSlugs()
	if err != nil {
		return err
	}
	label := o.Sync + "/" + o.Health
	for _, sp := range spaces {
		rel := latest[sp.SpaceID]
		if rel == nil {
			fmt.Fprintf(p.Out, "  %s: no release yet, nothing to report on\n", sp.Slug)
			continue
		}
		o.Message = message
		if o.Message == "" {
			component := ""
			if sp.ComponentID != nil {
				component = components[*sp.ComponentID]
			}
			o.Message = defaultMessage(component)
		}
		if p.DryRun {
			fmt.Fprintf(p.Out, "  would report %s on %s: %s\n", label, sp.Slug, o.Message)
			continue
		}
		if err := p.Client.SetReleaseLiveStatus(rel.SpaceID, rel.ReleaseID, o.Status(sp.Slug)); err != nil {
			return fmt.Errorf("%s: %w", sp.Slug, err)
		}
		fmt.Fprintf(p.Out, "  reported %s on %s: %s\n", label, sp.Slug, o.Message)
	}
	return nil
}

// componentSlugs maps the demo's Component entities to their slugs, for
// naming the component a space is a variant of.
func (p *Presenter) componentSlugs() (map[uuid.UUID]string, error) {
	components, err := p.Client.ListComponents(demoWhere(p.Model))
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]string{}
	for _, c := range components {
		out[c.ComponentID] = c.Slug
	}
	return out, nil
}
