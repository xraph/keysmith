package contract

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
)

func rotateReq(keyID id.KeyID, reason string, grace *int64) keysRotateRequest {
	return keysRotateRequest{ID: keyID.String(), Reason: reason, GraceSeconds: grace}
}

func rotateInt64(n int64) *int64 { return &n }

func rotateRecords(t *testing.T, s store.Store, keyID id.KeyID) []*rotation.Record {
	t.Helper()
	recs, err := s.Rotations().List(context.Background(), &rotation.ListFilter{KeyID: &keyID})
	require.NoError(t, err)
	return recs
}

// rotateStore lets a test swap one sub-store of a real backend.
type rotateStore struct {
	store.Store
	rots rotation.Store
	pols policy.Store
}

func (w rotateStore) Rotations() rotation.Store {
	if w.rots != nil {
		return w.rots
	}
	return w.Store.Rotations()
}

func (w rotateStore) Policies() policy.Store {
	if w.pols != nil {
		return w.pols
	}
	return w.Store.Policies()
}

type rotateFailingPolicies struct{ policy.Store }

func (rotateFailingPolicies) Get(context.Context, id.PolicyID) (*policy.Policy, error) {
	return nil, createPlainErr("dial tcp 10.0.0.1: secret-connection-detail")
}

// rotateFlakyRotations fails List from its failFrom-th call on.
type rotateFlakyRotations struct {
	rotation.Store
	calls    int
	failFrom int
}

func (w *rotateFlakyRotations) List(ctx context.Context, f *rotation.ListFilter) ([]*rotation.Record, error) {
	w.calls++
	if w.failFrom > 0 && w.calls >= w.failFrom {
		return nil, createPlainErr("list rotations: connection reset")
	}
	return w.Store.List(ctx, f)
}

// rotateOverlapRotations replays the last record of page one at the start of
// page two, as a rotation inserted between the two reads would.
type rotateOverlapRotations struct {
	rotation.Store
	lastOfFirst *rotation.Record
}

func (w *rotateOverlapRotations) List(ctx context.Context, f *rotation.ListFilter) ([]*rotation.Record, error) {
	recs, err := w.Store.List(ctx, f)
	if err != nil {
		return nil, err
	}
	switch {
	case f.Offset == 0 && len(recs) > 0:
		w.lastOfFirst = recs[len(recs)-1]
	case f.Offset > 0 && w.lastOfFirst != nil:
		recs = append([]*rotation.Record{w.lastOfFirst}, recs...)
	}
	return recs, nil
}

func TestKeysRotateAnswersAKeyThatValidatesAndTheOldHint(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		scopeA := mkScope(t, eng, "t1", "read", "")
		created := create(t, eng, "t1", &keysmith.CreateKeyInput{
			Name: "k", Prefix: "sk", Environment: "live", Scopes: []string{scopeA.Name},
		})
		oldHint, oldRaw := created.Key.Hint, created.RawKey

		out, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", nil), principal())
		require.NoError(t, err)

		require.NotEmpty(t, out.RawKey)
		assert.NotEqual(t, oldRaw, out.RawKey)
		assert.True(t, strings.HasSuffix(out.RawKey, out.Key.Hint),
			"raw key (length %d) does not end in the hint %q", len(out.RawKey), out.Key.Hint)
		assert.NotEqual(t, oldHint, out.Key.Hint)
		assert.NotEmpty(t, out.Key.RotatedAt)
		assert.Equal(t, []string{"read"}, out.Key.Scopes)

		res, err := eng.ValidateKey(tctx("t1"), out.RawKey)
		require.NoError(t, err, "new raw key of length %d did not validate", len(out.RawKey))
		assert.Equal(t, created.Key.ID.String(), res.Key.ID.String())
		// The old key rides its grace window.
		res, err = eng.ValidateKey(tctx("t1"), oldRaw)
		require.NoError(t, err)
		assert.True(t, res.ViaPreviousKey)

		require.Len(t, out.PreviousKeys, 1)
		pk := out.PreviousKeys[0]
		assert.Equal(t, oldHint, pk.Hint)
		assert.Equal(t, "manual", pk.Reason)
		assert.NotEmpty(t, pk.RotationID)
		recs := rotateRecords(t, s, created.Key.ID)
		require.Len(t, recs, 1)
		assert.Equal(t, recs[0].ID.String(), pk.RotationID)

		// The response matches what the detail read says afterwards.
		detail, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: out.Key.ID}, principal())
		require.NoError(t, err)
		assert.Equal(t, detail.Key.Hint, out.Key.Hint)
		assert.Equal(t, detail.PreviousKeys, out.PreviousKeys)
	})
}

func TestKeysRotateRecordsTheSubjectAndReason(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		for _, reason := range []string{"manual", "compromise", "policy"} {
			out, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, reason, rotateInt64(60)), principal())
			require.NoError(t, err, reason)
			assert.NotEmpty(t, out.RawKey)
		}
		recs := rotateRecords(t, s, created.Key.ID)
		require.Len(t, recs, 3)
		reasons := map[string]bool{}
		for _, r := range recs {
			assert.Equal(t, "user_1", r.RotatedBy)
			reasons[string(r.Reason)] = true
		}
		assert.Equal(t, map[string]bool{"manual": true, "compromise": true, "policy": true}, reasons)
	})
}

func TestKeysRotateWithZeroGraceStillListsEarlierWindows(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		firstHint := created.Key.Hint
		h := keysRotateHandler(deps)

		first, err := h(context.Background(), rotateReq(created.Key.ID, "manual", rotateInt64(3600)), principal())
		require.NoError(t, err)
		require.Len(t, first.PreviousKeys, 1)
		secondHint := first.Key.Hint
		secondRaw := first.RawKey

		second, err := h(context.Background(), rotateReq(created.Key.ID, "compromise", rotateInt64(0)), principal())
		require.NoError(t, err)
		// Only the first rotation's window is open. The second ended at once.
		require.Len(t, second.PreviousKeys, 1)
		assert.Equal(t, firstHint, second.PreviousKeys[0].Hint)
		assert.Equal(t, first.PreviousKeys[0].RotationID, second.PreviousKeys[0].RotationID)
		for _, pk := range second.PreviousKeys {
			assert.NotEqual(t, secondHint, pk.Hint, "a zero-grace rotation opened a window")
		}
		// And the key it replaced really stopped.
		_, err = eng.ValidateKey(tctx("t1"), secondRaw)
		assert.Error(t, err)
		_, err = eng.ValidateKey(tctx("t1"), second.RawKey)
		require.NoError(t, err)
	})
}

func TestKeysRotateWithZeroGraceAndNoEarlierWindowAnswersNone(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		out, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "compromise", rotateInt64(0)), principal())
		require.NoError(t, err)
		assert.NotNil(t, out.PreviousKeys, "previousKeys must be [] on the wire, never null")
		assert.Empty(t, out.PreviousKeys)
		raw, err := json.Marshal(out)
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"previousKeys":[]`)
	})
}

// The default grace is resolved in the handler, so the window the response
// shows is pinned against the record the engine stored.
func TestKeysRotateDefaultGraceFollowsThePolicyElseTwentyFourHours(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		pol2h := mkPolicy(t, eng, "t1", &policy.Policy{Name: "two hours", GracePeriod: 2 * time.Hour})
		pol0 := mkPolicy(t, eng, "t1", &policy.Policy{Name: "unset"})

		cases := []struct {
			name     string
			policyID *id.PolicyID
			want     time.Duration
		}{
			{"two hour policy", &pol2h.ID, 2 * time.Hour},
			{"policy without a grace", &pol0.ID, 24 * time.Hour},
			{"no policy", nil, 24 * time.Hour},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				created := create(t, eng, "t1", &keysmith.CreateKeyInput{
					Name: "k", Prefix: "sk", Environment: "live", PolicyID: tc.policyID,
				})
				out, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", nil), principal())
				require.NoError(t, err)
				require.Len(t, out.PreviousKeys, 1)

				recs := rotateRecords(t, s, created.Key.ID)
				require.Len(t, recs, 1)
				assert.Equal(t, tc.want, recs[0].GraceTTL)
				got, perr := time.Parse(time.RFC3339, out.PreviousKeys[0].GraceEnds)
				require.NoError(t, perr)
				assert.WithinDuration(t, recs[0].GraceEnds, got, 2*time.Second)
				assert.Equal(t, recs[0].ID.String(), out.PreviousKeys[0].RotationID)
			})
		}
	})
}

func TestKeysRotateExplicitGraceBeatsThePolicy(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "two hours", GracePeriod: 2 * time.Hour})
		created := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: "live", PolicyID: &pol.ID})
		_, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", rotateInt64(90)), principal())
		require.NoError(t, err)
		recs := rotateRecords(t, s, created.Key.ID)
		require.Len(t, recs, 1)
		assert.Equal(t, 90*time.Second, recs[0].GraceTTL)
	})
}

func TestKeysRotateAcceptsTheGraceBounds(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		_, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", rotateInt64(7776000)), principal())
		require.NoError(t, err)
		_, err = keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", rotateInt64(0)), principal())
		require.NoError(t, err)
	})
}

func TestKeysRotateRefusesBadInputBeforeRotating(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		h := keysRotateHandler(deps)

		for _, reason := range []string{"scheduled", "", "Manual", "other"} {
			_, err := h(context.Background(), rotateReq(created.Key.ID, reason, nil), principal())
			assert.Equal(t, "reason must be one of manual, compromise, policy", badRequestMessage(t, err), reason)
		}
		for _, g := range []int64{-1, 7776001} {
			_, err := h(context.Background(), rotateReq(created.Key.ID, "manual", rotateInt64(g)), principal())
			assert.Equal(t, "graceSeconds must be between 0 and 7776000", badRequestMessage(t, err))
		}
		_, err := h(context.Background(), keysRotateRequest{Reason: "manual"}, principal())
		assert.Equal(t, "id is required", badRequestMessage(t, err))
		_, err = h(context.Background(), keysRotateRequest{ID: "nope", Reason: "manual"}, principal())
		assert.Equal(t, "id is not a key id", badRequestMessage(t, err))

		assert.Empty(t, rotateRecords(t, s, created.Key.ID), "a refused request rotated the key")
	})
}

func TestKeysRotateRefusesAnUnauthenticatedCaller(t *testing.T) {
	deps, eng := setup(t, createMemoryStore())
	created := create(t, eng, "t1", nil)
	_, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", nil), dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))
	assert.Empty(t, rotateRecords(t, deps.Engine.Store(), created.Key.ID))
}

func TestKeysRotateAnotherTenantsKeyIsNotFound(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := create(t, eng, "t2", nil)
		missing := id.NewKeyID()

		_, errTheirs := keysRotateHandler(deps)(context.Background(), rotateReq(theirs.Key.ID, "manual", nil), principal())
		_, errMissing := keysRotateHandler(deps)(context.Background(), rotateReq(missing, "manual", nil), principal())
		assert.Equal(t, dashcontract.CodeNotFound, codeOf(t, errTheirs))
		assert.Equal(t, errMissing, errTheirs)
		assert.Empty(t, rotateRecords(t, s, theirs.Key.ID))

		// Their key still validates with the hash it had.
		_, err := eng.ValidateKey(tctx("t2"), theirs.RawKey)
		require.NoError(t, err)
	})
}

func TestKeysRotateRevokedKeyIsAConflict(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		require.NoError(t, eng.RevokeKey(tctx("t1"), created.Key.ID, "test"))

		_, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", nil), principal())
		require.Error(t, err)
		assert.Equal(t, dashcontract.CodeConflict, codeOf(t, err))
		var ce *dashcontract.Error
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, "a revoked or expired key cannot be rotated", ce.Message)
		assert.Empty(t, rotateRecords(t, s, created.Key.ID))
	})
}

// Every read happens before RotateKey: a failure after it would lose the raw
// key and leave a live key nobody holds.
func TestKeysRotateReadFailureBeforeRotatingLeavesTheKeyAlone(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "p", GracePeriod: time.Hour})
		created := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: "live", PolicyID: &pol.ID})

		broken, err := keysmith.NewEngine(keysmith.WithStore(rotateStore{Store: s, pols: rotateFailingPolicies{s.Policies()}}))
		require.NoError(t, err)
		deps := Deps{Engine: broken, DefaultTenantID: "t1"}

		_, err = keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", nil), principal())
		require.Error(t, err)
		assert.Equal(t, dashcontract.CodeInternal, codeOf(t, err))
		assert.NotContains(t, err.Error(), "secret-connection-detail")

		assert.Empty(t, rotateRecords(t, s, created.Key.ID))
		_, err = eng.ValidateKey(tctx("t1"), created.RawKey)
		require.NoError(t, err)
		again, err := eng.GetKey(tctx("t1"), created.Key.ID)
		require.NoError(t, err)
		assert.Equal(t, created.Key.Hint, again.Hint)
		assert.Nil(t, again.RotatedAt)
	})
}

func TestKeysRotateRotatesEvenWhenTheKeysPolicyIsGone(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		gone := id.NewPolicyID()
		created := create(t, eng, "t1", nil)
		k, err := eng.GetKey(tctx("t1"), created.Key.ID)
		require.NoError(t, err)
		k.PolicyID = &gone
		require.NoError(t, s.Keys().Update(context.Background(), k))

		out, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", nil), principal())
		require.NoError(t, err)
		assert.NotEmpty(t, out.RawKey)
		recs := rotateRecords(t, s, created.Key.ID)
		require.Len(t, recs, 1)
		assert.Equal(t, 24*time.Hour, recs[0].GraceTTL)
	})
}

// A failed window read after the rotation must not cost the caller the raw
// key. The answer is built from what was read before plus the new window.
func TestKeysRotateStillAnswersTheRawKeyWhenTheWindowReadFails(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		firstHint := created.Key.Hint

		// An earlier rotation, so the pre-read has a window to carry over.
		okDeps := Deps{Engine: eng, DefaultTenantID: "t1"}
		first, err := keysRotateHandler(okDeps)(context.Background(), rotateReq(created.Key.ID, "manual", rotateInt64(3600)), principal())
		require.NoError(t, err)
		secondHint := first.Key.Hint

		// Call 1 is the pre-read; call 2, after the rotation, fails.
		flaky := &rotateFlakyRotations{Store: s.Rotations(), failFrom: 2}
		broken, err := keysmith.NewEngine(keysmith.WithStore(rotateStore{Store: s, rots: flaky}))
		require.NoError(t, err)
		deps := Deps{Engine: broken, DefaultTenantID: "t1"}

		out, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "compromise", rotateInt64(1800)), principal())
		require.NoError(t, err, "a window read failure after the rotation must not fail the call")
		assert.Equal(t, 2, flaky.calls)

		_, err = eng.ValidateKey(tctx("t1"), out.RawKey)
		require.NoError(t, err, "raw key of length %d did not validate", len(out.RawKey))

		require.Len(t, out.PreviousKeys, 2)
		hints := []string{out.PreviousKeys[0].Hint, out.PreviousKeys[1].Hint}
		assert.ElementsMatch(t, []string{firstHint, secondHint}, hints)

		var added PreviousKey
		for _, pk := range out.PreviousKeys {
			if pk.Hint == secondHint {
				added = pk
			}
		}
		assert.Equal(t, "compromise", added.Reason)
		assert.Equal(t, "", added.RotationID)
		recs := rotateRecords(t, s, created.Key.ID)
		require.Len(t, recs, 2)
		var stored *rotation.Record
		for _, r := range recs {
			if r.OldHint == secondHint {
				stored = r
			}
		}
		require.NotNil(t, stored)
		gotEnds, perr := time.Parse(time.RFC3339, added.GraceEnds)
		require.NoError(t, perr)
		assert.WithinDuration(t, stored.GraceEnds, gotEnds, 2*time.Second)
		gotAt, perr := time.Parse(time.RFC3339, added.RotatedAt)
		require.NoError(t, perr)
		assert.WithinDuration(t, stored.CreatedAt, gotAt, 2*time.Second)
	})
}

func TestKeysRotateFallbackOmitsTheNewWindowForZeroGrace(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		flaky := &rotateFlakyRotations{Store: s.Rotations(), failFrom: 2}
		broken, err := keysmith.NewEngine(keysmith.WithStore(rotateStore{Store: s, rots: flaky}))
		require.NoError(t, err)
		deps := Deps{Engine: broken, DefaultTenantID: "t1"}

		out, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "compromise", rotateInt64(0)), principal())
		require.NoError(t, err)
		assert.NotEmpty(t, out.RawKey)
		assert.NotNil(t, out.PreviousKeys)
		assert.Empty(t, out.PreviousKeys)
	})
}

func TestKeysRotateIsDispatchedAndNullGraceMeansDefault(t *testing.T) {
	deps, eng := setup(t, createMemoryStore())
	d := createTestDispatcher(t, deps)
	created := create(t, eng, "t1", nil)

	dispatch := func(payload string) (keysRotateResponse, error) {
		req := dashcontract.Request{
			Envelope: "v1", Kind: dashcontract.KindCommand, Contributor: ContributorName,
			Intent: "keys.rotate", IntentVersion: 1,
			Payload: json.RawMessage(payload),
		}
		data, _, err := d.Dispatch(context.Background(), req, principal())
		if err != nil {
			return keysRotateResponse{}, err
		}
		var out keysRotateResponse
		require.NoError(t, json.Unmarshal(data, &out))
		return out, nil
	}

	keyID := created.Key.ID.String()
	out, err := dispatch(`{"id":"` + keyID + `","reason":"manual","graceSeconds":null}`)
	require.NoError(t, err)
	require.Len(t, out.PreviousKeys, 1)
	recs := rotateRecords(t, deps.Engine.Store(), created.Key.ID)
	require.Len(t, recs, 1)
	assert.Equal(t, 24*time.Hour, recs[0].GraceTTL)

	out, err = dispatch(`{"id":"` + keyID + `","reason":"manual","graceSeconds":0}`)
	require.NoError(t, err)
	// Only the first rotation's window remains.
	require.Len(t, out.PreviousKeys, 1)
	assert.Equal(t, recs[0].ID.String(), out.PreviousKeys[0].RotationID)
}

func TestKeysEndGraceClosesEveryWindow(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		raws := []string{created.RawKey}
		for i := 0; i < 2; i++ {
			out, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", rotateInt64(3600)), principal())
			require.NoError(t, err)
			raws = append(raws, out.RawKey)
		}
		before, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: created.Key.ID.String()}, principal())
		require.NoError(t, err)
		require.Len(t, before.PreviousKeys, 2)

		out, err := keysEndGraceHandler(deps)(context.Background(), keysEndGraceRequest{ID: created.Key.ID.String()}, principal())
		require.NoError(t, err)
		assert.EqualValues(t, 2, out.Closed)
		assert.Equal(t, created.Key.ID.String(), out.Key.ID)
		assert.Equal(t, before.Key.Hint, out.Key.Hint)

		after, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: created.Key.ID.String()}, principal())
		require.NoError(t, err)
		assert.Empty(t, after.PreviousKeys)

		for _, old := range raws[:2] {
			_, verr := eng.ValidateKey(tctx("t1"), old)
			assert.Error(t, verr, "a previous key of length %d still validates", len(old))
		}
		_, err = eng.ValidateKey(tctx("t1"), raws[2])
		require.NoError(t, err)

		// Nothing left to close.
		again, err := keysEndGraceHandler(deps)(context.Background(), keysEndGraceRequest{ID: created.Key.ID.String()}, principal())
		require.NoError(t, err)
		assert.EqualValues(t, 0, again.Closed)
	})
}

func TestKeysEndGraceRefusals(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := create(t, eng, "t2", nil)
		h := keysEndGraceHandler(deps)

		_, errTheirs := h(context.Background(), keysEndGraceRequest{ID: theirs.Key.ID.String()}, principal())
		_, errMissing := h(context.Background(), keysEndGraceRequest{ID: id.NewKeyID().String()}, principal())
		assert.Equal(t, dashcontract.CodeNotFound, codeOf(t, errTheirs))
		assert.Equal(t, errMissing, errTheirs)

		_, err := h(context.Background(), keysEndGraceRequest{}, principal())
		assert.Equal(t, "id is required", badRequestMessage(t, err))
		_, err = h(context.Background(), keysEndGraceRequest{ID: theirs.Key.ID.String()}, dashcontract.Principal{})
		assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))
	})
}

func TestKeysEndGraceCannotCloseAnotherTenantsWindows(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := create(t, eng, "t2", nil)
		_, err := eng.RotateKey(tctx("t2"), theirs.Key.ID, rotation.ReasonManual)
		require.NoError(t, err)

		_, err = keysEndGraceHandler(deps)(context.Background(), keysEndGraceRequest{ID: theirs.Key.ID.String()}, principal())
		require.Error(t, err)
		_, err = eng.ValidateKey(tctx("t2"), theirs.RawKey)
		require.NoError(t, err, "the old key's window was closed by another tenant")
	})
}

func TestKeysEndGraceIsDispatchedAsAWriteCommand(t *testing.T) {
	deps, eng := setup(t, createMemoryStore())
	d := createTestDispatcher(t, deps)
	created := create(t, eng, "t1", nil)
	req := dashcontract.Request{
		Envelope: "v1", Kind: dashcontract.KindCommand, Contributor: ContributorName,
		Intent: "keys.endGrace", IntentVersion: 1,
		Payload: json.RawMessage(`{"id":"` + created.Key.ID.String() + `"}`),
	}
	data, _, err := d.Dispatch(context.Background(), req, principal())
	require.NoError(t, err)
	var out keysEndGraceResponse
	require.NoError(t, json.Unmarshal(data, &out))
	assert.EqualValues(t, 0, out.Closed)
}

func TestListOpenWindowsDropsARecordSeenOnTwoPages(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		const total = openWindowPage + 1
		ids := map[string]bool{}
		for i := 0; i < total; i++ {
			rec := &rotation.Record{
				ID: id.NewRotationID(), KeyID: k.Key.ID, TenantID: "t1",
				OldKeyHash: "old", NewKeyHash: "new", OldHint: "abcd",
				Reason: rotation.ReasonManual, GraceTTL: time.Hour,
				GraceEnds: now.Add(time.Hour + time.Duration(i)*time.Minute),
				CreatedAt: now.Add(-time.Duration(total-i) * time.Minute),
			}
			ids[rec.ID.String()] = true
			require.NoError(t, s.Rotations().Create(context.Background(), rec))
		}
		overlap := &rotateOverlapRotations{Store: s.Rotations()}
		wrapped, err := keysmith.NewEngine(keysmith.WithStore(rotateStore{Store: s, rots: overlap}))
		require.NoError(t, err)

		got, err := listOpenWindows(context.Background(), wrapped, k.Key.ID, now)
		require.NoError(t, err)
		require.NotNil(t, overlap.lastOfFirst, "the wrapper never saw a first page")
		seen := map[string]bool{}
		for _, pk := range got {
			assert.False(t, seen[pk.RotationID], "rotation %s listed twice", pk.RotationID)
			seen[pk.RotationID] = true
		}
		assert.Len(t, got, total)
		assert.Equal(t, ids, seen)
	})
}
