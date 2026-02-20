package api

import (
	"fmt"
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/rotation"
)

func (a *API) listRotations(ctx forge.Context, req *ListRotationsRequest) ([]*RotationResponse, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	records, err := a.eng.ListRotations(ctx.Context(), &rotation.ListFilter{
		KeyID:  &keyID,
		Limit:  defaultLimit(req.Limit),
		Offset: req.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list rotations: %w", err)
	}

	resp := make([]*RotationResponse, len(records))
	for i, r := range records {
		resp[i] = toRotationResponse(r)
	}
	return resp, ctx.JSON(http.StatusOK, resp)
}
