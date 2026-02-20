package api

import (
	"net/http"

	"github.com/xraph/forge"
)

func (a *API) validateKey(ctx forge.Context, req *ValidateKeyRequest) (*ValidationResponse, error) {
	result, err := a.eng.ValidateKey(ctx.Context(), req.RawKey)
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := toValidationResponse(result)
	return resp, ctx.JSON(http.StatusOK, resp)
}
