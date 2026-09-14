package cubclient

import (
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// PublishRelease publishes a release of the space: the server bundles the head
// revision of every unit assigned to the space's release target.
func (c *Client) PublishRelease(spaceID uuid.UUID, labels map[string]string) error {
	res, err := c.api.PublishReleaseWithResponse(c.ctx, spaceID, goclient.ReleasePublishRequest{Labels: labels})
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if res.StatusCode() >= 300 {
		return fmt.Errorf("publish release: %s", res.Status())
	}
	return nil
}

// CountReleases returns the number of releases matching the where expression,
// org-wide.
func (c *Client) CountReleases(where string) (int, error) {
	params := &goclient.ListAllReleasesParams{}
	if where != "" {
		params.Where = &where
	}
	res, err := c.api.ListAllReleasesWithResponse(c.ctx, params)
	if cubapi.IsAPIError(err, res) {
		return 0, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return 0, nil
	}
	return len(*res.JSON200), nil
}

// CreateFilter creates a saved filter in a space, tolerating an existing one.
func (c *Client) CreateFilter(spaceID uuid.UUID, filter goclient.Filter) (*goclient.Filter, error) {
	res, err := c.api.CreateFilterWithResponse(c.ctx, spaceID, &goclient.CreateFilterParams{AllowExists: &allowExists}, filter)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("create filter %q: %s", filter.Slug, res.Status())
	}
	return res.JSON200, nil
}

// CreateView creates a view in a space, tolerating an existing one.
func (c *Client) CreateView(spaceID uuid.UUID, view goclient.View) error {
	res, err := c.api.CreateViewWithResponse(c.ctx, spaceID, &goclient.CreateViewParams{AllowExists: &allowExists}, view)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return fmt.Errorf("create view %q: %s", view.Slug, res.Status())
	}
	return nil
}

// CreateTrigger creates a trigger, tolerating an existing one.
func (c *Client) CreateTrigger(spaceID uuid.UUID, trigger goclient.Trigger) error {
	res, err := c.api.CreateTriggerWithResponse(c.ctx, spaceID, &goclient.CreateTriggerParams{AllowExists: &allowExists}, trigger)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return fmt.Errorf("create trigger %q: %s", trigger.Slug, res.Status())
	}
	return nil
}

// ViewBySlug returns the view with the slug in the space, or nil.
func (c *Client) ViewBySlug(spaceID uuid.UUID, slug string) (*goclient.View, error) {
	where := "Slug = '" + slug + "'"
	res, err := c.api.ListViewsWithResponse(c.ctx, spaceID, &goclient.ListViewsParams{Where: &where})
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	for _, ext := range *res.JSON200 {
		if ext.View != nil && ext.View.Slug == slug {
			return ext.View, nil
		}
	}
	return nil, nil
}

// DeleteView deletes a view.
func (c *Client) DeleteView(spaceID, viewID uuid.UUID) error {
	res, err := c.api.DeleteViewWithResponse(c.ctx, spaceID, viewID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// FilterBySlug returns the filter with the slug in the space, or nil.
func (c *Client) FilterBySlug(spaceID uuid.UUID, slug string) (*goclient.Filter, error) {
	where := "Slug = '" + slug + "'"
	res, err := c.api.ListFiltersWithResponse(c.ctx, spaceID, &goclient.ListFiltersParams{Where: &where})
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	for _, ext := range *res.JSON200 {
		if ext.Filter != nil && ext.Filter.Slug == slug {
			return ext.Filter, nil
		}
	}
	return nil, nil
}

// DeleteFilter deletes a filter.
func (c *Client) DeleteFilter(spaceID, filterID uuid.UUID) error {
	res, err := c.api.DeleteFilterWithResponse(c.ctx, spaceID, filterID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}
