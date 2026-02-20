package keysmith

import (
	"context"

	"github.com/xraph/forge"
)

type tenantScope struct {
	appID    string
	tenantID string
}

type ctxKeyApp struct{}
type ctxKeyTenant struct{}

// WithTenant sets the tenant scope on the context for standalone usage
// (without Forge). This is the non-Forge equivalent of forge.Scope.
func WithTenant(ctx context.Context, appID, tenantID string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyApp{}, appID)
	ctx = context.WithValue(ctx, ctxKeyTenant{}, tenantID)
	return ctx
}

// scopeFromContext extracts tenant scope from forge.Scope or standalone context.
// Falls back to explicit tenant if Forge scope is not set (standalone mode).
func scopeFromContext(ctx context.Context) tenantScope {
	fscope, ok := forge.ScopeFrom(ctx)
	if ok {
		return tenantScope{
			appID:    fscope.AppID(),
			tenantID: fscope.OrgID(),
		}
	}
	return tenantScope{
		appID:    appIDFromContext(ctx),
		tenantID: tenantIDFromContext(ctx),
	}
}

func appIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyApp{}).(string)
	return v
}

func tenantIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyTenant{}).(string)
	return v
}
