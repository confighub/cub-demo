package present

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/confighub/cub-demo/internal/cubexec"
)

// Release plays the delivery pipeline's publishing step: for every selected
// deployment space that has taken the change order but has not released it,
// it publishes a Release of the space's head, which is what a pipeline
// publishes when something in the space changed. The head, not the change
// order's own release tag: that tag marks each unit as the change order left
// it, so publishing by it would roll back whatever another change order
// promoted into the space since. The server counts a space as released for
// a change order once its released revisions are at or past the change
// order's end, whichever release got them there; a head release does not
// advance the change order's stored Stage on its own, so the stage is
// refreshed afterwards. The selection needs a change order: named as
// space/slug, or the component's one in flight; without a stage it is the
// stage the change order is in now. It never reports live status: that is
// observe's.
func (p *Presenter) Release(sel Selector) error {
	if sel.ChangeOrder == "" {
		if sel.Component == "" {
			return fmt.Errorf("release: name changeorder (space/slug) or component (its change order in flight is released)")
		}
		rollout, err := p.FindRollout(sel.Component, "")
		if err != nil {
			return err
		}
		sel.ChangeOrder = rollout.Space.Slug + "/" + rollout.ChangeOrder.Slug
	}
	spaces, rollout, err := p.Resolve(sel)
	if err != nil {
		return err
	}
	if len(spaces) == 0 {
		fmt.Fprintln(p.Out, "no deployment spaces match")
		return nil
	}
	released := map[uuid.UUID]bool{}
	for _, id := range rollout.ChangeOrder.ReleasedSpaceIDs {
		released[id] = true
	}
	published := 0
	for _, sp := range spaces {
		if released[sp.SpaceID] {
			fmt.Fprintf(p.Out, "  %s: already released %s\n", sp.Slug, rollout.ChangeOrder.Slug)
			continue
		}
		if err := p.runCub([]string{"release", "publish", sp.Slug}); err != nil {
			return fmt.Errorf("%s: %w", sp.Slug, err)
		}
		published++
	}
	if p.DryRun {
		return nil
	}
	fmt.Fprintf(p.Out, "released %s on %d of %d spaces\n", rollout.ChangeOrder.Slug, published, len(spaces))
	return p.refreshStage(rollout)
}

// refreshStage has the server work out the change order's Stage again: a
// Release published without naming the change order, or live status reported
// on one, satisfies its gates but does not advance the stored Stage.
func (p *Presenter) refreshStage(rollout *Rollout) error {
	if p.DryRun {
		return nil
	}
	_, err := cubexec.RunIn(p.Context, "changeorder", "refresh-stage", "--space", rollout.Space.Slug, rollout.ChangeOrder.Slug)
	return err
}
