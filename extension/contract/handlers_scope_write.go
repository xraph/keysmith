package contract

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/scope"
)

const (
	maxScopeNameLength        = 100
	maxScopeDescriptionLength = 1000

	// maxScopeChildCount is the most children a refused delete counts by
	// number. Past it the message says "more than".
	maxScopeChildCount = 200
	// scopePolicyCountPage is how many policies a refused delete reads per
	// call while it counts the ones that allow the scope.
	scopePolicyCountPage = 100
)

type scopesCreateRequest struct {
	Name        string `json:"name"`
	Parent      string `json:"parent"`
	Description string `json:"description"`
}

type scopesCreateResponse struct {
	Scope ScopeSummary `json:"scope"`
}

type scopesDeleteRequest struct {
	ID string `json:"id"`
}

type scopesDeleteResponse struct {
	ID string `json:"id"`
}

func projectScopeSummary(s *scope.Scope) ScopeSummary {
	return ScopeSummary{
		ID:          s.ID.String(),
		Name:        s.Name,
		Parent:      s.Parent,
		Description: s.Description,
	}
}

// validateScope trims the request's fields in place and checks them. The
// first failure wins, in this order: name, description, parent. Every
// failure is BAD_REQUEST, except a parent lookup that fails for a reason
// other than not found: that comes back unmapped, for the handler to map.
func validateScope(ctx context.Context, deps Deps, tenant string, in *scopesCreateRequest) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return badRequest("name is required")
	}
	if utf8.RuneCountInString(in.Name) > maxScopeNameLength {
		return badRequest("name is too long")
	}
	if strings.IndexFunc(in.Name, unicode.IsSpace) >= 0 {
		return badRequest("name cannot contain spaces")
	}
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Description) > maxScopeDescriptionLength {
		return badRequest("description is too long")
	}
	in.Parent = strings.TrimSpace(in.Parent)
	if in.Parent == "" {
		return nil
	}
	if in.Parent == in.Name {
		return badRequest("a scope cannot be its own parent")
	}
	if _, err := deps.Engine.Store().Scopes().GetByName(ctx, tenant, in.Parent); err != nil {
		if errors.Is(err, keysmith.ErrScopeNotFound) {
			return badRequest("parent scope " + strconv.Quote(in.Parent) + " does not exist in this tenant")
		}
		return err
	}
	return nil
}

func scopesCreateHandler(deps Deps) func(context.Context, scopesCreateRequest, dashcontract.Principal) (scopesCreateResponse, error) {
	return func(ctx context.Context, in scopesCreateRequest, p dashcontract.Principal) (scopesCreateResponse, error) {
		const intent = "scopes.create"
		if _, err := requireUser(p); err != nil {
			return scopesCreateResponse{}, err
		}
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return scopesCreateResponse{}, err
		}
		app, err := appFrom(p, deps)
		if err != nil {
			return scopesCreateResponse{}, err
		}
		if err := validateScope(ctx, deps, tenant, &in); err != nil {
			var ce *dashcontract.Error
			if errors.As(err, &ce) {
				return scopesCreateResponse{}, err
			}
			return scopesCreateResponse{}, deps.mapError(intent, err)
		}
		sc := &scope.Scope{Name: in.Name, Parent: in.Parent, Description: in.Description}
		// engineCtx: CreateScope checks the name and stamps the tenant and
		// app from the context, and it prefers a forge Scope the host may
		// have set.
		if err := deps.Engine.CreateScope(engineCtx(ctx, app, tenant), sc); err != nil {
			return scopesCreateResponse{}, deps.mapError(intent, err)
		}
		return scopesCreateResponse{Scope: projectScopeSummary(sc)}, nil
	}
}

// scopeHasChildren answers a delete refused for children. n is this
// tenant's count, read with one row past the cap; 0 means the count is
// unknown, so the message names no number.
func scopeHasChildren(n int) error {
	msg := "scopes name this scope as their parent"
	switch {
	case n > maxScopeChildCount:
		msg = "more than " + strconv.Itoa(maxScopeChildCount) + " scopes name this scope as their parent"
	case n == 1:
		msg = "1 scope names this scope as its parent"
	case n > 1:
		msg = strconv.Itoa(n) + " scopes name this scope as their parent"
	}
	return &dashcontract.Error{Code: dashcontract.CodeConflict, Message: msg}
}

// scopeAllowedByPolicy answers a delete refused because policies allow the
// scope. n is this tenant's count; 0 means the count is unknown.
func scopeAllowedByPolicy(n int) error {
	msg := "policies allow this scope"
	switch {
	case n == 1:
		msg = "1 policy allows this scope"
	case n > 1:
		msg = strconv.Itoa(n) + " policies allow this scope"
	}
	return &dashcontract.Error{Code: dashcontract.CodeConflict, Message: msg}
}

// countScopeChildren counts the tenant's scopes that name s as their parent,
// reading at most one past the cap.
func countScopeChildren(ctx context.Context, deps Deps, tenant string, s *scope.Scope) (int, error) {
	rows, err := deps.Engine.ListScopes(ctx, &scope.ListFilter{TenantID: tenant, Parent: s.Name, Limit: maxScopeChildCount + 1})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// countAllowingPolicies counts the tenant's policies that list s in
// allowedScopes, paging until a short page, with the same exact-name match
// the engine's delete check uses.
func countAllowingPolicies(ctx context.Context, deps Deps, tenant string, s *scope.Scope) (int, error) {
	n := 0
	for offset := 0; ; offset += scopePolicyCountPage {
		pols, err := deps.Engine.ListPolicies(ctx, &policy.ListFilter{TenantID: tenant, Limit: scopePolicyCountPage, Offset: offset})
		if err != nil {
			return 0, err
		}
		for _, p := range pols {
			if slices.Contains(p.AllowedScopes, s.Name) {
				n++
			}
		}
		if len(pols) < scopePolicyCountPage {
			return n, nil
		}
	}
}

func scopesDeleteHandler(deps Deps) func(context.Context, scopesDeleteRequest, dashcontract.Principal) (scopesDeleteResponse, error) {
	return func(ctx context.Context, in scopesDeleteRequest, p dashcontract.Principal) (scopesDeleteResponse, error) {
		const intent = "scopes.delete"
		if _, err := requireUser(p); err != nil {
			return scopesDeleteResponse{}, err
		}
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return scopesDeleteResponse{}, err
		}
		sc, err := loadScopeForTenant(ctx, deps, tenant, in.ID, intent)
		if err != nil {
			return scopesDeleteResponse{}, err
		}
		// count reads this tenant's number for a refusal. A failed read is
		// logged and the message then names no number.
		count := func(what string, fn func(context.Context, Deps, string, *scope.Scope) (int, error)) int {
			n, cerr := fn(ctx, deps, tenant, sc)
			if cerr != nil {
				if deps.Logger != nil {
					deps.Logger.Error("keysmith/contract: count "+what+" for a refused scope delete",
						forge.F("intent", intent),
						forge.F("error", cerr),
					)
				}
				return 0
			}
			return n
		}
		err = deps.Engine.DeleteScope(ctx, sc.ID)
		switch {
		case errors.Is(err, keysmith.ErrScopeHasChildren):
			return scopesDeleteResponse{}, scopeHasChildren(count("child scopes", countScopeChildren))
		case errors.Is(err, keysmith.ErrScopeAllowedByPolicy):
			return scopesDeleteResponse{}, scopeAllowedByPolicy(count("policies", countAllowingPolicies))
		case errors.Is(err, keysmith.ErrScopeNotFound):
			// Deleted between the load and the engine's own read.
			return scopesDeleteResponse{}, scopeNotFound()
		case err != nil:
			return scopesDeleteResponse{}, deps.mapError(intent, err)
		}
		return scopesDeleteResponse{ID: sc.ID.String()}, nil
	}
}
