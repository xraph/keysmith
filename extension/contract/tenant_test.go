package contract

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge"
	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
)

func user(claims map[string]any) dashcontract.Principal {
	return dashcontract.Principal{User: &dashauth.UserInfo{Subject: "user_1"}, Claims: claims}
}

func codeOf(t *testing.T, err error) dashcontract.ErrorCode {
	t.Helper()
	var ce *dashcontract.Error
	require.ErrorAs(t, err, &ce)
	return ce.Code
}

// orgCtx carries the Scope authsome's middleware sets for a session with an
// active org.
func orgCtx(org string) context.Context {
	return forge.WithScope(context.Background(), forge.NewOrgScope("app_x", org))
}

// appOnlyCtx carries the Scope authsome's middleware sets for a session with
// no org.
func appOnlyCtx() context.Context {
	return forge.WithScope(context.Background(), forge.NewAppScope("app_x"))
}

func TestTenantFrom(t *testing.T) {
	bare := context.Background()
	withDefault := Deps{DefaultTenantID: "acme"}
	cases := []struct {
		name string
		ctx  context.Context
		p    dashcontract.Principal
		deps Deps
		want string
		code dashcontract.ErrorCode
	}{
		{"no user refuses even with a default", bare, dashcontract.Principal{}, withDefault, "", dashcontract.CodeUnauthenticated},
		{"no user refuses even with a scope org", orgCtx("org_1"), dashcontract.Principal{}, withDefault, "", dashcontract.CodeUnauthenticated},
		{"blank subject refuses", bare, dashcontract.Principal{User: &dashauth.UserInfo{Subject: "  "}}, withDefault, "", dashcontract.CodeUnauthenticated},
		{"claim wins over default", bare, user(map[string]any{"tenant_id": "globex"}), withDefault, "globex", ""},
		{"claim wins over scope org", orgCtx("org_1"), user(map[string]any{"tenant_id": "globex"}), withDefault, "globex", ""},
		{"empty claim refuses, never defaults", bare, user(map[string]any{"tenant_id": ""}), withDefault, "", dashcontract.CodePermissionDenied},
		{"empty claim refuses even with a scope org", orgCtx("org_1"), user(map[string]any{"tenant_id": ""}), withDefault, "", dashcontract.CodePermissionDenied},
		{"wrong-typed claim refuses", bare, user(map[string]any{"tenant_id": 42}), withDefault, "", dashcontract.CodePermissionDenied},
		{"wrong-typed claim refuses even with a scope org", orgCtx("org_1"), user(map[string]any{"tenant_id": 42}), Deps{}, "", dashcontract.CodePermissionDenied},
		{"nil claim refuses", bare, user(map[string]any{"tenant_id": nil}), withDefault, "", dashcontract.CodePermissionDenied},
		{"scope org wins over default", orgCtx("org_1"), user(nil), withDefault, "org_1", ""},
		{"scope org needs no default", orgCtx("org_1"), user(nil), Deps{}, "org_1", ""},
		{"app-only scope takes the default", appOnlyCtx(), user(nil), withDefault, "acme", ""},
		{"app-only scope and no default refuses", appOnlyCtx(), user(nil), Deps{}, "", dashcontract.CodePermissionDenied},
		{"absent claim takes the default", bare, user(nil), withDefault, "acme", ""},
		{"absent claim and no default refuses", bare, user(nil), Deps{}, "", dashcontract.CodePermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tenantFrom(tc.ctx, tc.p, tc.deps)
			if tc.code != "" {
				assert.Equal(t, tc.code, codeOf(t, err))
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAppFromMayBeEmptyButNeverWrong(t *testing.T) {
	got, err := appFrom(user(nil), Deps{})
	require.NoError(t, err)
	assert.Empty(t, got)
	got, err = appFrom(user(nil), Deps{DefaultAppID: "app_1"})
	require.NoError(t, err)
	assert.Equal(t, "app_1", got)
	got, err = appFrom(user(map[string]any{"app_id": "app_2"}), Deps{DefaultAppID: "app_1"})
	require.NoError(t, err)
	assert.Equal(t, "app_2", got)
	_, err = appFrom(user(map[string]any{"app_id": 7}), Deps{DefaultAppID: "app_1"})
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
}

func TestAppFromEdges(t *testing.T) {
	// A present-but-empty claim refuses even when a default exists to fall back on.
	for _, deps := range []Deps{{}, {DefaultAppID: "app_1"}} {
		got, err := appFrom(user(map[string]any{"app_id": ""}), deps)
		assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
		assert.Empty(t, got)
	}
	// A claim of the wrong type refuses with no default too.
	got, err := appFrom(user(map[string]any{"app_id": 7}), Deps{})
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
	assert.Empty(t, got)
	// Absent with no default is the empty app, not an error.
	got, err = appFrom(user(map[string]any{"other": "x"}), Deps{})
	require.NoError(t, err)
	assert.Equal(t, "", got)
}
