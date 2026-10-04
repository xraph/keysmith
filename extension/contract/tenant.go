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
// Do not read the tenant from the request context. A scope helper ported
// from keysmith's templ dashboard would do that, and what it finds is not
// ours to trust: keysmith itself puts nothing there on the contract path, so
// a bare host leaves it empty, while a host such as authsome's dashboard
// bridge sets a forge Scope for the session's own org and app. Neither is
// the tenant this function resolves. Once a handler has the tenant from
// here, it hands the engine engineCtx, which overrides whatever the request
// carried.
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
//  1. The principal's tenant_id claim, the canonical per-request surface.
//  2. Deps.DefaultTenantID, for single-tenant deployments that configure it.
//  3. Refuse with PERMISSION_DENIED.
//
// Step 1 returns nothing today: the auth provider builds dashauth.UserInfo
// and sets no claims, so Principal.Claims is empty on every request. The
// claim read is here because it is where the tenant belongs once a tenant
// selector exists, and because reading it costs nothing. Until then a
// multi-tenant deployment either configures DefaultTenantID or gets
// refusals, which is the right behaviour for a dashboard that cannot tell
// which tenant it is looking at.
func tenantFrom(p dashcontract.Principal, deps Deps) (string, error) {
	// Identity comes first. DefaultTenantID exists so a single-tenant
	// deployment can answer without a tenant claim, not so a request with
	// no user at all can be served under that tenant.
	if _, err := requireUser(p); err != nil {
		return "", err
	}
	// A claim that is PRESENT but unusable is not the same as no claim, and
	// the difference decides whether the fallback is safe.
	//
	// No claim at all means nothing has been said about the tenant, so a
	// configured default is a reasonable answer. A claim that is present
	// and does not resolve means something tried to say which tenant this
	// is and failed, and answering with a different tenant is how "empty
	// matches everything" gets reintroduced by somebody following this
	// function correctly. So a broken claim refuses.
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
	if deps.DefaultTenantID != "" {
		return deps.DefaultTenantID, nil
	}
	return "", &dashcontract.Error{
		Code: dashcontract.CodePermissionDenied,
		Message: "no tenant in scope: keysmith cannot tell which tenant this request is for. " +
			"Set extensions.keysmith.dashboard.tenant_id for a single-tenant deployment.",
	}
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
// dashboard bridge sets one for the session's org on every request). The key
// would then be checked against this tenant's policy and scopes, stored
// under another, and never shown in this tenant's list. Setting both, to the
// tenant and app the contract resolved, means the engine reads the same
// answer whichever it looks at.
//
// An empty app still gets an org scope: keysmith never filters by app, and
// an org-level Scope is what makes the engine take the tenant from it.
func engineCtx(ctx context.Context, app, tenant string) context.Context {
	ctx = keysmith.WithTenant(ctx, app, tenant)
	return forge.WithScope(ctx, forge.NewOrgScope(app, tenant))
}
