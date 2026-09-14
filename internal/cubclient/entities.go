package cubclient

import (
	"bytes"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

var allowExists = "true"

// ListSpaces returns the spaces matching the where expression, org-wide.
func (c *Client) ListSpaces(where string) ([]*goclient.Space, error) {
	params := &goclient.ListSpacesParams{}
	if where != "" {
		params.Where = &where
	}
	res, err := c.api.ListSpacesWithResponse(c.ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	spaces := make([]*goclient.Space, 0, len(*res.JSON200))
	for _, ext := range *res.JSON200 {
		if ext.Space != nil {
			spaces = append(spaces, ext.Space)
		}
	}
	return spaces, nil
}

// EnsureSpace creates a space, tolerating an existing one, and returns it.
func (c *Client) EnsureSpace(space goclient.Space) (*goclient.Space, error) {
	res, err := c.api.CreateSpaceWithResponse(c.ctx, &goclient.CreateSpaceParams{AllowExists: &allowExists}, space)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("create space %q: %s", space.Slug, res.Status())
	}
	return res.JSON200, nil
}

// PatchSpace applies a merge patch to a space.
func (c *Client) PatchSpace(spaceID uuid.UUID, patch []byte) error {
	res, err := c.api.PatchSpaceWithBodyWithResponse(c.ctx, spaceID, &goclient.PatchSpaceParams{},
		"application/merge-patch+json", bytes.NewReader(patch))
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// DeleteSpace deletes a space and, recursively, everything in it. force uses
// recursive_force, which overrides delete gates.
func (c *Client) DeleteSpace(spaceID uuid.UUID, force bool) error {
	t := "true"
	params := &goclient.DeleteSpaceParams{Recursive: &t}
	if force {
		params.RecursiveForce = &t
	}
	res, err := c.api.DeleteSpaceWithResponse(c.ctx, spaceID, params)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// WorkerBySlug returns the worker with the given slug in the space, or nil.
func (c *Client) WorkerBySlug(spaceID uuid.UUID, slug string) (*goclient.BridgeWorker, error) {
	where := "Slug = '" + slug + "'"
	res, err := c.api.ListBridgeWorkersWithResponse(c.ctx, spaceID, &goclient.ListBridgeWorkersParams{Where: &where})
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	for _, ext := range *res.JSON200 {
		if ext.BridgeWorker != nil && ext.BridgeWorker.Slug == slug {
			return ext.BridgeWorker, nil
		}
	}
	return nil, nil
}

// CreateWorker creates a bridge worker.
func (c *Client) CreateWorker(spaceID uuid.UUID, worker goclient.BridgeWorker) (*goclient.BridgeWorker, error) {
	res, err := c.api.CreateBridgeWorkerWithResponse(c.ctx, spaceID, &goclient.CreateBridgeWorkerParams{}, worker)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("create worker %q: %s", worker.Slug, res.Status())
	}
	return res.JSON200, nil
}

// TargetBySlug returns the target with the given slug in the space, or nil.
func (c *Client) TargetBySlug(spaceID uuid.UUID, slug string) (*goclient.Target, error) {
	where := "Slug = '" + slug + "'"
	res, err := c.api.ListTargetsWithResponse(c.ctx, spaceID, &goclient.ListTargetsParams{Where: &where})
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	for _, ext := range *res.JSON200 {
		if ext.Target != nil && ext.Target.Slug == slug {
			return ext.Target, nil
		}
	}
	return nil, nil
}

// CreateTarget creates a target, tolerating an existing one.
func (c *Client) CreateTarget(spaceID uuid.UUID, target goclient.Target) (*goclient.Target, error) {
	res, err := c.api.CreateTargetWithResponse(c.ctx, spaceID, &goclient.CreateTargetParams{AllowExists: &allowExists}, target)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("create target %q: %s", target.Slug, res.Status())
	}
	return res.JSON200, nil
}

// CountTargets returns the number of targets matching the where expression,
// org-wide.
func (c *Client) CountTargets(where string) (int, error) {
	params := &goclient.ListAllTargetsParams{}
	if where != "" {
		params.Where = &where
	}
	res, err := c.api.ListAllTargetsWithResponse(c.ctx, params)
	if cubapi.IsAPIError(err, res) {
		return 0, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return 0, nil
	}
	return len(*res.JSON200), nil
}

// ListTargetsAll returns the targets matching the where expression, org-wide.
func (c *Client) ListTargetsAll(where string) ([]*goclient.Target, error) {
	params := &goclient.ListAllTargetsParams{}
	if where != "" {
		params.Where = &where
	}
	res, err := c.api.ListAllTargetsWithResponse(c.ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	targets := make([]*goclient.Target, 0, len(*res.JSON200))
	for _, ext := range *res.JSON200 {
		if ext.Target != nil {
			targets = append(targets, ext.Target)
		}
	}
	return targets, nil
}

// ListUnitsAll returns the units matching the where expression, org-wide.
func (c *Client) ListUnitsAll(where string) ([]*goclient.Unit, error) {
	params := &goclient.ListAllUnitsParams{}
	if where != "" {
		params.Where = &where
	}
	res, err := c.api.ListAllUnitsWithResponse(c.ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	units := make([]*goclient.Unit, 0, len(*res.JSON200))
	for _, ext := range *res.JSON200 {
		if ext.Unit != nil {
			units = append(units, ext.Unit)
		}
	}
	return units, nil
}

// CountUnits returns the number of units matching the where expression,
// org-wide.
func (c *Client) CountUnits(where string) (int, error) {
	units, err := c.ListUnitsAll(where)
	return len(units), err
}

// ChangeOrderBySlug returns the change order with the slug in the space, or
// nil if none exists.
func (c *Client) ChangeOrderBySlug(spaceID uuid.UUID, slug string) (*goclient.ChangeOrder, error) {
	where := "Slug = '" + slug + "'"
	res, err := c.api.ListChangeOrdersWithResponse(c.ctx, spaceID, &goclient.ListChangeOrdersParams{Where: &where})
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	for _, ext := range *res.JSON200 {
		if ext.ChangeOrder != nil && ext.ChangeOrder.Slug == slug {
			return ext.ChangeOrder, nil
		}
	}
	return nil, nil
}

// SpaceBySlug returns the space with the given slug, or nil if none exists.
func (c *Client) SpaceBySlug(slug string) (*goclient.Space, error) {
	spaces, err := c.ListSpaces("Slug = '" + slug + "'")
	if err != nil {
		return nil, err
	}
	for _, sp := range spaces {
		if sp.Slug == slug {
			return sp, nil
		}
	}
	return nil, nil
}

// ListChangeOrders lists the change orders in one space, optionally filtered.
func (c *Client) ListChangeOrders(spaceID uuid.UUID, where string) ([]*goclient.ChangeOrder, error) {
	params := &goclient.ListChangeOrdersParams{}
	if where != "" {
		params.Where = &where
	}
	res, err := c.api.ListChangeOrdersWithResponse(c.ctx, spaceID, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("list change orders: %s", res.Status())
	}
	out := make([]*goclient.ChangeOrder, 0, len(*res.JSON200))
	for i := range *res.JSON200 {
		if co := (*res.JSON200)[i].ChangeOrder; co != nil {
			out = append(out, co)
		}
	}
	return out, nil
}

// ListReleasesAll lists releases across the org, optionally filtered.
func (c *Client) ListReleasesAll(where string) ([]*goclient.Release, error) {
	params := &goclient.ListAllReleasesParams{}
	if where != "" {
		params.Where = &where
	}
	res, err := c.api.ListAllReleasesWithResponse(c.ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("list releases: %s", res.Status())
	}
	out := make([]*goclient.Release, 0, len(*res.JSON200))
	for i := range *res.JSON200 {
		if r := (*res.JSON200)[i].Release; r != nil {
			out = append(out, r)
		}
	}
	return out, nil
}
