package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"

	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/store"
)

// idemStore adapts forge's in-memory idempotency store to the dispatcher the
// way forge's dashboard extension does, and keeps a copy of every entry the
// dispatcher writes so a test can read what was stored. Lookup and Store
// mirror forge v1.12.1's unexported adaptIdempotencyStore
// (extensions/dashboard/extension.go). If forge changes that wiring, change
// this to match, or these tests stop testing what a host runs.
type idemStore struct {
	inner idempotency.Store

	mu     sync.Mutex
	stored []dispatcher.IdempotencyCached
}

func (a *idemStore) Lookup(ctx context.Context, key, identity string) (*dispatcher.IdempotencyCached, bool) {
	c, ok := a.inner.Lookup(ctx, key, identity)
	if !ok {
		return nil, false
	}
	return &dispatcher.IdempotencyCached{Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL}, true
}

func (a *idemStore) Store(ctx context.Context, key, identity string, c dispatcher.IdempotencyCached) error {
	a.mu.Lock()
	a.stored = append(a.stored, c)
	a.mu.Unlock()
	return a.inner.Store(ctx, key, identity, idempotency.Cached{
		Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL,
	})
}

// entry reads back what the real store holds for key under the dispatcher's
// identity for principal() and intent.
func (a *idemStore) entry(t *testing.T, key, intent string) *idempotency.Cached {
	t.Helper()
	// "user_1:"+intent mirrors forge's principalIdentity (dispatcher.go):
	// the principal's subject, a colon, then the intent.
	c, ok := a.inner.Lookup(context.Background(), key, "user_1:"+intent)
	require.True(t, ok, "no idempotency entry for %s", intent)
	return c
}

// assertNeverHolds fails if any entry the dispatcher stored carries raw.
func (a *idemStore) assertNeverHolds(t *testing.T, raw string) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, c := range a.stored {
		assert.False(t, bytes.Contains(c.WireBody, []byte(raw)), "stored entry %d holds the raw key", i)
	}
}

func idemDispatcher(t *testing.T, deps Deps) (*dispatcher.Dispatcher, *idemStore) {
	t.Helper()
	s := &idemStore{inner: idempotency.NewInMemoryStore()}
	d := dispatcher.NewWithOptions(nil, dispatcher.WithIdempotencyStore(s))
	require.NoError(t, Register(d, dashcontract.NewRegistry(), dashcontract.NewWardenRegistry(), deps))
	return d, s
}

func idemCommand(t *testing.T, intent, key string, payload any) dashcontract.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return dashcontract.Request{
		Envelope:       "v1",
		Kind:           dashcontract.KindCommand,
		Contributor:    ContributorName,
		Intent:         intent,
		IntentVersion:  1,
		Payload:        body,
		IdempotencyKey: key,
	}
}

// assertSecretNotKept checks a replay of a secret command: CONFLICT, no data,
// and nothing in the answer that could be the key.
func assertSecretNotKept(t *testing.T, data json.RawMessage, err error, raw string) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, dashcontract.CodeConflict, codeOf(t, err))
	assert.Contains(t, err.Error(), "send a new idempotency key")
	assert.NotContains(t, err.Error(), raw)
	assert.Empty(t, data)
}

// A replay of keys.create with the same idempotency key and user answers
// CONFLICT, mints no second key, and the store keeps a tombstone, never the
// raw key.
func TestKeysCreateReplayAnswersConflictAndKeepsNoRawKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, _ := setup(t, s)
		d, idem := idemDispatcher(t, deps)
		req := idemCommand(t, "keys.create", "idem-create-1", validCreate())

		data, _, err := d.Dispatch(context.Background(), req, principal())
		require.NoError(t, err)
		var first keyWithSecretResponse
		require.NoError(t, json.Unmarshal(data, &first))
		require.NotEmpty(t, first.RawKey)

		data, _, err = d.Dispatch(context.Background(), req, principal())
		assertSecretNotKept(t, data, err, first.RawKey)

		total, err := s.Keys().Count(context.Background(), &createTenantFilter)
		require.NoError(t, err)
		assert.EqualValues(t, 1, total, "the replay minted a second key")

		c := idem.entry(t, "idem-create-1", "keys.create")
		assert.Equal(t, dispatcher.TombstoneStatus, c.Status)
		assert.Empty(t, c.WireBody)
		idem.assertNeverHolds(t, first.RawKey)
	})
}

// The same for keys.rotate: the replay answers CONFLICT, the key rotated
// once, and no stored entry holds the new raw key.
func TestKeysRotateReplayAnswersConflictAndKeepsNoRawKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		d, idem := idemDispatcher(t, deps)
		created := create(t, eng, "t1", nil)
		req := idemCommand(t, "keys.rotate", "idem-rotate-1", rotateReq(created.Key.ID, "manual", nil))

		data, _, err := d.Dispatch(context.Background(), req, principal())
		require.NoError(t, err)
		var first keysRotateResponse
		require.NoError(t, json.Unmarshal(data, &first))
		require.NotEmpty(t, first.RawKey)

		data, _, err = d.Dispatch(context.Background(), req, principal())
		assertSecretNotKept(t, data, err, first.RawKey)

		assert.Len(t, rotateRecords(t, s, created.Key.ID), 1, "the replay rotated the key again")
		// The key handed out by the first rotation is still the live one.
		_, err = eng.ValidateKey(tctx("t1"), first.RawKey)
		require.NoError(t, err)

		c := idem.entry(t, "idem-rotate-1", "keys.rotate")
		assert.Equal(t, dispatcher.TombstoneStatus, c.Status)
		assert.Empty(t, c.WireBody)
		idem.assertNeverHolds(t, first.RawKey)
	})
}

// A command without a secret still replays the first answer: same scope,
// one row, and the stored entry is the full success envelope.
func TestNonSecretCommandStillReplays(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, _ := setup(t, s)
		d, idem := idemDispatcher(t, deps)
		req := idemCommand(t, "scopes.create", "idem-scope-1", scopesCreateRequest{Name: "read"})

		first, _, err := d.Dispatch(context.Background(), req, principal())
		require.NoError(t, err)
		second, _, err := d.Dispatch(context.Background(), req, principal())
		require.NoError(t, err, "a replay of a non-secret command must answer from the store")
		assert.JSONEq(t, string(first), string(second))
		assert.Equal(t, []string{"read"}, swNames(t, s, "t1"))

		c := idem.entry(t, "idem-scope-1", "scopes.create")
		assert.Equal(t, http.StatusOK, c.Status)
		assert.NotEmpty(t, c.WireBody)
	})
}
