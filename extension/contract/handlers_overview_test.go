package contract

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
	"github.com/xraph/keysmith/usage"
)

func ovGet(deps Deps) (overviewResponse, error) {
	return overviewHandler(deps)(context.Background(), overviewRequest{}, principal())
}

// ovKey writes a key straight to the store, so a test sets its state,
// expiry and CreatedAt exactly. Keys sit whole minutes apart in the tests,
// which every backend keeps exactly.
func ovKey(t *testing.T, s store.Store, tenant, name string, state key.State, at time.Time, expiresAt *time.Time) *key.Key {
	t.Helper()
	k := &key.Key{
		ID: id.NewKeyID(), TenantID: tenant, AppID: "app", Name: name,
		Prefix: "sk", Hint: "a3f8", KeyHash: "hash-" + id.NewKeyID().String(),
		Environment: key.EnvLive, State: state, ExpiresAt: expiresAt,
		CreatedAt: at, UpdatedAt: at,
	}
	if state == key.StateRevoked {
		k.RevokedAt = &at
	}
	require.NoError(t, s.Keys().Create(context.Background(), k))
	return k
}

func ovAt(d time.Duration) *time.Time {
	t := time.Now().Add(d)
	return &t
}

func ovKeyIDs(ks []KeySummary) []string {
	out := make([]string, 0, len(ks))
	for _, k := range ks {
		out = append(out, k.ID)
	}
	return out
}

// Counts are of the stored state, per tenant. An active key past its expiry
// that nothing has marked yet is still stored active, so it counts there.
func TestOverviewCountsKeysByStoredState(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, _ := setup(t, s)
		base := time.Now().Add(-time.Hour)
		ovKey(t, s, "t1", "a1", key.StateActive, base, nil)
		ovKey(t, s, "t1", "a2", key.StateActive, base.Add(time.Minute), ovAt(-time.Minute))
		ovKey(t, s, "t1", "a3", key.StateActive, base.Add(2*time.Minute), nil)
		ovKey(t, s, "t1", "s1", key.StateSuspended, base.Add(3*time.Minute), nil)
		ovKey(t, s, "t1", "r1", key.StateRevoked, base.Add(4*time.Minute), nil)
		ovKey(t, s, "t1", "r2", key.StateRevoked, base.Add(5*time.Minute), nil)
		ovKey(t, s, "t1", "e1", key.StateExpired, base.Add(6*time.Minute), ovAt(-time.Hour))
		for i, st := range []key.State{key.StateActive, key.StateSuspended, key.StateRevoked, key.StateExpired} {
			ovKey(t, s, "t2", "theirs", st, base.Add(time.Duration(10+i)*time.Minute), nil)
		}

		out, err := ovGet(deps)
		require.NoError(t, err)
		assert.Equal(t, overviewCounts{Active: 3, Suspended: 1, Revoked: 2, Expired: 1}, out.Counts)
	})
}

// A key counts as expiring when it is active, belongs to the tenant, and
// its expiry is ahead of now and no more than seven days out. A key already
// past its expiry is expired, not expiring, even before anything marks it.
func TestOverviewCountsKeysExpiringWithinSevenDays(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, _ := setup(t, s)
		base := time.Now().Add(-time.Hour)
		ovKey(t, s, "t1", "in 3 days", key.StateActive, base, ovAt(3*24*time.Hour))
		ovKey(t, s, "t1", "in 8 days", key.StateActive, base.Add(time.Minute), ovAt(8*24*time.Hour))
		ovKey(t, s, "t1", "an hour ago", key.StateActive, base.Add(2*time.Minute), ovAt(-time.Hour))
		ovKey(t, s, "t1", "never", key.StateActive, base.Add(3*time.Minute), nil)
		ovKey(t, s, "t1", "suspended", key.StateSuspended, base.Add(4*time.Minute), ovAt(2*24*time.Hour))
		ovKey(t, s, "t2", "theirs", key.StateActive, base.Add(5*time.Minute), ovAt(3*24*time.Hour))
		// Revoked, though a stale write put the stored state back to active.
		// RevokedAt wins, as it does everywhere else the contract reads a
		// key's state.
		raced := ovKey(t, s, "t1", "raced", key.StateRevoked, base.Add(6*time.Minute), ovAt(2*24*time.Hour))
		require.NoError(t, s.Keys().UpdateState(context.Background(), raced.ID, key.StateActive))

		out, err := ovGet(deps)
		require.NoError(t, err)
		assert.Equal(t, 1, out.ExpiringWithin7Days)

		// The same keys keys.list flags as expiring soon.
		list, err := keysListHandler(deps)(context.Background(), keysListRequest{}, principal())
		require.NoError(t, err)
		soon := 0
		for _, k := range list.Keys {
			if k.ExpiresSoon {
				soon++
			}
		}
		assert.Equal(t, soon, out.ExpiringWithin7Days)
	})
}

// A window counts when rotations.list would show it open: the record is
// this tenant's, it has an old hint, its grace ends ahead of now, and its
// key exists in this tenant. A suspended key's window still counts, since
// it resumes on reactivation.
func TestOverviewCountsOpenGraceWindowsAsTheRotationsPageShowsThem(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		now := time.Now()
		mine := create(t, eng, "t1", nil)
		paused := create(t, eng, "t1", nil)
		require.NoError(t, eng.SuspendKey(tctx("t1"), paused.Key.ID))
		theirs := create(t, eng, "t2", nil)

		rotSeed(t, s, "t1", mine.Key.ID, rotation.ReasonManual, now.Add(-time.Minute), time.Hour)
		rotSeed(t, s, "t1", paused.Key.ID, rotation.ReasonManual, now.Add(-2*time.Minute), time.Hour)
		// Closed: its grace ended an hour ago.
		rotSeed(t, s, "t1", mine.Key.ID, rotation.ReasonManual, now.Add(-3*time.Hour), 2*time.Hour)
		// Another tenant's open window.
		rotSeed(t, s, "t2", theirs.Key.ID, rotation.ReasonManual, now.Add(-4*time.Minute), time.Hour)
		// A t1 record naming a t2 key is not this tenant's window.
		rotSeed(t, s, "t1", theirs.Key.ID, rotation.ReasonManual, now.Add(-5*time.Minute), time.Hour)
		// Written before hints existed: the engine never honours it.
		at := now.Add(-6 * time.Minute)
		require.NoError(t, s.Rotations().Create(context.Background(), &rotation.Record{
			ID: id.NewRotationID(), KeyID: mine.Key.ID, TenantID: "t1",
			OldKeyHash: "old-hash", NewKeyHash: "new-hash",
			Reason: rotation.ReasonManual, GraceTTL: time.Hour, GraceEnds: at.Add(time.Hour), CreatedAt: at,
		}))

		out, err := ovGet(deps)
		require.NoError(t, err)
		assert.Equal(t, 2, out.OpenGraceWindows)

		rots, err := rotList(deps, rotationsListRequest{Limit: maxListLimit})
		require.NoError(t, err)
		open := 0
		for _, it := range rots.Items {
			if it.WindowOpen {
				open++
			}
		}
		assert.Equal(t, open, out.OpenGraceWindows, "the overview and the Rotations page agree")
	})
}

// A record whose key is gone from the store is not an open window.
func TestOverviewDoesNotCountAWindowWhoseKeyIsGone(t *testing.T) {
	s := memory.New()
	deps, _ := setup(t, s)
	rotSeed(t, s, "t1", id.NewKeyID(), rotation.ReasonManual, time.Now().Add(-time.Minute), time.Hour)
	out, err := ovGet(deps)
	require.NoError(t, err)
	assert.Equal(t, 0, out.OpenGraceWindows)
}

// null says the tenant has never recorded usage, so the page can say "not
// recorded" rather than show a quiet day as 0.
func TestOverviewRequestsLast24hIsNullUntilUsageIsRecorded(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		theirs := create(t, eng, "t2", nil)
		now := time.Now().Truncate(time.Minute)

		out, err := ovGet(deps)
		require.NoError(t, err)
		assert.Nil(t, out.RequestsLast24h, "no rows at all")

		usSeed(t, s, "t2", theirs.Key.ID, now.Add(-time.Minute), 200, 0)
		out, err = ovGet(deps)
		require.NoError(t, err)
		assert.Nil(t, out.RequestsLast24h, "another tenant's rows do not count")

		usSeed(t, s, "t1", k.Key.ID, now.Add(-48*time.Hour), 200, 0)
		out, err = ovGet(deps)
		require.NoError(t, err)
		require.NotNil(t, out.RequestsLast24h, "recorded, just not lately")
		assert.EqualValues(t, 0, *out.RequestsLast24h)
		b, err := json.Marshal(out)
		require.NoError(t, err)
		assert.Contains(t, string(b), `"requestsLast24h":0`)

		usSeed(t, s, "t1", k.Key.ID, now.Add(-2*time.Minute), 200, 0)
		usSeed(t, s, "t1", k.Key.ID, now.Add(-3*time.Minute), 404, 0)
		usSeed(t, s, "t1", k.Key.ID, now.Add(-23*time.Hour), 500, 0)
		usSeed(t, s, "t1", k.Key.ID, now.Add(-25*time.Hour), 200, 0)
		out, err = ovGet(deps)
		require.NoError(t, err)
		require.NotNil(t, out.RequestsLast24h)
		assert.EqualValues(t, 3, *out.RequestsLast24h, "every status, the last 24 hours only")
	})
}

// Whether anything was recorded is a one-row read, never a count of the
// tenant's whole history. The 24h count runs only once that read finds a
// row.
func TestOverviewAsksForOneUsageRowBeforeCounting(t *testing.T) {
	deps, spy := usSpied(t)
	_, err := ovGet(deps)
	require.NoError(t, err)
	require.Len(t, spy.queries, 1)
	assert.Equal(t, usage.QueryFilter{TenantID: "t1", Limit: 1}, spy.queries[0])
	assert.Empty(t, spy.counts, "nothing recorded, so nothing to count")

	require.NoError(t, deps.Engine.RecordUsage(context.Background(), &usage.Record{
		KeyID: id.NewKeyID(), TenantID: "t1", Endpoint: "/v1/things", Method: "GET", StatusCode: 200,
	}))
	before := time.Now()
	out, err := ovGet(deps)
	require.NoError(t, err)
	require.NotNil(t, out.RequestsLast24h)
	assert.EqualValues(t, 1, *out.RequestsLast24h)
	require.Len(t, spy.counts, 1)
	c := spy.counts[0]
	assert.Equal(t, "t1", c.TenantID)
	assert.Nil(t, c.KeyID)
	assert.Nil(t, c.Before)
	require.NotNil(t, c.After)
	assert.WithinDuration(t, before.Add(-24*time.Hour), *c.After, 5*time.Second)
}

func TestOverviewListsTheFiveNewestKeysAndRotations(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		base := time.Now().Add(-time.Hour)
		var keys []*key.Key
		for i := 0; i < 7; i++ {
			keys = append(keys, ovKey(t, s, "t1", "k", key.StateActive, base.Add(time.Duration(i)*time.Minute), nil))
		}
		// Newer than every t1 key, and never shown.
		ovKey(t, s, "t2", "theirs", key.StateActive, base.Add(30*time.Minute), nil)
		mkScope(t, eng, "t1", "read", "")
		mkScope(t, eng, "t1", "write", "")
		require.NoError(t, eng.AssignScopes(tctx("t1"), keys[6].ID, []string{"write", "read"}))

		newest := keys[6]
		var rots []*rotation.Record
		for i := 0; i < 7; i++ {
			rots = append(rots, rotSeed(t, s, "t1", newest.ID, rotation.ReasonManual, base.Add(time.Duration(i)*time.Minute), 2*time.Hour))
		}
		theirKey := ovKey(t, s, "t2", "theirs-rotated", key.StateActive, base, nil)
		rotSeed(t, s, "t2", theirKey.ID, rotation.ReasonManual, base.Add(30*time.Minute), time.Hour)

		out, err := ovGet(deps)
		require.NoError(t, err)

		require.Equal(t, []string{
			keys[6].ID.String(), keys[5].ID.String(), keys[4].ID.String(), keys[3].ID.String(), keys[2].ID.String(),
		}, ovKeyIDs(out.RecentKeys))
		// Projected the way keys.list projects them, scopes included.
		list, err := keysListHandler(deps)(context.Background(), keysListRequest{Limit: 5}, principal())
		require.NoError(t, err)
		assert.Equal(t, list.Keys, out.RecentKeys)
		assert.Equal(t, []string{"read", "write"}, out.RecentKeys[0].Scopes)
		assert.Equal(t, []string{}, out.RecentKeys[1].Scopes)

		require.Equal(t, []string{
			rots[6].ID.String(), rots[5].ID.String(), rots[4].ID.String(), rots[3].ID.String(), rots[2].ID.String(),
		}, rotIDs(out.RecentRotations))
		// Projected the way rotations.list projects them.
		page, err := rotList(deps, rotationsListRequest{Limit: 5})
		require.NoError(t, err)
		assert.Equal(t, page.Items, out.RecentRotations)
		first := out.RecentRotations[0]
		require.NotNil(t, first.KeyName)
		assert.Equal(t, "k", *first.KeyName)
		assert.True(t, first.WindowOpen)
	})
}

// An empty tenant answers lists as [], never null, and the 24h count as an
// explicit null.
func TestOverviewOfAnEmptyTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		create(t, eng, "t2", nil)
		out, err := ovGet(deps)
		require.NoError(t, err)
		b, err := json.Marshal(out)
		require.NoError(t, err)
		body := string(b)
		assert.Contains(t, body, `"counts":{"active":0,"suspended":0,"revoked":0,"expired":0}`)
		assert.Contains(t, body, `"openGraceWindows":0`)
		assert.Contains(t, body, `"expiringWithin7Days":0`)
		assert.Contains(t, body, `"requestsLast24h":null`)
		assert.Contains(t, body, `"recentKeys":[]`)
		assert.Contains(t, body, `"recentRotations":[]`)
		assert.Contains(t, body, `"policyFields":13`)
	})
}

func TestOverviewSaysHowManyPolicyFieldsAreEnforced(t *testing.T) {
	s := memory.New()
	deps, _ := setup(t, s)
	out, err := ovGet(deps)
	require.NoError(t, err)
	assert.Equal(t, 3, out.EnforcedFields)
	assert.Equal(t, 13, out.PolicyFields)

	limited, err := keysmith.NewEngine(keysmith.WithStore(s), keysmith.WithRateLimiter(allowAll{}))
	require.NoError(t, err)
	deps.Engine = limited
	out, err = ovGet(deps)
	require.NoError(t, err)
	assert.Equal(t, 5, out.EnforcedFields)
	assert.Equal(t, 13, out.PolicyFields)
}

func TestOverviewRefusesWithoutAUserOrTenant(t *testing.T) {
	deps, _ := setup(t, memory.New())
	_, err := overviewHandler(deps)(context.Background(), overviewRequest{}, dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))
	deps.DefaultTenantID = ""
	_, err = ovGet(deps)
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
}

// A store that fails answers INTERNAL without its text, and the operator's
// log names the overview.
func TestOverviewAnswersInternalWhenAReadFails(t *testing.T) {
	base := memory.New()
	counting := &rotCountingKeys{Store: base.Keys(), gets: map[string]int{}}
	_, eng := setup(t, rotKeysStore{Store: base, keys: counting})
	var lines []rotateLogLine
	deps := Deps{
		Engine: eng, DefaultTenantID: "t1",
		Logger: rotateCapturingLogger{Logger: forge.NewNoopLogger(), lines: &lines},
	}
	k := create(t, eng, "t1", nil)
	rotSeed(t, base, "t1", k.Key.ID, rotation.ReasonManual, time.Now().Add(-time.Minute), time.Hour)
	counting.fail = true
	_, err := ovGet(deps)
	assert.Equal(t, dashcontract.CodeInternal, codeOf(t, err))
	assert.NotContains(t, err.Error(), "secret-connection-detail")

	usDeps, spy := usSpied(t)
	usDeps.Logger = deps.Logger
	spy.failQuery = true
	_, err = ovGet(usDeps)
	assert.Equal(t, dashcontract.CodeInternal, codeOf(t, err))
	assert.NotContains(t, err.Error(), "secret-connection-detail")

	require.NotEmpty(t, lines)
	for _, l := range lines {
		assert.Equal(t, "overview", l.fields["intent"])
	}
}

func TestOverviewIsDispatchedAsAReadQuery(t *testing.T) {
	deps, eng := setup(t, memory.New())
	create(t, eng, "t1", nil)
	d := createTestDispatcher(t, deps)
	data, _, err := d.Dispatch(context.Background(), dashcontract.Request{
		Envelope: "v1", Kind: dashcontract.KindQuery, Contributor: ContributorName,
		Intent: "overview", IntentVersion: 1, Params: map[string]any{},
	}, principal())
	require.NoError(t, err)
	var out overviewResponse
	require.NoError(t, json.Unmarshal(data, &out))
	assert.EqualValues(t, 1, out.Counts.Active)
	require.Len(t, out.RecentKeys, 1)
}
