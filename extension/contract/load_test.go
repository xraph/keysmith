package contract

import (
	"context"
	"errors"
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
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

func TestRequireID(t *testing.T) {
	valid := id.NewKeyID()
	cases := []struct {
		name    string
		raw     string
		message string
	}{
		{"empty", "", "id is required"},
		{"only spaces", "   ", "id is required"},
		{"not a key id", "not-a-key", "id is not a key id"},
		{"valid", valid.String(), ""},
		{"valid with surrounding spaces", "  " + valid.String() + "\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := requireID(tc.raw)
			if tc.message != "" {
				var ce *dashcontract.Error
				require.ErrorAs(t, err, &ce)
				assert.Equal(t, dashcontract.CodeBadRequest, ce.Code)
				assert.Equal(t, tc.message, ce.Message)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, valid, got)
		})
	}
}

func TestLoadKeyForTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mine := create(t, eng, "t1", nil)
		theirs := create(t, eng, "t2", nil)

		k, err := loadKeyForTenant(context.Background(), deps, "t1", mine.Key.ID.String(), "keys.detail")
		require.NoError(t, err)
		assert.Equal(t, mine.Key.ID, k.ID)

		_, otherTenant := loadKeyForTenant(context.Background(), deps, "t1", theirs.Key.ID.String(), "keys.detail")
		_, missing := loadKeyForTenant(context.Background(), deps, "t1", id.NewKeyID().String(), "keys.detail")
		require.Error(t, otherTenant)
		assert.Equal(t, dashcontract.CodeNotFound, codeOf(t, otherTenant))
		// Another tenant's key and a key that does not exist are the same answer.
		assert.Equal(t, missing, otherTenant)
		assert.Equal(t, keyNotFound(), otherTenant)

		_, err = loadKeyForTenant(context.Background(), deps, "t1", "", "keys.detail")
		assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err))
	})
}

func TestListOpenWindowsHasNoCap(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		other := create(t, eng, "t1", nil)

		now := time.Now()
		base := now.Add(-200 * time.Hour)
		const total = 120
		var oldest id.RotationID
		for i := 0; i < total; i++ {
			rec := &rotation.Record{
				ID: id.NewRotationID(), KeyID: k.Key.ID, TenantID: "t1",
				OldKeyHash: "old", NewKeyHash: "new", OldHint: "abcd",
				Reason: rotation.ReasonManual, GraceTTL: time.Hour,
				// Oldest first: only the very first record is still open.
				GraceEnds: base.Add(time.Duration(i) * time.Minute),
				CreatedAt: base.Add(time.Duration(i) * time.Minute),
			}
			if i == 0 {
				oldest = rec.ID
				rec.GraceEnds = now.Add(30 * 24 * time.Hour)
			}
			require.NoError(t, s.Rotations().Create(context.Background(), rec))
		}
		// Another key's open window must not leak in.
		require.NoError(t, s.Rotations().Create(context.Background(), &rotation.Record{
			ID: id.NewRotationID(), KeyID: other.Key.ID, TenantID: "t1",
			OldKeyHash: "old", NewKeyHash: "new", OldHint: "zzzz",
			Reason: rotation.ReasonManual, GraceTTL: time.Hour,
			GraceEnds: now.Add(time.Hour), CreatedAt: now,
		}))

		got, err := listOpenWindows(context.Background(), eng, k.Key.ID, now)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, oldest.String(), got[0].RotationID)
		assert.Equal(t, "abcd", got[0].Hint)
	})
}

func TestListOpenWindowsSortsByGraceEndsAndSkipsHintless(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		mk := func(hint string, ends time.Duration, age time.Duration) id.RotationID {
			rec := &rotation.Record{
				ID: id.NewRotationID(), KeyID: k.Key.ID, TenantID: "t1",
				OldKeyHash: "old", NewKeyHash: "new", OldHint: hint,
				Reason: rotation.ReasonManual, GraceTTL: time.Hour,
				GraceEnds: now.Add(ends), CreatedAt: now.Add(-age),
			}
			require.NoError(t, s.Rotations().Create(context.Background(), rec))
			return rec.ID
		}
		late := mk("late", 3*time.Hour, 3*time.Hour)
		soon := mk("soon", time.Hour, time.Hour)
		mk("", 2*time.Hour, 2*time.Hour)    // legacy record, no hint
		mk("shut", -time.Hour, 4*time.Hour) // window already closed

		got, err := listOpenWindows(context.Background(), eng, k.Key.ID, now)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, soon.String(), got[0].RotationID)
		assert.Equal(t, late.String(), got[1].RotationID)
	})
}

// seedWindows writes n rotation records for the key, every one of them an
// open window, and returns their IDs.
func seedWindows(t *testing.T, s store.Store, keyID id.KeyID, n int, now time.Time) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		rec := &rotation.Record{
			ID: id.NewRotationID(), KeyID: keyID, TenantID: "t1",
			OldKeyHash: "old", NewKeyHash: "new", OldHint: "abcd",
			Reason: rotation.ReasonManual, GraceTTL: time.Hour,
			GraceEnds: now.Add(time.Duration(i+1) * time.Minute),
			CreatedAt: now.Add(-time.Duration(i+1) * time.Minute),
		}
		require.NoError(t, s.Rotations().Create(context.Background(), rec))
		ids = append(ids, rec.ID.String())
	}
	return ids
}

func windowIDs(ws []PreviousKey) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.RotationID)
	}
	return out
}

func TestListOpenWindowsExactlyOneFullPage(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		want := seedWindows(t, s, k.Key.ID, openWindowPage, now)

		got, err := listOpenWindows(context.Background(), eng, k.Key.ID, now)
		require.NoError(t, err)
		assert.ElementsMatch(t, want, windowIDs(got))
	})
}

// offsetBlindStore hands out a rotation store whose List ignores Offset, the
// way a backend that does not support it would, and gives up after a few
// calls so a loop that never ends fails the test.
type offsetBlindStore struct {
	store.Store
	calls *int
}

func (s offsetBlindStore) Rotations() rotation.Store {
	return offsetBlindRotations{Store: s.Store.Rotations(), calls: s.calls}
}

type offsetBlindRotations struct {
	rotation.Store
	calls *int
}

func (r offsetBlindRotations) List(ctx context.Context, f *rotation.ListFilter) ([]*rotation.Record, error) {
	*r.calls++
	if *r.calls > 10 {
		return nil, errors.New("listOpenWindows kept paging a store that ignores Offset")
	}
	blind := *f
	blind.Offset = 0
	return r.Store.List(ctx, &blind)
}

func TestListOpenWindowsStopsWhenAStoreIgnoresOffset(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		want := seedWindows(t, s, k.Key.ID, openWindowPage, now)

		calls := 0
		blindEng, err := keysmith.NewEngine(keysmith.WithStore(offsetBlindStore{Store: s, calls: &calls}))
		require.NoError(t, err)

		got, err := listOpenWindows(context.Background(), blindEng, k.Key.ID, now)
		require.NoError(t, err)
		// Every window once, from one full page and one that added nothing.
		assert.ElementsMatch(t, want, windowIDs(got))
		assert.Len(t, got, openWindowPage)
		assert.Equal(t, 2, calls)
	})
}

// loadFailingKeys fails every Get the way a dropped connection would.
type loadFailingKeys struct{ key.Store }

func (loadFailingKeys) Get(context.Context, id.KeyID) (*key.Key, error) {
	return nil, errors.New("dial tcp 10.0.0.1: connection reset")
}

// A loader that fails logs the intent that called it, not a label of its
// own, so an operator can tell which request hit the failure.
func TestLoadersLogTheCallersIntent(t *testing.T) {
	s := memory.New()
	_, eng := setup(t, s)
	k := create(t, eng, "t1", nil)
	pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "p"})

	var lines []rotateLogLine
	logger := rotateCapturingLogger{Logger: forge.NewNoopLogger(), lines: &lines}

	keysBroken, err := keysmith.NewEngine(keysmith.WithStore(stateRaceStore{Store: s, keys: loadFailingKeys{s.Keys()}}))
	require.NoError(t, err)
	deps := Deps{Engine: keysBroken, DefaultTenantID: "t1", Logger: logger}
	_, err = keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: k.Key.ID.String()}, principal())
	assert.Equal(t, dashcontract.CodeInternal, codeOf(t, err))
	_, err = loadKeyForTenant(context.Background(), deps, "t1", k.Key.ID.String(), "keys.revoke")
	assert.Equal(t, dashcontract.CodeInternal, codeOf(t, err))

	polsBroken, err := keysmith.NewEngine(keysmith.WithStore(rotateStore{Store: s, pols: rotateFailingPolicies{s.Policies()}}))
	require.NoError(t, err)
	deps.Engine = polsBroken
	_, err = policiesDetailHandler(deps)(context.Background(), policyIDRequest{ID: pol.ID.String()}, principal())
	assert.Equal(t, dashcontract.CodeInternal, codeOf(t, err))
	assert.NotContains(t, err.Error(), "secret-connection-detail")

	intents := make([]string, 0, len(lines))
	for _, l := range lines {
		intents = append(intents, l.fields["intent"])
	}
	assert.Equal(t, []string{"keys.detail", "keys.revoke", "policies.detail"}, intents)
}
