package contract

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

func mkPolicy(t *testing.T, eng *keysmith.Engine, tenant string, p *policy.Policy) *policy.Policy {
	t.Helper()
	require.NoError(t, eng.CreatePolicy(tctx(tenant), p))
	return p
}

func mkScope(t *testing.T, eng *keysmith.Engine, tenant, name, parent string) *scope.Scope {
	t.Helper()
	sc := &scope.Scope{Name: name, Parent: parent}
	require.NoError(t, eng.CreateScope(tctx(tenant), sc))
	return sc
}

func TestPoliciesListIsTenantScopedByID(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		a := mkPolicy(t, eng, "t1", &policy.Policy{Name: "a"})
		b := mkPolicy(t, eng, "t1", &policy.Policy{Name: "b"})
		mkPolicy(t, eng, "t2", &policy.Policy{Name: "theirs"})
		out, err := policiesListHandler(deps)(context.Background(), pickerRequest{}, principal())
		require.NoError(t, err)
		ids := make([]string, 0, len(out.Policies))
		for _, p := range out.Policies {
			ids = append(ids, p.ID)
		}
		assert.ElementsMatch(t, []string{a.ID.String(), b.ID.String()}, ids)
		assert.False(t, out.HasMore)
	})
}

func TestScopesListIsTenantScopedByID(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		a := mkScope(t, eng, "t1", "read", "")
		b := mkScope(t, eng, "t1", "read:users", "read")
		mkScope(t, eng, "t2", "theirs", "")
		out, err := scopesListHandler(deps)(context.Background(), pickerRequest{}, principal())
		require.NoError(t, err)
		ids := make([]string, 0, len(out.Scopes))
		for _, sc := range out.Scopes {
			ids = append(ids, sc.ID)
		}
		assert.ElementsMatch(t, []string{a.ID.String(), b.ID.String()}, ids)
		assert.False(t, out.HasMore)
		for _, sc := range out.Scopes {
			if sc.ID == b.ID.String() {
				assert.Equal(t, "read", sc.Parent)
			}
		}
	})
}

func TestPickersReportHasMore(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		for _, n := range []string{"a", "b", "c"} {
			mkPolicy(t, eng, "t1", &policy.Policy{Name: n})
			mkScope(t, eng, "t1", n, "")
		}
		pols, err := policiesListHandler(deps)(context.Background(), pickerRequest{Limit: 2}, principal())
		require.NoError(t, err)
		assert.Len(t, pols.Policies, 2)
		assert.True(t, pols.HasMore)

		scs, err := scopesListHandler(deps)(context.Background(), pickerRequest{Limit: 2}, principal())
		require.NoError(t, err)
		assert.Len(t, scs.Scopes, 2)
		assert.True(t, scs.HasMore)

		pols, err = policiesListHandler(deps)(context.Background(), pickerRequest{Limit: 3}, principal())
		require.NoError(t, err)
		assert.Len(t, pols.Policies, 3)
		assert.False(t, pols.HasMore, "a page that ends exactly at the last row has no more")

		scs, err = scopesListHandler(deps)(context.Background(), pickerRequest{Limit: 2, Offset: 2}, principal())
		require.NoError(t, err)
		assert.Len(t, scs.Scopes, 1)
		assert.False(t, scs.HasMore)
	})
}

func TestPickerLimitDefaultsAndCaps(t *testing.T) {
	lim, off := clampPickerPage(0, -3)
	assert.Equal(t, 100, lim)
	assert.Equal(t, 0, off)
	lim, _ = clampPickerPage(5000, 0)
	assert.Equal(t, 200, lim)
	lim, off = clampPickerPage(7, 4)
	assert.Equal(t, 7, lim)
	assert.Equal(t, 4, off)
}

func TestPoliciesListProjectsUnsetDurationsAsNull(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkPolicy(t, eng, "t1", &policy.Policy{Name: "bare"})
		mkPolicy(t, eng, "t1", &policy.Policy{
			Name:           "set",
			Description:    "has limits",
			MaxKeyLifetime: 90 * 24 * time.Hour,
			GracePeriod:    time.Hour,
			AllowedScopes:  []string{"read", "write"},
		})
		out, err := policiesListHandler(deps)(context.Background(), pickerRequest{}, principal())
		require.NoError(t, err)
		require.Len(t, out.Policies, 2)

		bare, set := out.Policies[0], out.Policies[1]
		require.Equal(t, "bare", bare.Name)
		assert.Nil(t, bare.MaxKeyLifetimeSeconds)
		assert.Nil(t, bare.GraceSeconds)
		assert.NotNil(t, bare.AllowedScopes, "allowedScopes is never nil")
		assert.Empty(t, bare.AllowedScopes)

		require.Equal(t, "set", set.Name)
		require.NotNil(t, set.MaxKeyLifetimeSeconds)
		require.NotNil(t, set.GraceSeconds)
		assert.EqualValues(t, 90*24*3600, *set.MaxKeyLifetimeSeconds)
		assert.EqualValues(t, 3600, *set.GraceSeconds)
		assert.Equal(t, "has limits", set.Description)
	})
}

func TestPickersSortByName(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		for _, n := range []string{"charlie", "alpha", "bravo"} {
			mkPolicy(t, eng, "t1", &policy.Policy{Name: n})
			mkScope(t, eng, "t1", n, "")
		}
		pols, err := policiesListHandler(deps)(context.Background(), pickerRequest{}, principal())
		require.NoError(t, err)
		pn := make([]string, 0, len(pols.Policies))
		for _, p := range pols.Policies {
			pn = append(pn, p.Name)
		}
		assert.Equal(t, []string{"alpha", "bravo", "charlie"}, pn)

		scs, err := scopesListHandler(deps)(context.Background(), pickerRequest{}, principal())
		require.NoError(t, err)
		sn := make([]string, 0, len(scs.Scopes))
		for _, sc := range scs.Scopes {
			sn = append(sn, sc.Name)
		}
		assert.Equal(t, []string{"alpha", "bravo", "charlie"}, sn)
	})
}

func TestPickersRefuseWithoutATenant(t *testing.T) {
	deps, _ := setup(t, memory.New())
	deps.DefaultTenantID = ""
	_, err := policiesListHandler(deps)(context.Background(), pickerRequest{}, principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
	_, err = scopesListHandler(deps)(context.Background(), pickerRequest{}, principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
}
