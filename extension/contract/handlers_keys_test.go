package contract

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

func principal() dashcontract.Principal {
	return dashcontract.Principal{User: &dashauth.UserInfo{Subject: "user_1"}}
}

func setup(t *testing.T, s store.Store) (Deps, *keysmith.Engine) {
	t.Helper()
	eng, err := keysmith.NewEngine(keysmith.WithStore(s))
	require.NoError(t, err)
	return Deps{Engine: eng, DefaultTenantID: "t1"}, eng
}

func tctx(tenant string) context.Context {
	return keysmith.WithTenant(context.Background(), "app", tenant)
}

func create(t *testing.T, eng *keysmith.Engine, tenant string, in *keysmith.CreateKeyInput) *key.CreateResult {
	t.Helper()
	if in == nil {
		in = &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive}
	}
	r, err := eng.CreateKey(tctx(tenant), in)
	require.NoError(t, err)
	return r
}

func TestKeysListIsTenantScopedByID(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		a := create(t, eng, "t1", nil)
		b := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "k2", Prefix: "sk", Environment: key.EnvTest})
		create(t, eng, "t2", nil)
		out, err := keysListHandler(deps)(context.Background(), keysListRequest{}, principal())
		require.NoError(t, err)
		ids := make([]string, 0, len(out.Keys))
		for _, k := range out.Keys {
			ids = append(ids, k.ID)
		}
		assert.ElementsMatch(t, []string{a.Key.ID.String(), b.Key.ID.String()}, ids)
		assert.EqualValues(t, 2, out.Total)
	})
}

func TestKeysListFiltersAndPages(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		for i := 0; i < 3; i++ {
			create(t, eng, "t1", nil)
		}
		test := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "t", Prefix: "sk", Environment: key.EnvTest})
		out, err := keysListHandler(deps)(context.Background(), keysListRequest{Environment: "test"}, principal())
		require.NoError(t, err)
		require.Len(t, out.Keys, 1)
		assert.Equal(t, test.Key.ID.String(), out.Keys[0].ID)
		assert.EqualValues(t, 1, out.Total)

		page, err := keysListHandler(deps)(context.Background(), keysListRequest{Limit: 2, Offset: 2}, principal())
		require.NoError(t, err)
		assert.Len(t, page.Keys, 2)
		assert.EqualValues(t, 4, page.Total, "total counts the filter, not the page")

		_, err = keysListHandler(deps)(context.Background(), keysListRequest{Environment: "prod"}, principal())
		assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err))
		_, err = keysListHandler(deps)(context.Background(), keysListRequest{State: "rotated"}, principal())
		assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err))
	})
}

func TestKeysListRefusesWithoutATenant(t *testing.T) {
	deps, _ := setup(t, memory.New())
	deps.DefaultTenantID = ""
	_, err := keysListHandler(deps)(context.Background(), keysListRequest{}, principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
	_, err = keysListHandler(Deps{Engine: deps.Engine, DefaultTenantID: "t1"})(context.Background(), keysListRequest{}, dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))
}

func TestKeyDetailHidesOtherTenantsKeys(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := create(t, eng, "t2", nil)
		_, errTheirs := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: theirs.Key.ID.String()}, principal())
		_, errMissing := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: id.NewKeyID().String()}, principal())
		assert.Equal(t, dashcontract.CodeNotFound, codeOf(t, errTheirs))
		assert.Equal(t, errMissing.Error(), errTheirs.Error(), "another tenant's key must read exactly like a missing one")
		_, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: "not-a-key"}, principal())
		assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err))
		_, err = keysDetailHandler(deps)(context.Background(), keysDetailRequest{}, principal())
		assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err))
	})
}

func TestKeyDetailShowsOpenWindowsOnly(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		orig := create(t, eng, "t1", nil)
		_, err := eng.RotateKey(tctx("t1"), orig.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
		require.NoError(t, err)
		// A pre-fix record: no hint, window in the future. Must not show.
		require.NoError(t, s.Rotations().Create(context.Background(), &rotation.Record{
			ID: id.NewRotationID(), KeyID: orig.Key.ID, TenantID: "t1", OldKeyHash: "legacy", NewKeyHash: "x",
			Reason: rotation.ReasonManual, GraceTTL: time.Hour, GraceEnds: time.Now().Add(time.Hour), CreatedAt: time.Now(),
		}))
		out, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: orig.Key.ID.String()}, principal())
		require.NoError(t, err)
		require.Len(t, out.PreviousKeys, 1)
		assert.Equal(t, orig.RawKey[len(orig.RawKey)-4:], out.PreviousKeys[0].Hint)
		assert.Nil(t, out.Policy)

		_, err = eng.EndGrace(tctx("t1"), orig.Key.ID)
		require.NoError(t, err)
		out, err = keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: orig.Key.ID.String()}, principal())
		require.NoError(t, err)
		assert.Empty(t, out.PreviousKeys)
	})
}

func TestKeyDetailCarriesScopesPolicyAndPendingExpiry(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		require.NoError(t, eng.CreateScope(tctx("t1"), &scope.Scope{Name: "read"}))
		pol := &policy.Policy{Name: "p", GracePeriod: 2 * time.Hour, MaxKeyLifetime: 720 * time.Hour}
		require.NoError(t, eng.CreatePolicy(tctx("t1"), pol))
		k := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID, Scopes: []string{"read"}})
		out, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: k.Key.ID.String()}, principal())
		require.NoError(t, err)
		assert.Equal(t, []string{"read"}, out.Key.Scopes)
		require.NotNil(t, out.Policy)
		require.NotNil(t, out.Policy.GraceSeconds)
		require.NotNil(t, out.Policy.MaxKeyLifetimeSeconds)
		assert.EqualValues(t, 7200, *out.Policy.GraceSeconds)
		assert.EqualValues(t, 720*3600, *out.Policy.MaxKeyLifetimeSeconds)

		past := time.Now().Add(-time.Minute)
		old := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "old", Prefix: "sk", Environment: key.EnvLive, ExpiresAt: &past})
		out, err = keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: old.Key.ID.String()}, principal())
		require.NoError(t, err)
		assert.Equal(t, "active", out.Key.State)
		assert.Equal(t, "expired", out.Key.EffectiveState)
		assert.True(t, out.Key.ExpiryPending)
	})
}

func TestNoQueryResponseCarriesASecret(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		r := create(t, eng, "t1", nil)
		list, err := keysListHandler(deps)(context.Background(), keysListRequest{}, principal())
		require.NoError(t, err)
		detail, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: r.Key.ID.String()}, principal())
		require.NoError(t, err)
		for _, v := range []any{list, detail} {
			b, err := json.Marshal(v)
			require.NoError(t, err)
			body := string(b)
			for _, banned := range []string{"rawKey", "raw_key", "keyHash", "key_hash", r.RawKey, r.Key.KeyHash} {
				assert.NotContains(t, body, banned)
			}
		}
	})
}
