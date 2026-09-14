package cubclient

import (
	"bytes"
	"fmt"

	"github.com/confighub/sdk/core/cubapi"
	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
)

// mergePatch is the content type every bulk create/patch endpoint requires.
const mergePatch = "application/merge-patch+json"

// BulkCreateSpaces clones the spaces selected by params.Where, applying patch
// to every clone. Returns one response per clone; a 207 carries per-entity
// errors, which bulkErrors folds into one error.
func (c *Client) BulkCreateSpaces(params *goclient.BulkCreateSpacesParams, patch []byte) ([]goclient.SpaceCreateOrUpdateResponse, error) {
	res, err := c.api.BulkCreateSpacesWithBodyWithResponse(c.ctx, params, mergePatch, bytes.NewReader(patch))
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	var out []goclient.SpaceCreateOrUpdateResponse
	if res.JSON200 != nil {
		out = *res.JSON200
	} else if res.JSON207 != nil {
		out = *res.JSON207
	}
	var errs []string
	for _, r := range out {
		if r.Error != nil {
			errs = append(errs, r.Error.Message)
		}
	}
	return out, bulkErrors("space clone", errs)
}

// BulkCreateUnits clones the units selected by params into the destination
// spaces params.WhereSpace selects.
func (c *Client) BulkCreateUnits(params *goclient.BulkCreateUnitsParams, patch []byte) ([]goclient.UnitCreateOrUpdateResponse, error) {
	res, err := c.api.BulkCreateUnitsWithBodyWithResponse(c.ctx, params, mergePatch, bytes.NewReader(patch))
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	var out []goclient.UnitCreateOrUpdateResponse
	if res.JSON200 != nil {
		out = *res.JSON200
	} else if res.JSON207 != nil {
		out = *res.JSON207
	}
	var errs []string
	for _, r := range out {
		if r.Error != nil {
			errs = append(errs, r.Error.Message)
		}
	}
	return out, bulkErrors("unit clone", errs)
}

// BulkPatchSpaces applies one merge patch to every space params.Where selects.
func (c *Client) BulkPatchSpaces(where string, patch []byte) error {
	params := &goclient.BulkPatchSpacesParams{Where: &where}
	res, err := c.api.BulkPatchSpacesWithBodyWithResponse(c.ctx, params, mergePatch, bytes.NewReader(patch))
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	var errs []string
	if res.JSON207 != nil {
		for _, r := range *res.JSON207 {
			if r.Error != nil {
				errs = append(errs, r.Error.Message)
			}
		}
	}
	return bulkErrors("space patch", errs)
}

// BulkPatchUnits applies one merge patch to every unit params.Where selects,
// org-wide.
func (c *Client) BulkPatchUnits(where string, patch []byte) error {
	params := &goclient.BulkPatchUnitsParams{Where: &where}
	res, err := c.api.BulkPatchUnitsWithBodyWithResponse(c.ctx, params, mergePatch, bytes.NewReader(patch))
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	var errs []string
	if res.JSON207 != nil {
		for _, r := range *res.JSON207 {
			if r.Error != nil {
				errs = append(errs, r.Error.Message)
			}
		}
	}
	return bulkErrors("unit patch", errs)
}

func bulkErrors(what string, errs []string) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) > 3 {
		errs = append(errs[:3], fmt.Sprintf("... and %d more", len(errs)-3))
	}
	return fmt.Errorf("%s: %d failed: %v", what, len(errs), errs)
}
