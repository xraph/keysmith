package contract

import (
	"context"
	"encoding/json"
	"strings"
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
	"github.com/xraph/keysmith/usage"
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

// A finished key lists no previous keys, even with an open grace record:
// ValidateKey refuses an expired or revoked key whichever hash is
// presented, and a suspended key past its expiry is refused on expiry once
// reactivated. A suspended key within its expiry still lists them, since
// its windows resume on reactivation. This is rotations.list's rule.
func TestKeyDetailListsNoPreviousKeysForAFinishedKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, _ := setup(t, s)
		now := time.Now()
		base := now.Add(-time.Hour).Truncate(time.Minute)
		expired := ovKey(t, s, "t1", "expired", key.StateExpired, base, ovAt(-time.Minute))
		lapsed := ovKey(t, s, "t1", "lapsed", key.StateActive, base.Add(time.Minute), ovAt(-time.Minute))
		raced := ovKey(t, s, "t1", "raced", key.StateActive, base.Add(2*time.Minute), nil)
		raced.RevokedAt = &base
		require.NoError(t, s.Keys().Update(context.Background(), raced))
		stale := ovKey(t, s, "t1", "stale", key.StateSuspended, base.Add(3*time.Minute), ovAt(-time.Minute))
		paused := ovKey(t, s, "t1", "paused", key.StateSuspended, base.Add(4*time.Minute), ovAt(time.Hour))

		for i, k := range []*key.Key{expired, lapsed, raced, stale, paused} {
			rotSeed(t, s, "t1", k.ID, rotation.ReasonManual, now.Add(-time.Duration(i+1)*time.Minute), time.Hour)
		}

		for _, k := range []*key.Key{expired, lapsed, raced, stale} {
			out, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: k.ID.String()}, principal())
			require.NoError(t, err, k.Name)
			assert.NotNil(t, out.PreviousKeys, k.Name)
			assert.Empty(t, out.PreviousKeys, k.Name)
			b, err := json.Marshal(out)
			require.NoError(t, err)
			assert.Contains(t, string(b), `"previousKeys":[]`, k.Name)
		}

		out, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: paused.ID.String()}, principal())
		require.NoError(t, err)
		require.Len(t, out.PreviousKeys, 1)
		assert.Equal(t, "a3f8", out.PreviousKeys[0].Hint)
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
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "p"})
		polID := pol.ID
		r := create(t, eng, "t1", &keysmith.CreateKeyInput{
			Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &polID,
		})
		list, err := keysListHandler(deps)(context.Background(), keysListRequest{}, principal())
		require.NoError(t, err)
		detail, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: r.Key.ID.String()}, principal())
		require.NoError(t, err)
		policies, err := policiesListHandler(deps)(context.Background(), pickerRequest{}, principal())
		require.NoError(t, err)
		polDetail, err := policiesDetailHandler(deps)(context.Background(), policyIDRequest{ID: pol.ID.String()}, principal())
		require.NoError(t, err)
		// A rotation, so rotations.list has a row whose old hash is the
		// first key's hash.
		rotated, err := eng.RotateKey(tctx("t1"), r.Key.ID, rotation.ReasonManual)
		require.NoError(t, err)
		rotations, err := rotationsListHandler(deps)(context.Background(), rotationsListRequest{}, principal())
		require.NoError(t, err)
		require.Len(t, rotations.Items, 1)
		// A usage row for each key, so usage.series and usage.records have
		// something to answer.
		for _, kid := range []id.KeyID{r.Key.ID, rotated.Key.ID} {
			require.NoError(t, eng.RecordUsage(context.Background(), &usage.Record{
				KeyID: kid, TenantID: "t1", Endpoint: "/v1/things", Method: "GET", StatusCode: 200,
			}))
		}
		now := time.Now()
		series, err := usageSeriesHandler(deps)(context.Background(), usageSeriesRequest{
			Period: "hourly", After: rfc3339(now.Add(-time.Hour)), Before: rfc3339(now.Add(time.Hour)),
		}, principal())
		require.NoError(t, err)
		require.True(t, series.Recorded)
		records, err := usageRecordsHandler(deps)(context.Background(), usageRecordsRequest{}, principal())
		require.NoError(t, err)
		require.Len(t, records.Items, 2)
		overview, err := overviewHandler(deps)(context.Background(), overviewRequest{}, principal())
		require.NoError(t, err)
		require.Len(t, overview.RecentKeys, 1)
		require.Len(t, overview.RecentRotations, 1)
		settings, err := settingsHandler(deps)(context.Background(), settingsRequest{}, principal())
		require.NoError(t, err)
		for _, v := range []any{list, detail, policies, polDetail, rotations, series, records, overview, settings} {
			b, err := json.Marshal(v)
			require.NoError(t, err)
			body := string(b)
			for _, banned := range []string{"rawKey", "raw_key", "keyHash", "key_hash", "KeyHash"} {
				assert.NotContains(t, body, banned)
			}
			// Hashes are checked without printing them.
			for i, hash := range []string{r.Key.KeyHash, rotated.Key.KeyHash} {
				assert.False(t, strings.Contains(body, hash), "response of %d bytes carries key hash %d", len(body), i)
			}
			// Never print the raw key, even in a failure message.
			assert.False(t, strings.Contains(body, r.RawKey), "response of %d bytes carries the raw key", len(body))
			assert.False(t, strings.Contains(body, rotated.RawKey), "response of %d bytes carries the rotated raw key", len(body))
		}
	})
}

func TestKeyDetailHidesAnotherTenantsPolicyAndADanglingOne(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := &policy.Policy{Name: "theirs", GracePeriod: time.Hour}
		require.NoError(t, eng.CreatePolicy(tctx("t2"), theirs))

		for name, pid := range map[string]id.PolicyID{
			"another tenant's policy":      theirs.ID,
			"a policy that does not exist": id.NewPolicyID(),
		} {
			r := create(t, eng, "t1", nil)
			// Written straight to the store. CreateKey refuses a policy that does not
			// exist but not another tenant's, so the contract is the only guard
			// against showing a foreign policy.
			k, err := s.Keys().Get(context.Background(), r.Key.ID)
			require.NoError(t, err)
			p := pid
			k.PolicyID = &p
			require.NoError(t, s.Keys().Update(context.Background(), k))

			out, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: r.Key.ID.String()}, principal())
			require.NoError(t, err, name)
			assert.Nil(t, out.Policy, name)
			assert.Equal(t, pid.String(), out.Key.PolicyID, "%s: the key still shows the id it points at", name)
		}
	})
}

func TestKeysListFiltersByPolicy(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		pol := &policy.Policy{Name: "p"}
		require.NoError(t, eng.CreatePolicy(tctx("t1"), pol))
		with := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "with", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID})
		create(t, eng, "t1", nil)

		out, err := keysListHandler(deps)(context.Background(), keysListRequest{PolicyID: pol.ID.String()}, principal())
		require.NoError(t, err)
		require.Len(t, out.Keys, 1)
		assert.Equal(t, with.Key.ID.String(), out.Keys[0].ID)
		assert.EqualValues(t, 1, out.Total)

		_, err = keysListHandler(deps)(context.Background(), keysListRequest{PolicyID: "not-a-policy"}, principal())
		assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err))
	})
}

// The memory store's ListByKey matches scopes by name across every tenant and
// in map order, so the same name in two tenants comes back twice. The contract
// sorts and de-duplicates whatever the store returns.
func TestKeyScopesAreSortedAndUniqueOnMemory(t *testing.T) {
	deps, eng := setup(t, memory.New())
	for _, name := range []string{"write", "read"} {
		require.NoError(t, eng.CreateScope(tctx("t1"), &scope.Scope{Name: name}))
	}
	require.NoError(t, eng.CreateScope(tctx("t2"), &scope.Scope{Name: "read"}))
	k := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, Scopes: []string{"write", "read"}})

	for i := 0; i < 20; i++ {
		out, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: k.Key.ID.String()}, principal())
		require.NoError(t, err)
		require.Equal(t, []string{"read", "write"}, out.Key.Scopes)
		list, err := keysListHandler(deps)(context.Background(), keysListRequest{}, principal())
		require.NoError(t, err)
		require.Len(t, list.Keys, 1)
		require.Equal(t, []string{"read", "write"}, list.Keys[0].Scopes)
	}
}
