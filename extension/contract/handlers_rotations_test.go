package contract

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

func rotList(deps Deps, in rotationsListRequest) (rotationsListResponse, error) {
	return rotationsListHandler(deps)(context.Background(), in, principal())
}

// rotSeed writes a rotation record straight to the store. at is its
// CreatedAt; minutes apart keeps the order exact on every backend (mongo
// keeps milliseconds).
func rotSeed(t *testing.T, s store.Store, tenant string, keyID id.KeyID, reason rotation.Reason, at time.Time, grace time.Duration) *rotation.Record {
	t.Helper()
	rec := &rotation.Record{
		ID: id.NewRotationID(), KeyID: keyID, TenantID: tenant,
		OldKeyHash: "old-hash", NewKeyHash: "new-hash", OldHint: "a3f8", NewHint: "b7c2",
		Reason: reason, GraceTTL: grace, GraceEnds: at.Add(grace), RotatedBy: "user_1", CreatedAt: at,
	}
	require.NoError(t, s.Rotations().Create(context.Background(), rec))
	return rec
}

func rotIDs(items []RotationItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestRotationsListIsTenantScopedByIDAndNewestFirst(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mine := create(t, eng, "t1", nil)
		theirs := create(t, eng, "t2", nil)
		now := time.Now()
		older := rotSeed(t, s, "t1", mine.Key.ID, rotation.ReasonManual, now.Add(-2*time.Hour), time.Hour)
		newer := rotSeed(t, s, "t1", mine.Key.ID, rotation.ReasonPolicy, now.Add(-time.Hour), time.Hour)
		rotSeed(t, s, "t2", theirs.Key.ID, rotation.ReasonManual, now.Add(-30*time.Minute), time.Hour)

		out, err := rotList(deps, rotationsListRequest{})
		require.NoError(t, err)
		assert.Equal(t, []string{newer.ID.String(), older.ID.String()}, rotIDs(out.Items))
		assert.False(t, out.HasMore)

		require.NotNil(t, out.Items[0].KeyName)
		require.NotNil(t, out.Items[0].Prefix)
		require.NotNil(t, out.Items[0].Environment)
		assert.Equal(t, "k", *out.Items[0].KeyName)
		assert.Equal(t, "sk", *out.Items[0].Prefix)
		assert.Equal(t, "live", *out.Items[0].Environment)
		assert.Equal(t, mine.Key.ID.String(), out.Items[0].KeyID)
		assert.Equal(t, "policy", out.Items[0].Reason)
		assert.Equal(t, "a3f8", out.Items[0].OldHint)
		assert.Equal(t, "b7c2", out.Items[0].NewHint)
		assert.Equal(t, "user_1", out.Items[0].RotatedBy)
		assert.Equal(t, rfc3339(newer.CreatedAt), out.Items[0].RotatedAt)
		assert.Equal(t, rfc3339(newer.GraceEnds), out.Items[0].GraceEnds)
	})
}

func TestRotationsListFiltersByKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		a := create(t, eng, "t1", nil)
		b := create(t, eng, "t1", nil)
		theirs := create(t, eng, "t2", nil)
		now := time.Now()
		ra := rotSeed(t, s, "t1", a.Key.ID, rotation.ReasonManual, now.Add(-time.Hour), time.Hour)
		rotSeed(t, s, "t1", b.Key.ID, rotation.ReasonManual, now.Add(-2*time.Hour), time.Hour)
		rotSeed(t, s, "t2", theirs.Key.ID, rotation.ReasonManual, now.Add(-3*time.Hour), time.Hour)

		out, err := rotList(deps, rotationsListRequest{KeyID: "  " + a.Key.ID.String() + " "})
		require.NoError(t, err)
		assert.Equal(t, []string{ra.ID.String()}, rotIDs(out.Items))

		// A filter, not a lookup: another tenant's key and a missing one
		// both answer an empty list, never NOT_FOUND.
		for _, kid := range []string{theirs.Key.ID.String(), id.NewKeyID().String()} {
			out, err = rotList(deps, rotationsListRequest{KeyID: kid})
			require.NoError(t, err)
			assert.NotNil(t, out.Items)
			assert.Empty(t, out.Items)
			assert.False(t, out.HasMore)
		}

		_, err = rotList(deps, rotationsListRequest{KeyID: "not-a-key"})
		assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err))
		assert.Equal(t, "keyId is not a key id", badRequestMessage(t, err))
	})
}

func TestRotationsListFiltersByReason(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		reasons := []rotation.Reason{rotation.ReasonManual, rotation.ReasonCompromise, rotation.ReasonPolicy, rotation.ReasonScheduled}
		byReason := map[string]string{}
		for i, r := range reasons {
			rec := rotSeed(t, s, "t1", k.Key.ID, r, now.Add(-time.Duration(i+1)*time.Minute), time.Hour)
			byReason[string(r)] = rec.ID.String()
		}
		for reason, want := range byReason {
			out, err := rotList(deps, rotationsListRequest{Reason: reason})
			require.NoError(t, err)
			assert.Equal(t, []string{want}, rotIDs(out.Items), reason)
			assert.Equal(t, reason, out.Items[0].Reason)
		}

		for _, bad := range []string{"rotated", "Manual", "expired"} {
			_, err := rotList(deps, rotationsListRequest{Reason: bad})
			assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err), bad)
			assert.Equal(t, "reason must be one of manual, compromise, policy, scheduled", badRequestMessage(t, err))
		}
	})
}

func TestRotationsListReportsHasMore(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		var ids []string
		for i := 0; i < 3; i++ {
			ids = append(ids, rotSeed(t, s, "t1", k.Key.ID, rotation.ReasonManual, now.Add(-time.Duration(i+1)*time.Minute), time.Hour).ID.String())
		}
		out, err := rotList(deps, rotationsListRequest{Limit: 2})
		require.NoError(t, err)
		assert.Equal(t, ids[:2], rotIDs(out.Items))
		assert.True(t, out.HasMore)

		out, err = rotList(deps, rotationsListRequest{Limit: 2, Offset: 2})
		require.NoError(t, err)
		assert.Equal(t, ids[2:], rotIDs(out.Items))
		assert.False(t, out.HasMore)

		out, err = rotList(deps, rotationsListRequest{Limit: 3})
		require.NoError(t, err)
		assert.Len(t, out.Items, 3)
		assert.False(t, out.HasMore, "exactly a full page has no more")
	})
}

// rotSpy records the filter the handler hands the store.
type rotSpy struct {
	rotation.Store
	last *rotation.ListFilter
}

func (w *rotSpy) List(ctx context.Context, f *rotation.ListFilter) ([]*rotation.Record, error) {
	cp := *f
	w.last = &cp
	return w.Store.List(ctx, f)
}

func TestRotationsListLimitDefaultsAndCaps(t *testing.T) {
	base := memory.New()
	spy := &rotSpy{Store: base.Rotations()}
	deps, _ := setup(t, rotateStore{Store: base, rots: spy})

	_, err := rotList(deps, rotationsListRequest{Offset: -4})
	require.NoError(t, err)
	assert.Equal(t, 26, spy.last.Limit, "default 25, plus one to see the next page")
	assert.Equal(t, 0, spy.last.Offset)
	assert.Equal(t, "t1", spy.last.TenantID)
	assert.Nil(t, spy.last.KeyID)
	assert.Empty(t, spy.last.Reason)

	_, err = rotList(deps, rotationsListRequest{Limit: 5000, Offset: 7, Reason: "compromise"})
	require.NoError(t, err)
	assert.Equal(t, 101, spy.last.Limit, "capped at 100, plus one")
	assert.Equal(t, 7, spy.last.Offset)
	assert.Equal(t, rotation.ReasonCompromise, spy.last.Reason)
}

func TestRotationsListDefaultPageIsTwentyFive(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		for i := 0; i < 26; i++ {
			rotSeed(t, s, "t1", k.Key.ID, rotation.ReasonManual, now.Add(-time.Duration(i+1)*time.Minute), time.Hour)
		}
		out, err := rotList(deps, rotationsListRequest{})
		require.NoError(t, err)
		assert.Len(t, out.Items, 25)
		assert.True(t, out.HasMore)
	})
}

func TestRotationsListProjectsHintlessRecords(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		// Written before hints existed. Its window is in the future, but the
		// engine never honours a hintless record, so it must not read open.
		legacy := &rotation.Record{
			ID: id.NewRotationID(), KeyID: k.Key.ID, TenantID: "t1",
			OldKeyHash: "legacy-old", NewKeyHash: "legacy-new",
			Reason: rotation.ReasonCompromise, GraceTTL: 24 * time.Hour,
			GraceEnds: now.Add(time.Hour), CreatedAt: now.Add(-time.Minute),
		}
		require.NoError(t, s.Rotations().Create(context.Background(), legacy))

		out, err := rotList(deps, rotationsListRequest{})
		require.NoError(t, err)
		require.Equal(t, []string{legacy.ID.String()}, rotIDs(out.Items))
		l := out.Items[0]
		assert.Equal(t, "", l.OldHint)
		assert.Equal(t, "", l.NewHint)
		assert.False(t, l.WindowOpen, "a hintless record never opens a window")
		assert.EqualValues(t, 86400, l.GraceSeconds)
		assert.Empty(t, l.RotatedBy)
		require.NotNil(t, l.KeyName)
		assert.Equal(t, "k", *l.KeyName)

		b, err := json.Marshal(l)
		require.NoError(t, err)
		assert.Contains(t, string(b), `"oldHint":""`)
		assert.Contains(t, string(b), `"newHint":""`)
		assert.NotContains(t, string(b), "rotatedBy")
	})
}

func TestRotationsListProjectsAKeyThatIsGoneOrForeignAsNull(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		doomed := create(t, eng, "t1", nil)
		theirs := create(t, eng, "t2", &keysmith.CreateKeyInput{Name: "their-secret-name", Prefix: "sk", Environment: key.EnvLive})
		now := time.Now()
		gone := rotSeed(t, s, "t1", doomed.Key.ID, rotation.ReasonManual, now.Add(-2*time.Minute), time.Hour)
		// A t1 record naming a t2 key never shows that key.
		foreign := rotSeed(t, s, "t1", theirs.Key.ID, rotation.ReasonManual, now.Add(-3*time.Minute), time.Hour)

		// Deleted from the store. postgres and sqlite cascade the delete to
		// the key's rotations; memory and mongo keep them.
		require.NoError(t, s.Keys().Delete(context.Background(), doomed.Key.ID))
		_, getErr := s.Rotations().Get(context.Background(), gone.ID)
		kept := getErr == nil

		out, err := rotList(deps, rotationsListRequest{})
		require.NoError(t, err)
		want := []string{foreign.ID.String()}
		if kept {
			want = []string{gone.ID.String(), foreign.ID.String()}
		}
		require.Equal(t, want, rotIDs(out.Items))

		for _, it := range out.Items {
			assert.Nil(t, it.KeyName, it.ID)
			assert.Nil(t, it.Prefix, it.ID)
			assert.Nil(t, it.Environment, it.ID)
			assert.True(t, it.WindowOpen, it.ID)
			assert.Equal(t, "a3f8", it.OldHint, it.ID)

			b, err := json.Marshal(it)
			require.NoError(t, err)
			body := string(b)
			assert.Contains(t, body, `"keyName":null`)
			assert.Contains(t, body, `"prefix":null`)
			assert.Contains(t, body, `"environment":null`)
			assert.NotContains(t, body, "their-secret-name")
		}
		assert.Equal(t, theirs.Key.ID.String(), out.Items[len(out.Items)-1].KeyID)
		if kept {
			assert.Equal(t, doomed.Key.ID.String(), out.Items[0].KeyID)
		}
	})
}

// A record whose key never existed, as an import or a store without foreign
// keys can leave behind.
func TestRotationsListProjectsAnUnknownKeyAsNull(t *testing.T) {
	s := memory.New()
	deps, _ := setup(t, s)
	rec := rotSeed(t, s, "t1", id.NewKeyID(), rotation.ReasonPolicy, time.Now().Add(-time.Minute), time.Hour)
	out, err := rotList(deps, rotationsListRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{rec.ID.String()}, rotIDs(out.Items))
	assert.Nil(t, out.Items[0].KeyName)
	assert.Nil(t, out.Items[0].Prefix)
	assert.Nil(t, out.Items[0].Environment)
	assert.Equal(t, rec.KeyID.String(), out.Items[0].KeyID)
}

func TestRotationsListWindowsAndGrace(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		open := rotSeed(t, s, "t1", k.Key.ID, rotation.ReasonManual, now.Add(-time.Minute), 90*time.Minute)
		closed := rotSeed(t, s, "t1", k.Key.ID, rotation.ReasonManual, now.Add(-3*time.Hour), time.Hour)
		zero := rotSeed(t, s, "t1", k.Key.ID, rotation.ReasonCompromise, now.Add(-4*time.Hour), 0)

		out, err := rotList(deps, rotationsListRequest{})
		require.NoError(t, err)
		require.Equal(t, []string{open.ID.String(), closed.ID.String(), zero.ID.String()}, rotIDs(out.Items))

		assert.True(t, out.Items[0].WindowOpen)
		assert.EqualValues(t, 5400, out.Items[0].GraceSeconds)
		assert.False(t, out.Items[1].WindowOpen)
		assert.EqualValues(t, 3600, out.Items[1].GraceSeconds)
		assert.False(t, out.Items[2].WindowOpen)
		assert.EqualValues(t, 0, out.Items[2].GraceSeconds)
		assert.Equal(t, out.Items[2].RotatedAt, out.Items[2].GraceEnds)

		b, err := json.Marshal(out.Items[2])
		require.NoError(t, err)
		assert.Contains(t, string(b), `"graceSeconds":0`, "zero grace is a real value, not unset")
	})
}

func TestRotationsListFromARealRotation(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		k := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "billing", Prefix: "pk", Environment: key.EnvTest})
		res, err := eng.RotateKey(tctx("t1"), k.Key.ID, rotation.ReasonScheduled, keysmith.WithGrace(2*time.Hour))
		require.NoError(t, err)

		out, err := rotList(deps, rotationsListRequest{KeyID: k.Key.ID.String()})
		require.NoError(t, err)
		require.Len(t, out.Items, 1)
		it := out.Items[0]
		assert.Equal(t, k.RawKey[len(k.RawKey)-4:], it.OldHint)
		assert.Equal(t, res.RawKey[len(res.RawKey)-4:], it.NewHint)
		assert.EqualValues(t, 7200, it.GraceSeconds)
		assert.True(t, it.WindowOpen)
		assert.Equal(t, "scheduled", it.Reason)
		require.NotNil(t, it.KeyName)
		assert.Equal(t, "billing", *it.KeyName)
		assert.Equal(t, "pk", *it.Prefix)
		assert.Equal(t, "test", *it.Environment)
	})
}

// rotCountingKeys counts Get calls per key, and can fail them.
type rotCountingKeys struct {
	key.Store
	gets map[string]int
	fail bool
}

func (w *rotCountingKeys) Get(ctx context.Context, kid id.KeyID) (*key.Key, error) {
	w.gets[kid.String()]++
	if w.fail {
		return nil, errors.New("dial tcp 10.0.0.1: secret-connection-detail")
	}
	return w.Store.Get(ctx, kid)
}

type rotKeysStore struct {
	store.Store
	keys key.Store
}

func (w rotKeysStore) Keys() key.Store { return w.keys }

func TestRotationsListLoadsEachKeyOnce(t *testing.T) {
	base := memory.New()
	counting := &rotCountingKeys{Store: base.Keys(), gets: map[string]int{}}
	deps, eng := setup(t, rotKeysStore{Store: base, keys: counting})
	k := create(t, eng, "t1", nil)
	now := time.Now()
	for i := 0; i < 4; i++ {
		rotSeed(t, base, "t1", k.Key.ID, rotation.ReasonManual, now.Add(-time.Duration(i+1)*time.Minute), time.Hour)
	}
	counting.gets = map[string]int{}
	out, err := rotList(deps, rotationsListRequest{})
	require.NoError(t, err)
	assert.Len(t, out.Items, 4)
	assert.Equal(t, map[string]int{k.Key.ID.String(): 1}, counting.gets)
}

func TestRotationsListAnswersInternalWhenAReadFails(t *testing.T) {
	base := memory.New()
	counting := &rotCountingKeys{Store: base.Keys(), gets: map[string]int{}}
	deps, eng := setup(t, rotKeysStore{Store: base, keys: counting})
	k := create(t, eng, "t1", nil)
	rotSeed(t, base, "t1", k.Key.ID, rotation.ReasonManual, time.Now().Add(-time.Minute), time.Hour)
	counting.fail = true
	_, err := rotList(deps, rotationsListRequest{})
	assert.Equal(t, dashcontract.CodeInternal, codeOf(t, err))
	assert.NotContains(t, err.Error(), "10.0.0.1")

	flaky := &rotateFlakyRotations{Store: base.Rotations(), failFrom: 1}
	deps, _ = setup(t, rotateStore{Store: base, rots: flaky})
	_, err = rotList(deps, rotationsListRequest{})
	assert.Equal(t, dashcontract.CodeInternal, codeOf(t, err))
	assert.NotContains(t, err.Error(), "connection reset")
}

func TestRotationsListRefusesWithoutAUserOrTenant(t *testing.T) {
	deps, _ := setup(t, memory.New())
	_, err := rotationsListHandler(deps)(context.Background(), rotationsListRequest{}, dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))
	deps.DefaultTenantID = ""
	_, err = rotList(deps, rotationsListRequest{})
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
}

func TestRotationsListIsDispatchedAsAReadQuery(t *testing.T) {
	deps, eng := setup(t, memory.New())
	k := create(t, eng, "t1", nil)
	_, err := eng.RotateKey(tctx("t1"), k.Key.ID, rotation.ReasonManual)
	require.NoError(t, err)
	d := createTestDispatcher(t, deps)
	data, _, err := d.Dispatch(context.Background(), dashcontract.Request{
		Envelope: "v1", Kind: dashcontract.KindQuery, Contributor: ContributorName,
		Intent: "rotations.list", IntentVersion: 1,
		Params: map[string]any{"keyId": k.Key.ID.String()},
	}, principal())
	require.NoError(t, err)
	var out rotationsListResponse
	require.NoError(t, json.Unmarshal(data, &out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, k.Key.ID.String(), out.Items[0].KeyID)
}
