package seed

import (
	"fmt"
	"sort"
	"strings"

	goclient "github.com/confighub/sdk/core/openapi/goclient-new"

	"github.com/confighub/cub-demo/internal/scenario"
)

// protectScope says which of a class's spaces a protection pass covers.
type protectScope int

const (
	// protectClassBases runs as its own phase, after bases and before
	// deployments: a protection is a revision on the base, and a deployment
	// cloned before it would sit behind its upstream, which a change order
	// then refuses to promote past ("take the intervening changes first").
	protectClassBases protectScope = iota
	// protectDeployments runs at the end of the deployments phase, because a
	// clone starts unprotected whatever its upstream carries.
	protectDeployments
)

// protect marks the paths a component spec declares as local overrides on the
// spaces the scope names. Protection is a property of one unit's
// MutationSources, so each unit is marked directly. The server creates a
// revision only when the protection actually changes, so re-runs are no-ops.
func (s *Seeder) protect(scope protectScope) error {
	total := 0
	for _, cm := range s.Model.Components {
		if !cm.HasManifests || cm.Spec == nil || len(cm.Spec.Protect) == 0 {
			continue
		}
		classes := make([]string, 0, len(cm.Spec.Protect))
		for class := range cm.Spec.Protect {
			classes = append(classes, class)
		}
		sort.Strings(classes)
		for _, class := range classes {
			for _, p := range cm.Spec.Protect[class] {
				slugs := s.protectedSpaces(cm, class, p, scope)
				if len(slugs) == 0 {
					continue
				}
				rp, err := ParseProtections(p.Paths)
				if err != nil {
					return fmt.Errorf("%s %s: %w", cm.Name, class, err)
				}
				if s.DryRun {
					fmt.Fprintf(s.Out, "  would protect %d paths on %s in %d %s %s of %s\n", len(p.Paths), p.Unit, len(slugs), class, scope, cm.Name)
					continue
				}
				var ids []string
				for _, slug := range slugs {
					if sp := s.space(slug); sp != nil {
						ids = append(ids, "'"+sp.SpaceID.String()+"'")
					}
				}
				if len(ids) == 0 {
					continue
				}
				units, err := s.Client.ListUnitsAll(fmt.Sprintf("Slug = '%s' AND SpaceID IN (%s)", p.Unit, strings.Join(ids, ", ")))
				if err != nil {
					return fmt.Errorf("%s %s: list %s units: %w", cm.Name, class, p.Unit, err)
				}
				err = forEach(s, units, func(u *goclient.Unit) error {
					if err := s.Client.SetUnitProtection(u.SpaceID, u.UnitID, rp); err != nil {
						return fmt.Errorf("%s/%s: %w", u.SpaceSlug, u.Slug, err)
					}
					return nil
				})
				if err != nil {
					return err
				}
				total += len(units)
			}
		}
	}
	if !s.DryRun && total > 0 {
		fmt.Fprintf(s.Out, "  protected paths on %d %s\n", total, scope)
	}
	return nil
}

func (sc protectScope) String() string {
	if sc == protectClassBases {
		return "class bases"
	}
	return "deployments"
}

// protectedSpaces is where one protection applies within the scope: the class
// base (unless the protection names variants), or the class's deployments
// (all, or the named variants).
func (s *Seeder) protectedSpaces(cm *scenario.ComponentModel, class string, p scenario.Protection, scope protectScope) []string {
	var out []string
	if scope == protectClassBases {
		if len(p.Variants) > 0 {
			return nil
		}
		for _, cb := range cm.ClassBases {
			if cb.Class == class {
				out = append(out, cb.Space)
			}
		}
		return out
	}
	only := map[string]bool{}
	for _, v := range p.Variants {
		only[v] = true
	}
	for _, d := range cm.Deployments {
		if d.Class != class {
			continue
		}
		if len(only) > 0 && !only[d.Cluster.Name] {
			continue
		}
		out = append(out, d.Space)
	}
	return out
}

// ParseProtections turns RESOURCE_TYPE:RESOURCE_NAME:PATH strings into the
// per-resource request the protection API takes, grouping paths by resource.
func ParseProtections(paths []string) ([]goclient.ResourceProtection, error) {
	byResource := map[string]*goclient.ResourceProtection{}
	var order []string
	for _, p := range paths {
		parts := strings.SplitN(p, ":", 3)
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return nil, fmt.Errorf("protect path %q: want RESOURCE_TYPE:RESOURCE_NAME:PATH", p)
		}
		key := parts[0] + ":" + parts[1]
		rp, ok := byResource[key]
		if !ok {
			rp = &goclient.ResourceProtection{
				Resource:  &goclient.ResourceInfo{ResourceType: parts[0], ResourceName: parts[1]},
				Protected: map[string]bool{},
			}
			byResource[key] = rp
			order = append(order, key)
		}
		rp.Protected[parts[2]] = true
	}
	out := make([]goclient.ResourceProtection, 0, len(order))
	for _, key := range order {
		out = append(out, *byResource[key])
	}
	return out, nil
}
