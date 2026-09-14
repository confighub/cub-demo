package cubclient

import (
	"fmt"
	"strings"

	"github.com/confighub/sdk/core/cubapi"
	goclient "github.com/confighub/sdk/core/openapi/goclient-new"
	"github.com/google/uuid"
)

// ListUnits returns the units in a space matching the optional where filter.
func (c *Client) ListUnits(spaceID uuid.UUID, where string) ([]*goclient.Unit, error) {
	params := &goclient.ListUnitsParams{}
	if where != "" {
		params.Where = &where
	}
	res, err := c.api.ListUnitsWithResponse(c.ctx, spaceID, params)
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

// CreateUnit creates a unit, tolerating an existing one.
func (c *Client) CreateUnit(spaceID uuid.UUID, unit goclient.Unit) (*goclient.Unit, error) {
	res, err := c.api.CreateUnitWithResponse(c.ctx, spaceID, &goclient.CreateUnitParams{AllowExists: &allowExists}, unit)
	if cubapi.IsAPIError(err, res) {
		return nil, cubapi.InterpretErrorGeneric(err, res)
	}
	if res.JSON200 == nil || res.JSON200.Unit == nil {
		return nil, fmt.Errorf("create unit %q: %s", unit.Slug, res.Status())
	}
	return res.JSON200.Unit, nil
}

// UploadUnitData replaces a unit's configuration data, recording description
// as the change description.
func (c *Client) UploadUnitData(spaceID, unitID uuid.UUID, data []byte, description string) error {
	params := &goclient.UploadUnitDataParams{}
	if description != "" {
		params.LastChangeDescription = &description
	}
	res, err := c.api.UploadUnitDataWithBodyWithResponse(c.ctx, spaceID, unitID, params,
		"application/octet-stream", strings.NewReader(string(data)))
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}

// SetUnitProtection sets Protected flags on a unit's MutationSources: the
// /protection API, which records a revision only when something changes.
func (c *Client) SetUnitProtection(spaceID, unitID uuid.UUID, protection []goclient.ResourceProtection) error {
	res, err := c.api.SetUnitProtectionWithResponse(c.ctx, spaceID, unitID, goclient.UnitProtectionRequest{ResourceProtection: protection})
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	if res.StatusCode() >= 300 {
		return fmt.Errorf("set protection: %s", res.Status())
	}
	return nil
}

// DownloadUnitData returns a unit's configuration data at its head.
func (c *Client) DownloadUnitData(spaceID, unitID uuid.UUID) ([]byte, error) {
	// Not through cubapi.IsAPIError: it looks for a JSON body, and this
	// endpoint returns the data itself.
	res, err := c.api.DownloadUnitDataWithResponse(c.ctx, spaceID, unitID)
	if err != nil {
		return nil, err
	}
	if res.StatusCode() != 200 {
		return nil, cubapi.InterpretErrorGeneric(nil, res)
	}
	return res.Body, nil
}

// DeleteUnit deletes a unit.
func (c *Client) DeleteUnit(spaceID, unitID uuid.UUID) error {
	res, err := c.api.DeleteUnitWithResponse(c.ctx, spaceID, unitID)
	if cubapi.IsAPIError(err, res) {
		return cubapi.InterpretErrorGeneric(err, res)
	}
	return nil
}
