package api

import (
	"fmt"
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
)

func (a *API) createKey(ctx forge.Context, req *CreateKeyRequest) (*KeyCreateResponse, error) {
	input := &keysmith.CreateKeyInput{
		Name:        req.Name,
		Description: req.Description,
		Prefix:      req.Prefix,
		Environment: key.Environment(req.Environment),
		Scopes:      req.Scopes,
		Metadata:    req.Metadata,
		ExpiresAt:   req.ExpiresAt,
	}

	if req.PolicyID != "" {
		polID, err := id.ParsePolicyID(req.PolicyID)
		if err != nil {
			return nil, forge.BadRequest(fmt.Sprintf("invalid policy ID: %v", err))
		}
		input.PolicyID = &polID
	}

	result, err := a.eng.CreateKey(ctx.Context(), input)
	if err != nil {
		return nil, fmt.Errorf("create key: %w", err)
	}

	resp := &KeyCreateResponse{
		Key:    toKeyResponse(result.Key),
		RawKey: result.RawKey,
	}
	return resp, ctx.JSON(http.StatusCreated, resp)
}

func (a *API) getKey(ctx forge.Context, _ *GetKeyRequest) (*KeyResponse, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	k, err := a.eng.GetKey(ctx.Context(), keyID)
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := toKeyResponse(k)
	return resp, ctx.JSON(http.StatusOK, resp)
}

func (a *API) listKeys(ctx forge.Context, req *ListKeysRequest) ([]*KeyResponse, error) {
	keys, err := a.eng.ListKeys(ctx.Context(), &key.ListFilter{
		Environment: key.Environment(req.Environment),
		State:       key.State(req.State),
		Limit:       defaultLimit(req.Limit),
		Offset:      req.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}

	resp := make([]*KeyResponse, len(keys))
	for i, k := range keys {
		resp[i] = toKeyResponse(k)
	}
	return resp, ctx.JSON(http.StatusOK, resp)
}

func (a *API) deleteKey(ctx forge.Context, _ *DeleteKeyRequest) (*struct{}, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	if err := a.eng.RevokeKey(ctx.Context(), keyID, "deleted via API"); err != nil {
		return nil, mapStoreError(err)
	}

	return nil, ctx.NoContent(http.StatusNoContent)
}

func (a *API) rotateKey(ctx forge.Context, req *RotateKeyRequest) (*KeyCreateResponse, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	result, err := a.eng.RotateKey(ctx.Context(), keyID, rotation.Reason(req.Reason))
	if err != nil {
		return nil, fmt.Errorf("rotate key: %w", err)
	}

	resp := &KeyCreateResponse{
		Key:    toKeyResponse(result.Key),
		RawKey: result.RawKey,
	}
	return resp, ctx.JSON(http.StatusOK, resp)
}

func (a *API) revokeKey(ctx forge.Context, req *RevokeKeyRequest) (*struct{}, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	if err := a.eng.RevokeKey(ctx.Context(), keyID, req.Reason); err != nil {
		return nil, mapStoreError(err)
	}

	return nil, ctx.NoContent(http.StatusNoContent)
}

func (a *API) suspendKey(ctx forge.Context, _ *GetKeyRequest) (*struct{}, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	if err := a.eng.SuspendKey(ctx.Context(), keyID); err != nil {
		return nil, mapStoreError(err)
	}

	return nil, ctx.NoContent(http.StatusNoContent)
}

func (a *API) reactivateKey(ctx forge.Context, _ *GetKeyRequest) (*struct{}, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	if err := a.eng.ReactivateKey(ctx.Context(), keyID); err != nil {
		return nil, mapStoreError(err)
	}

	return nil, ctx.NoContent(http.StatusNoContent)
}
