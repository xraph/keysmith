package contract

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestTenantFrom(t *testing.T) {
	withDefault := Deps{DefaultTenantID: "acme"}
	cases := []struct {
		name string
		p    dashcontract.Principal
		deps Deps
		want string
		code dashcontract.ErrorCode
	}{
		{"no user refuses even with a default", dashcontract.Principal{}, withDefault, "", dashcontract.CodeUnauthenticated},
		{"blank subject refuses", dashcontract.Principal{User: &dashauth.UserInfo{Subject: "  "}}, withDefault, "", dashcontract.CodeUnauthenticated},
		{"claim wins over default", user(map[string]any{"tenant_id": "globex"}), withDefault, "globex", ""},
		{"empty claim refuses, never defaults", user(map[string]any{"tenant_id": ""}), withDefault, "", dashcontract.CodePermissionDenied},
		{"wrong-typed claim refuses", user(map[string]any{"tenant_id": 42}), withDefault, "", dashcontract.CodePermissionDenied},
		{"nil claim refuses", user(map[string]any{"tenant_id": nil}), withDefault, "", dashcontract.CodePermissionDenied},
		{"absent claim takes the default", user(nil), withDefault, "acme", ""},
		{"absent claim and no default refuses", user(nil), Deps{}, "", dashcontract.CodePermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tenantFrom(tc.p, tc.deps)
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
