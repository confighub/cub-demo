package cubclient

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// ListComponents returns the Components matching the where expression. A
// Component is in no space: it belongs to the organization.
func (c *Client) ListComponents(where string) ([]*goclient.Component, error) {
	params := &goclient.ListComponentsParams{}
	if where != "" {
		params.Where = &where
	}
	res, err := c.api.ListComponentsWithResponse(c.ctx, params)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	out := make([]*goclient.Component, 0, len(*res.JSON200))
	for _, ext := range *res.JSON200 {
		if ext.Component != nil {
			out = append(out, ext.Component)
		}
	}
	return out, nil
}

// ComponentBySlug returns the Component with the slug, or nil.
func (c *Client) ComponentBySlug(slug string) (*goclient.Component, error) {
	components, err := c.ListComponents("Slug = '" + slug + "'")
	if err != nil {
		return nil, err
	}
	for _, comp := range components {
		if comp.Slug == slug {
			return comp, nil
		}
	}
	return nil, nil
}

// EnsureComponent creates a Component, tolerating an existing one.
func (c *Client) EnsureComponent(component goclient.Component) (*goclient.Component, error) {
	res, err := c.api.CreateComponentWithResponse(c.ctx, &goclient.CreateComponentParams{AllowExists: &allowExists}, component)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("create component %q: %s", component.Slug, res.Status())
	}
	return res.JSON200, nil
}

// PatchComponent applies a merge patch to a Component.
func (c *Client) PatchComponent(componentID uuid.UUID, patch []byte) error {
	res, err := c.api.PatchComponentWithBodyWithResponse(c.ctx, componentID, &goclient.PatchComponentParams{},
		mergePatch, bytes.NewReader(patch))
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// DeleteComponent deletes a Component. The server refuses while a Space
// still names it.
func (c *Client) DeleteComponent(componentID uuid.UUID) error {
	res, err := c.api.DeleteComponentWithResponse(c.ctx, componentID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// ListChangeWorkflows returns the ChangeWorkflows of one space.
func (c *Client) ListChangeWorkflows(spaceID uuid.UUID) ([]*goclient.ChangeWorkflow, error) {
	res, err := c.api.ListChangeWorkflowsWithResponse(c.ctx, spaceID, &goclient.ListChangeWorkflowsParams{})
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, nil
	}
	out := make([]*goclient.ChangeWorkflow, 0, len(*res.JSON200))
	for _, ext := range *res.JSON200 {
		if ext.ChangeWorkflow != nil {
			out = append(out, ext.ChangeWorkflow)
		}
	}
	return out, nil
}

// CreateChangeWorkflow creates a ChangeWorkflow in the space.
func (c *Client) CreateChangeWorkflow(spaceID uuid.UUID, wf goclient.ChangeWorkflow) (*goclient.ChangeWorkflow, error) {
	res, err := c.api.CreateChangeWorkflowWithResponse(c.ctx, spaceID, &goclient.CreateChangeWorkflowParams{}, wf)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("create change workflow %q: %s", wf.Slug, res.Status())
	}
	return res.JSON200, nil
}

// UpdateChangeWorkflow replaces a ChangeWorkflow with the given definition.
func (c *Client) UpdateChangeWorkflow(wf goclient.ChangeWorkflow) error {
	res, err := c.api.UpdateChangeWorkflowWithResponse(c.ctx, wf.SpaceID, wf.ChangeWorkflowID, &goclient.UpdateChangeWorkflowParams{}, wf)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// DeleteChangeWorkflow deletes a ChangeWorkflow.
func (c *Client) DeleteChangeWorkflow(spaceID, changeWorkflowID uuid.UUID) error {
	res, err := c.api.DeleteChangeWorkflowWithResponse(c.ctx, spaceID, changeWorkflowID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// CreateChangeOrder creates a ChangeOrder in the space. The server mints its
// start and end tags and, for one naming a ChangeWorkflow, works out the
// spaces it is headed for from the space's Component.
func (c *Client) CreateChangeOrder(spaceID uuid.UUID, co goclient.ChangeOrder) (*goclient.ChangeOrder, error) {
	res, err := c.api.CreateChangeOrderWithResponse(c.ctx, spaceID, &goclient.CreateChangeOrderParams{}, co)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("create change order %q: %s", co.Slug, res.Status())
	}
	return res.JSON200, nil
}

// PromoteStage promotes a ChangeOrder into every space of one stage of its
// ChangeWorkflow. The server checks the stage's entry gates and promotes
// space by space; a space or unit that failed comes back in the result, and
// every one of them is folded into the returned error.
func (c *Client) PromoteStage(changeOrderID uuid.UUID, stage string) error {
	res, err := c.api.PromoteWithResponse(c.ctx, &goclient.PromoteParams{},
		goclient.PromoteRequest{ChangeOrderID: &changeOrderID, TargetStage: stage})
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	result := res.JSON200
	if result == nil {
		result = res.JSON207
	}
	if result == nil {
		return fmt.Errorf("promote stage %s: %s", stage, res.Status())
	}
	var failed []string
	for _, sp := range result.Spaces {
		if sp.Error != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", sp.SpaceSlug, sp.Error.Message))
		}
		for _, u := range sp.Units {
			if u.Error != nil {
				failed = append(failed, fmt.Sprintf("%s/%s: %s", sp.SpaceSlug, u.Slug, u.Error.Message))
			}
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("promote stage %s: %s", stage, strings.Join(failed, "; "))
	}
	return nil
}
