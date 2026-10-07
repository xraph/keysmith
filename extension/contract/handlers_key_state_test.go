package contract

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store"
)

func stateRevokeReq(keyID id.KeyID, reason string) keysRevokeRequest {
	return keysRevokeRequest{ID: keyID.String(), Reason: reason}
}

func stateIDReq(keyID id.KeyID) keyIDRequest { return keyIDRequest{ID: keyID.String()} }

func stateDetail(t *testing.T, deps Deps, keyID id.KeyID) keysDetailResponse {
	t.Helper()
	out, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: keyID.String()}, principal())
	require.NoError(t, err)
	return out
}

func stateConflictMessage(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, dashcontract.CodeConflict, codeOf(t, err))
	var ce *dashcontract.Error
	require.ErrorAs(t, err, &ce)
	return ce.Message
}

// stateRecorder is a hook plugin that remembers the reason a revoke carried.
type stateRecorder struct {
	mu      sync.Mutex
	reasons []string
}

func (*stateRecorder) Name() string { return "state-recorder" }

func (r *stateRecorder) OnKeyRevoked(_ context.Context, _ *key.Key, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reasons = append(r.reasons, reason)
	return nil
}

// stateRaceKeys answers the first Get as stored and every later one with the
// key already in racedState, as if another request changed it in between.
type stateRaceKeys struct {
	key.Store
	racedState key.State
	calls      int
}

func (w *stateRaceKeys) Get(ctx context.Context, keyID id.KeyID) (*key.Key, error) {
	k, err := w.Store.Get(ctx, keyID)
	if err != nil {
		return nil, err
	}
	w.calls++
	if w.calls > 1 {
		c := *k
		c.State = w.racedState
		return &c, nil
	}
	return k, nil
}

type stateRaceStore struct {
	store.Store
	keys key.Store
}

func (w stateRaceStore) Keys() key.Store { return w.keys }

func TestKeysRevokeChangesTheNextDetailAndStopsTheKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)

		out, err := keysRevokeHandler(deps)(context.Background(), stateRevokeReq(created.Key.ID, "leaked in a log"), principal())
		require.NoError(t, err)
		assert.Equal(t, created.Key.ID.String(), out.Key.ID)
		assert.Equal(t, "revoked", out.Key.State)
		assert.Equal(t, "revoked", out.Key.EffectiveState)
		assert.NotEmpty(t, out.Key.RevokedAt)

		detail := stateDetail(t, deps, created.Key.ID)
		assert.Equal(t, out.Key, detail.Key)
		assert.Equal(t, "revoked", detail.Key.EffectiveState)

		_, err = eng.ValidateKey(tctx("t1"), created.RawKey)
		assert.ErrorIs(t, err, keysmith.ErrKeyInactive)
	})
}

func TestKeysRevokeEndsOpenWindows(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		raws := []string{created.RawKey}
		for i := 0; i < 2; i++ {
			out, err := keysRotateHandler(deps)(context.Background(), rotateReq(created.Key.ID, "manual", rotateInt64(3600)), principal())
			require.NoError(t, err)
			raws = append(raws, out.RawKey)
		}
		require.Len(t, stateDetail(t, deps, created.Key.ID).PreviousKeys, 2)

		_, err := keysRevokeHandler(deps)(context.Background(), stateRevokeReq(created.Key.ID, "compromised"), principal())
		require.NoError(t, err)

		detail := stateDetail(t, deps, created.Key.ID)
		assert.Empty(t, detail.PreviousKeys)
		assert.NotNil(t, detail.PreviousKeys, "previousKeys must be [] on the wire, never null")
		for _, raw := range raws {
			_, verr := eng.ValidateKey(tctx("t1"), raw)
			assert.Error(t, verr, "a key of length %d still validates after a revoke", len(raw))
		}
	})
}

func TestKeysRevokeTrimsTheReasonAndPassesItOn(t *testing.T) {
	rec := &stateRecorder{}
	eng, err := keysmith.NewEngine(keysmith.WithStore(createMemoryStore()), keysmith.WithExtension(rec))
	require.NoError(t, err)
	deps := Deps{Engine: eng, DefaultTenantID: "t1"}
	created := create(t, eng, "t1", nil)

	_, err = keysRevokeHandler(deps)(context.Background(), stateRevokeReq(created.Key.ID, "  leaked in a log \n"), principal())
	require.NoError(t, err)
	assert.Equal(t, []string{"leaked in a log"}, rec.reasons)
}

func TestKeysRevokeReasonRules(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		h := keysRevokeHandler(deps)
		created := create(t, eng, "t1", nil)

		for _, blank := range []string{"", "   ", "\n\t"} {
			_, err := h(context.Background(), stateRevokeReq(created.Key.ID, blank), principal())
			assert.Equal(t, "reason is required", badRequestMessage(t, err), "reason %q", blank)
		}
		_, err := h(context.Background(), stateRevokeReq(created.Key.ID, strings.Repeat("a", 501)), principal())
		assert.Equal(t, "reason must be at most 500 characters", badRequestMessage(t, err))
		// Nothing was revoked by the refusals.
		assert.Equal(t, "active", stateDetail(t, deps, created.Key.ID).Key.EffectiveState)

		// 500 characters pass, counted after trimming and as characters
		// rather than bytes.
		_, err = h(context.Background(), stateRevokeReq(created.Key.ID, "  "+strings.Repeat("é", 500)+"  "), principal())
		require.NoError(t, err)
	})
}

func TestKeysRevokeAlreadyRevokedIsAConflict(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		h := keysRevokeHandler(deps)
		_, err := h(context.Background(), stateRevokeReq(created.Key.ID, "first"), principal())
		require.NoError(t, err)

		_, err = h(context.Background(), stateRevokeReq(created.Key.ID, "second"), principal())
		assert.Equal(t, "this key is already revoked", stateConflictMessage(t, err))
	})
}

func TestKeysRevokeAcceptsSuspendedAndExpiredKeys(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		h := keysRevokeHandler(deps)

		suspended := create(t, eng, "t1", nil)
		require.NoError(t, eng.SuspendKey(tctx("t1"), suspended.Key.ID))
		out, err := h(context.Background(), stateRevokeReq(suspended.Key.ID, "no longer needed"), principal())
		require.NoError(t, err)
		assert.Equal(t, "revoked", out.Key.EffectiveState)

		past := time.Now().Add(-time.Minute)
		expired := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "old", Prefix: "sk", Environment: key.EnvLive, ExpiresAt: &past})
		out, err = h(context.Background(), stateRevokeReq(expired.Key.ID, "tidy up"), principal())
		require.NoError(t, err)
		assert.Equal(t, "revoked", out.Key.EffectiveState)
	})
}

func TestKeysSuspendThenReactivateChangesTheNextDetail(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)

		out, err := keysSuspendHandler(deps)(context.Background(), stateIDReq(created.Key.ID), principal())
		require.NoError(t, err)
		assert.Equal(t, "suspended", out.Key.State)
		assert.Equal(t, "suspended", out.Key.EffectiveState)
		detail := stateDetail(t, deps, created.Key.ID)
		assert.Equal(t, out.Key, detail.Key)
		_, err = eng.ValidateKey(tctx("t1"), created.RawKey)
		assert.ErrorIs(t, err, keysmith.ErrKeyInactive)

		out, err = keysReactivateHandler(deps)(context.Background(), stateIDReq(created.Key.ID), principal())
		require.NoError(t, err)
		assert.Equal(t, "active", out.Key.State)
		assert.Equal(t, "active", out.Key.EffectiveState)
		detail = stateDetail(t, deps, created.Key.ID)
		assert.Equal(t, out.Key, detail.Key)
		_, err = eng.ValidateKey(tctx("t1"), created.RawKey)
		require.NoError(t, err)
	})
}

func TestKeysSuspendRefusesAnythingButAnActiveKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		h := keysSuspendHandler(deps)
		const want = "only an active key can be suspended"

		suspended := create(t, eng, "t1", nil)
		require.NoError(t, eng.SuspendKey(tctx("t1"), suspended.Key.ID))
		_, err := h(context.Background(), stateIDReq(suspended.Key.ID), principal())
		assert.Equal(t, want, stateConflictMessage(t, err))

		revoked := create(t, eng, "t1", nil)
		require.NoError(t, eng.RevokeKey(tctx("t1"), revoked.Key.ID, "gone"))
		_, err = h(context.Background(), stateIDReq(revoked.Key.ID), principal())
		assert.Equal(t, want, stateConflictMessage(t, err))

		// Stored active, but past its expiry: the dashboard shows it as
		// expired, so the engine is not asked to suspend it.
		past := time.Now().Add(-time.Minute)
		expired := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "old", Prefix: "sk", Environment: key.EnvLive, ExpiresAt: &past})
		_, err = h(context.Background(), stateIDReq(expired.Key.ID), principal())
		assert.Equal(t, want, stateConflictMessage(t, err))
		assert.Equal(t, "active", stateDetail(t, deps, expired.Key.ID).Key.State, "a refused suspend changed the key")
	})
}

func TestKeysReactivateRefusesAnythingButASuspendedKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		h := keysReactivateHandler(deps)
		const want = "only a suspended key can be reactivated"

		active := create(t, eng, "t1", nil)
		_, err := h(context.Background(), stateIDReq(active.Key.ID), principal())
		assert.Equal(t, want, stateConflictMessage(t, err))

		revoked := create(t, eng, "t1", nil)
		require.NoError(t, eng.RevokeKey(tctx("t1"), revoked.Key.ID, "gone"))
		_, err = h(context.Background(), stateIDReq(revoked.Key.ID), principal())
		assert.Equal(t, want, stateConflictMessage(t, err))
		assert.Equal(t, "revoked", stateDetail(t, deps, revoked.Key.ID).Key.EffectiveState)

		past := time.Now().Add(-time.Minute)
		expired := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "old", Prefix: "sk", Environment: key.EnvLive, ExpiresAt: &past})
		_, err = h(context.Background(), stateIDReq(expired.Key.ID), principal())
		assert.Equal(t, want, stateConflictMessage(t, err))
	})
}

func TestKeysReactivateAKeySuspendedBeforeItExpired(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		soon := time.Now().Add(time.Hour)
		k := create(t, eng, "t1", &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, ExpiresAt: &soon})
		require.NoError(t, eng.SuspendKey(tctx("t1"), k.Key.ID))

		out, err := keysReactivateHandler(deps)(context.Background(), stateIDReq(k.Key.ID), principal())
		require.NoError(t, err)
		assert.Equal(t, "active", out.Key.EffectiveState)
	})
}

// A request that raced another one reaches the engine with a key the
// handler read in a different state. The engine refuses and the handler says
// the same thing it would have said before the call.
func TestKeysStateChangesMapAnEngineRefusalFromARace(t *testing.T) {
	cases := []struct {
		name    string
		call    func(Deps, id.KeyID) error
		raced   key.State
		message string
	}{
		{"suspend", func(d Deps, kid id.KeyID) error {
			_, err := keysSuspendHandler(d)(context.Background(), stateIDReq(kid), principal())
			return err
		}, key.StateSuspended, "only an active key can be suspended"},
		{"reactivate", func(d Deps, kid id.KeyID) error {
			_, err := keysReactivateHandler(d)(context.Background(), stateIDReq(kid), principal())
			return err
		}, key.StateActive, "only a suspended key can be reactivated"},
		{"revoke", func(d Deps, kid id.KeyID) error {
			_, err := keysRevokeHandler(d)(context.Background(), stateRevokeReq(kid, "r"), principal())
			return err
		}, key.StateRevoked, "this key is already revoked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := createMemoryStore()
			_, eng := setup(t, base)
			created := create(t, eng, "t1", nil)
			if tc.name == "reactivate" {
				require.NoError(t, eng.SuspendKey(tctx("t1"), created.Key.ID))
			}
			racing := &stateRaceKeys{Store: base.Keys(), racedState: tc.raced}
			wrapped, err := keysmith.NewEngine(keysmith.WithStore(stateRaceStore{Store: base, keys: racing}))
			require.NoError(t, err)
			deps := Deps{Engine: wrapped, DefaultTenantID: "t1"}

			assert.Equal(t, tc.message, stateConflictMessage(t, tc.call(deps, created.Key.ID)))
			assert.GreaterOrEqual(t, racing.calls, 2, "the engine never read the key")
		})
	}
}

func TestKeysStateChangesRefuseAnotherTenantsKeyAndLeaveItAlone(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := create(t, eng, "t2", nil)
		suspended := create(t, eng, "t2", nil)
		require.NoError(t, eng.SuspendKey(tctx("t2"), suspended.Key.ID))

		calls := map[string]func(string) error{
			"revoke": func(raw string) error {
				_, err := keysRevokeHandler(deps)(context.Background(), keysRevokeRequest{ID: raw, Reason: "r"}, principal())
				return err
			},
			"suspend": func(raw string) error {
				_, err := keysSuspendHandler(deps)(context.Background(), keyIDRequest{ID: raw}, principal())
				return err
			},
			"reactivate": func(raw string) error {
				_, err := keysReactivateHandler(deps)(context.Background(), keyIDRequest{ID: raw}, principal())
				return err
			},
		}
		for name, call := range calls {
			target := theirs.Key.ID.String()
			if name == "reactivate" {
				target = suspended.Key.ID.String()
			}
			errTheirs := call(target)
			errMissing := call(id.NewKeyID().String())
			assert.Equal(t, dashcontract.CodeNotFound, codeOf(t, errTheirs), name)
			assert.Equal(t, errMissing, errTheirs, name)
		}

		got, err := eng.GetKey(tctx("t2"), theirs.Key.ID)
		require.NoError(t, err)
		assert.Equal(t, key.StateActive, got.State)
		assert.Nil(t, got.RevokedAt)
		got, err = eng.GetKey(tctx("t2"), suspended.Key.ID)
		require.NoError(t, err)
		assert.Equal(t, key.StateSuspended, got.State)
	})
}

func TestKeysStateChangesRefuseBadInputAndAnonymousCallers(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)
		kid := created.Key.ID

		calls := map[string]func(keyID string, p dashcontract.Principal) error{
			"revoke": func(raw string, p dashcontract.Principal) error {
				_, err := keysRevokeHandler(deps)(context.Background(), keysRevokeRequest{ID: raw, Reason: "r"}, p)
				return err
			},
			"suspend": func(raw string, p dashcontract.Principal) error {
				_, err := keysSuspendHandler(deps)(context.Background(), keyIDRequest{ID: raw}, p)
				return err
			},
			"reactivate": func(raw string, p dashcontract.Principal) error {
				_, err := keysReactivateHandler(deps)(context.Background(), keyIDRequest{ID: raw}, p)
				return err
			},
		}
		for name, call := range calls {
			assert.Equal(t, "id is required", badRequestMessage(t, call("", principal())), name)
			assert.Equal(t, "id is not a key id", badRequestMessage(t, call("nope", principal())), name)
			assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, call(kid.String(), dashcontract.Principal{})), name)
		}
		assert.Equal(t, "active", stateDetail(t, deps, kid).Key.EffectiveState)
	})
}

func TestKeysStateChangesRefuseWithoutATenant(t *testing.T) {
	deps, eng := setup(t, createMemoryStore())
	created := create(t, eng, "t1", nil)
	deps.DefaultTenantID = ""

	_, err := keysRevokeHandler(deps)(context.Background(), stateRevokeReq(created.Key.ID, "r"), principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
	_, err = keysSuspendHandler(deps)(context.Background(), stateIDReq(created.Key.ID), principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
	_, err = keysReactivateHandler(deps)(context.Background(), stateIDReq(created.Key.ID), principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
}

func TestKeysStateChangesAreDispatchedAsWriteCommands(t *testing.T) {
	deps, eng := setup(t, createMemoryStore())
	d := createTestDispatcher(t, deps)
	created := create(t, eng, "t1", nil)

	send := func(intent, payload string) (keyResponse, error) {
		req := dashcontract.Request{
			Envelope: "v1", Kind: dashcontract.KindCommand, Contributor: ContributorName,
			Intent: intent, IntentVersion: 1, Payload: json.RawMessage(payload),
		}
		data, _, err := d.Dispatch(context.Background(), req, principal())
		if err != nil {
			return keyResponse{}, err
		}
		var out keyResponse
		require.NoError(t, json.Unmarshal(data, &out))
		return out, nil
	}
	idJSON := `{"id":"` + created.Key.ID.String() + `"}`

	out, err := send("keys.suspend", idJSON)
	require.NoError(t, err)
	assert.Equal(t, "suspended", out.Key.EffectiveState)
	out, err = send("keys.reactivate", idJSON)
	require.NoError(t, err)
	assert.Equal(t, "active", out.Key.EffectiveState)
	out, err = send("keys.revoke", `{"id":"`+created.Key.ID.String()+`","reason":"done with it"}`)
	require.NoError(t, err)
	assert.Equal(t, "revoked", out.Key.EffectiveState)
}

func TestKeysStateResponsesCarryNoSecret(t *testing.T) {
	deps, eng := setup(t, createMemoryStore())
	created := create(t, eng, "t1", nil)

	out, err := keysSuspendHandler(deps)(context.Background(), stateIDReq(created.Key.ID), principal())
	require.NoError(t, err)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), created.RawKey)
	assert.NotContains(t, strings.ToLower(string(raw)), "rawkey")
	assert.NotContains(t, strings.ToLower(string(raw)), "hash")
}

// conflictKeys refuses every version-checked write, as a store does when
// another write landed between the engine's read and its own.
type conflictKeys struct{ key.Store }

func (conflictKeys) UpdateIfVersion(context.Context, *key.Key, int64) error {
	return store.ErrKeyConflict
}

// A key command whose write lost a race answers CONFLICT and asks the caller
// to reload, and the key is left as the other write put it.
func TestKeyCommandsThatLostARaceAskForAReload(t *testing.T) {
	calls := map[string]func(Deps, id.KeyID) error{
		"revoke": func(d Deps, kid id.KeyID) error {
			_, err := keysRevokeHandler(d)(context.Background(), stateRevokeReq(kid, "r"), principal())
			return err
		},
		"suspend": func(d Deps, kid id.KeyID) error {
			_, err := keysSuspendHandler(d)(context.Background(), stateIDReq(kid), principal())
			return err
		},
		"reactivate": func(d Deps, kid id.KeyID) error {
			_, err := keysReactivateHandler(d)(context.Background(), stateIDReq(kid), principal())
			return err
		},
		"rotate": func(d Deps, kid id.KeyID) error {
			_, err := keysRotateHandler(d)(context.Background(), rotateReq(kid, "manual", nil), principal())
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			base := createMemoryStore()
			_, eng := setup(t, base)
			created := create(t, eng, "t1", nil)
			if name == "reactivate" {
				require.NoError(t, eng.SuspendKey(tctx("t1"), created.Key.ID))
			}
			before, err := eng.GetKey(tctx("t1"), created.Key.ID)
			require.NoError(t, err)
			wrapped, err := keysmith.NewEngine(keysmith.WithStore(stateRaceStore{Store: base, keys: conflictKeys{base.Keys()}}))
			require.NoError(t, err)
			deps := Deps{Engine: wrapped, DefaultTenantID: "t1"}

			assert.Equal(t, "this key changed while you were acting on it. Reload and try again.",
				stateConflictMessage(t, call(deps, created.Key.ID)))
			after, err := eng.GetKey(tctx("t1"), created.Key.ID)
			require.NoError(t, err)
			assert.Equal(t, before.State, after.State)
			assert.Equal(t, before.KeyHash, after.KeyHash)
			assert.Equal(t, before.Version, after.Version)
		})
	}
}
