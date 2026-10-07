package contract

import (
	"context"
	"strings"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
)

const (
	// tenantClaim is the claim key a tenant selector would populate,
	// matching the app_id convention authsome uses.
	tenantClaim = "tenant_id"
	// appClaim is the claim key for the caller's app.
	appClaim = "app_id"
)

// requireUser returns the caller's trimmed subject, or an UNAUTHENTICATED
// error when the request carries no signed-in user.
func requireUser(p dashcontract.Principal) (string, error) {
	if p.User != nil {
		if sub := strings.TrimSpace(p.User.Subject); sub != "" {
			return sub, nil
		}
	}
	return "", &dashcontract.Error{
		Code:    dashcontract.CodeUnauthenticated,
		Message: "keysmith: authentication required",
	}
}

// tenantFrom resolves the caller's tenant for a contract request.
//
// READ THIS BEFORE CHANGING IT. It is the most dangerous function here.
//
// The empty string is not a harmless zero. An empty TenantID in a store
// filter matches EVERY tenant's rows rather than none, so a handler that
// resolved "" would serve every tenant's keys, policies, scopes, rotations
// and usage to whoever opened the dashboard. For the commands it is worse:
// a write made in the wrong scope lands in, or acts on, a tenant the caller
// has no business touching.
//
// So this never defaults to empty. An unresolvable tenant refuses.
//
// Resolution order:
//  0. A signed-in user, or refuse with UNAUTHENTICATED.
//  1. The principal's tenant_id claim. A claim that is present but
//     unusable refuses here and never falls through.
//  2. The org of the forge Scope on ctx, when the Scope names one.
//  3. Deps.DefaultTenantID, for deployments that configure it.
//  4. Refuse with PERMISSION_DENIED.
//
// Step 2 is how the dashboard agrees with the engine. The engine takes the
// tenant from the forge Scope's org (scopeFromContext in keysmith's
// scope.go), and under authsome that Scope is the session's org: its auth
// middleware puts forge.NewOrgScope(app, org) on every request whose
// session has an active org. We trust it because the host sets it, server
// side, from the session it has just authenticated. Nothing the browser
// sends reaches it: the dashboard transport hands the dispatcher the HTTP
// request's own context, and the request body cannot write a context value.
// Read anything else from ctx and that argument stops holding, so don't.
//
// A Scope with an empty org (an app-only session, or a host that sets
// forge.NewAppScope) falls through to step 3. Reading its OrgID as the
// tenant would hand the stores "" and show every tenant's rows, and the
// engine would read the same "" for a write. An app-only session therefore
// gets the configured tenant, or a refusal when none is configured.
//
// Step 1 returns nothing today: the auth provider builds dashauth.UserInfo
// and sets no claims, so Principal.Claims is empty on every request. The
// claim read stays first because a claim is the one thing that names a
// tenant for this request on purpose, and because reading it costs nothing.
func tenantFrom(ctx context.Context, p dashcontract.Principal, deps Deps) (string, error) {
	// Identity comes first. Neither the Scope nor DefaultTenantID exists so
	// a request with no user at all can be served under a tenant.
	if _, err := requireUser(p); err != nil {
		return "", err
	}
	// A claim that is PRESENT but unusable is not the same as no claim, and
	// the difference decides whether the fallback is safe.
	//
	// No claim at all means nothing has been said about the tenant, so the
	// session's org or a configured default is a reasonable answer. A claim
	// that is present and does not resolve means something tried to say
	// which tenant this is and failed, and answering with a different tenant
	// is how "empty matches everything" gets reintroduced by somebody
	// following this function correctly. So a broken claim refuses, Scope or
	// no Scope.
	if raw, present := p.Claims[tenantClaim]; present {
		s, ok := raw.(string)
		if !ok || s == "" {
			return "", &dashcontract.Error{
				Code: dashcontract.CodePermissionDenied,
				Message: "tenant claim is present but unusable: refusing rather than " +
					"falling back to a different tenant",
			}
		}
		return s, nil
	}
	if org := scopeOrg(ctx); org != "" {
		return org, nil
	}
	if deps.DefaultTenantID != "" {
		return deps.DefaultTenantID, nil
	}
	return "", &dashcontract.Error{
		Code: dashcontract.CodePermissionDenied,
		Message: "no tenant in scope: keysmith cannot tell which tenant this request is for. " +
			"Set extensions.keysmith.dashboard.tenant_id for a single-tenant deployment.",
	}
}

// scopeOrg is the org of the forge Scope on ctx, or "" when ctx carries no
// Scope or an app-only one. Callers treat "" as "the Scope said nothing".
func scopeOrg(ctx context.Context) string {
	if sc, ok := forge.ScopeFrom(ctx); ok {
		return sc.OrgID()
	}
	return ""
}

// tenantSourceOf says where tenantFrom took the tenant from: "claim" when
// the principal carries a tenant_id claim, "scope" when the forge Scope on
// ctx names an org, and "config" when it fell back to Deps.DefaultTenantID.
// Call it only after tenantFrom has answered: a claim that is present but
// unusable refuses there, so here a present claim is a claim tenantFrom used.
func tenantSourceOf(ctx context.Context, p dashcontract.Principal, _ Deps) string {
	if _, present := p.Claims[tenantClaim]; present {
		return "claim"
	}
	if scopeOrg(ctx) != "" {
		return "scope"
	}
	return "config"
}

// appFrom resolves the app that labels rows the caller creates. The result
// may be empty: keysmith never filters by app, so a missing app only leaves
// a new row unlabelled and cannot widen what any read returns. A claim that
// is present but unusable still refuses, for the same reason tenantFrom
// refuses: something tried to name an app and failed, and quietly labelling
// the row with a different one would hide that.
func appFrom(p dashcontract.Principal, deps Deps) (string, error) {
	if raw, present := p.Claims[appClaim]; present {
		s, ok := raw.(string)
		if !ok || s == "" {
			return "", &dashcontract.Error{
				Code: dashcontract.CodePermissionDenied,
				Message: "app claim is present but unusable: refusing rather than " +
					"falling back to a different app",
			}
		}
		return s, nil
	}
	return deps.DefaultAppID, nil
}

// engineCtx is the context a handler hands the engine for a write that
// stamps a tenant on what it creates. The engine reads the tenant from the
// context and prefers a forge Scope over keysmith.WithTenant, so setting
// only WithTenant loses to any Scope the host put on the request (authsome's
// middleware sets one on every signed-in request). When the contract took
// its tenant from a claim or from config, that Scope can name another org,
// or none. The key would then be checked against this tenant's policy and
// scopes, stored under another, and never shown in this tenant's list.
// Setting both, to the tenant and app the contract resolved, means the
// engine reads the same answer whichever it looks at.
//
// An empty app still gets an org scope: keysmith never filters by app, and
// an org-level Scope is what makes the engine take the tenant from it.
func engineCtx(ctx context.Context, app, tenant string) context.Context {
	ctx = keysmith.WithTenant(ctx, app, tenant)
	return forge.WithScope(ctx, forge.NewOrgScope(app, tenant))
}
