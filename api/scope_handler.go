package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/scope"
)

func (a *API) createScope(ctx forge.Context, req *CreateScopeRequest) (*ScopeResponse, error) {
	sc := &scope.Scope{
		ID:          id.NewScopeID(),
		Name:        req.Name,
		Description: req.Description,
		Parent:      req.Parent,
		CreatedAt:   time.Now(),
	}

	if err := a.eng.CreateScope(ctx.Context(), sc); err != nil {
		return nil, fmt.Errorf("create scope: %w", err)
	}

	resp := toScopeResponse(sc)
	return resp, ctx.JSON(http.StatusCreated, resp)
}

func (a *API) listScopes(ctx forge.Context, req *ListScopesRequest) ([]*ScopeResponse, error) {
	scopes, err := a.eng.ListScopes(ctx.Context(), &scope.ListFilter{
		Parent: req.Parent,
		Limit:  defaultLimit(req.Limit),
		Offset: req.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list scopes: %w", err)
	}

	resp := make([]*ScopeResponse, len(scopes))
	for i, s := range scopes {
		resp[i] = toScopeResponse(s)
	}
	return resp, ctx.JSON(http.StatusOK, resp)
}

func (a *API) deleteScope(ctx forge.Context, _ *ListScopesRequest) (*struct{}, error) {
	scopeID, err := id.ParseScopeID(ctx.Param("scopeId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid scope ID: %v", err))
	}

	if err := a.eng.DeleteScope(ctx.Context(), scopeID); err != nil {
		return nil, mapStoreError(err)
	}

	return nil, ctx.NoContent(http.StatusNoContent)
}

func (a *API) assignScopes(ctx forge.Context, req *AssignScopesRequest) (*struct{}, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	if err := a.eng.AssignScopes(ctx.Context(), keyID, req.Scopes); err != nil {
		return nil, mapStoreError(err)
	}

	return nil, ctx.NoContent(http.StatusNoContent)
}

func (a *API) removeScopes(ctx forge.Context, req *RemoveScopesRequest) (*struct{}, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	if err := a.eng.RemoveScopes(ctx.Context(), keyID, req.Scopes); err != nil {
		return nil, mapStoreError(err)
	}

	return nil, ctx.NoContent(http.StatusNoContent)
}
