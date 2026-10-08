package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"

	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store"
)

// idemStore adapts forge's in-memory idempotency store to the dispatcher the
// way forge's dashboard extension does, and keeps a copy of every entry the
// dispatcher writes so a test can read what was stored. idemStore and
// claimingIdemStore mirror forge v1.12.2's unexported adaptIdempotencyStore,
// idempotencyAdapter and claimingIdempotencyAdapter
// (extensions/dashboard/extension.go). If forge changes that wiring, change
// these to match, or these tests stop testing what a host runs.
type idemStore struct {
	inner idempotency.Store

	// enter, when set, runs at the start of every Lookup and Claim the
	// dispatcher makes, so a test can tell when a dispatch reached the store.
	enter func()

	mu     sync.Mutex
	stored []dispatcher.IdempotencyCached
}

// claimingIdemStore is idemStore over a store that can also claim, so the
// dispatcher's type assertion finds a claimer only when forge's would.
type claimingIdemStore struct {
	*idemStore

	claimer idempotency.Claimer
}

// adaptIdem picks the adapter the way adaptIdempotencyStore does.
func adaptIdem(a *idemStore) dispatcher.IdempotencyStore {
	if c, ok := a.inner.(idempotency.Claimer); ok {
		return &claimingIdemStore{idemStore: a, claimer: c}
	}
	return a
}

func (a *idemStore) record(c dispatcher.IdempotencyCached) {
	a.mu.Lock()
	a.stored = append(a.stored, c)
	a.mu.Unlock()
}

func (a *idemStore) Lookup(ctx context.Context, key, identity string) (*dispatcher.IdempotencyCached, bool) {
	if a.enter != nil {
		a.enter()
	}
	c, ok := a.inner.Lookup(ctx, key, identity)
	if !ok {
		return nil, false
	}
	return &dispatcher.IdempotencyCached{Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL}, true
}

func (a *idemStore) Store(ctx context.Context, key, identity string, c dispatcher.IdempotencyCached) error {
	a.record(c)
	return a.inner.Store(ctx, key, identity, idempotency.Cached{
		Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL,
	})
}

func (a *claimingIdemStore) Claim(ctx context.Context, key, identity string) (dispatcher.IdempotencyClaim, error) {
	if a.enter != nil {
		a.enter()
	}
	claim, err := a.claimer.Claim(ctx, key, identity)
	if errors.Is(err, idempotency.ErrClaimHeld) {
		return dispatcher.IdempotencyClaim{}, fmt.Errorf("%w: %w", dispatcher.ErrIdempotencyClaimHeld, err)
	}
	if err != nil {
		return dispatcher.IdempotencyClaim{}, err
	}

	var out dispatcher.IdempotencyClaim
	if c := claim.Cached; c != nil {
		out.Cached = &dispatcher.IdempotencyCached{Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL}
	}
	if end := claim.End; end != nil {
		out.End = func(ctx context.Context, c *dispatcher.IdempotencyCached) error {
			var err error
			if c == nil {
				err = end(ctx, nil)
			} else {
				a.record(*c)
				err = end(ctx, &idempotency.Cached{Status: c.Status, WireBody: c.WireBody, StoredAt: c.StoredAt, TTL: c.TTL})
			}
			if errors.Is(err, idempotency.ErrClaimLost) {
				return fmt.Errorf("%w: %w", dispatcher.ErrIdempotencyClaimLost, err)
			}
			return err
		}
	}
	return out, nil
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

func idemDispatcher(t *testing.T, deps Deps, opts ...dispatcher.Option) (*dispatcher.Dispatcher, *idemStore) {
	t.Helper()
	s := &idemStore{inner: idempotency.NewInMemoryStore()}
	opts = append([]dispatcher.Option{dispatcher.WithIdempotencyStore(adaptIdem(s))}, opts...)
	d := dispatcher.NewWithOptions(nil, opts...)
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

// blockingKeys holds the first key Create until release, so a test can keep
// one keys.create inside its handler while a second one arrives.
type blockingKeys struct {
	key.Store

	once     sync.Once
	entered  chan struct{}
	release  chan struct{}
	released sync.Once
}

func newBlockingKeys(t *testing.T, inner key.Store) *blockingKeys {
	b := &blockingKeys{Store: inner, entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(b.unblock)
	return b
}

func (b *blockingKeys) Create(ctx context.Context, k *key.Key) error {
	first := false
	b.once.Do(func() { first = true })
	if first {
		close(b.entered)
		<-b.release
	}
	return b.Store.Create(ctx, k)
}

func (b *blockingKeys) unblock() { b.released.Do(func() { close(b.release) }) }

type blockingStore struct {
	store.Store
	keys *blockingKeys
}

func (w blockingStore) Keys() key.Store { return w.keys }

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

type idemResult struct {
	data json.RawMessage
	err  error
}

func dispatchAsync(d *dispatcher.Dispatcher, req dashcontract.Request) <-chan idemResult {
	out := make(chan idemResult, 1)
	go func() {
		data, _, err := d.Dispatch(context.Background(), req, principal())
		out <- idemResult{data, err}
	}()
	return out
}

func receive(t *testing.T, ch <-chan idemResult, what string) idemResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return idemResult{}
	}
}

// Two overlapping keys.create dispatches with one idempotency key and user
// run the handler once. The first holds forge's claim while its key write is
// held back; the second reaches the store while the first is still inside
// the handler, and only then does the first go on. The second answers
// CONFLICT, either still running or already ran, depending on whether it
// looks before or after the first ends its claim, and one key exists.
func TestKeysCreateOverlapRunsTheHandlerOnce(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		keys := newBlockingKeys(t, s.Keys())
		deps, _ := setup(t, blockingStore{Store: s, keys: keys})
		d, idem := idemDispatcher(t, deps)

		second := make(chan struct{})
		var calls atomic.Int32
		idem.enter = func() {
			if calls.Add(1) == 2 {
				close(second)
			}
		}
		req := idemCommand(t, "keys.create", "idem-overlap-1", validCreate())

		firstDone := dispatchAsync(d, req)
		waitFor(t, keys.entered, "the first keys.create to reach its key write")
		secondDone := dispatchAsync(d, req)
		waitFor(t, second, "the second keys.create to reach the idempotency store")
		keys.unblock()

		r1 := receive(t, firstDone, "the first keys.create")
		require.NoError(t, r1.err)
		var first keyWithSecretResponse
		require.NoError(t, json.Unmarshal(r1.data, &first))
		require.NotEmpty(t, first.RawKey)

		r2 := receive(t, secondDone, "the second keys.create")
		require.Error(t, r2.err, "the overlapping keys.create ran its handler too")
		assert.Equal(t, dashcontract.CodeConflict, codeOf(t, r2.err))
		assert.NotContains(t, r2.err.Error(), first.RawKey)
		assert.Empty(t, r2.data)

		total, err := s.Keys().Count(context.Background(), &createTenantFilter)
		require.NoError(t, err)
		assert.EqualValues(t, 1, total, "two overlapping dispatches minted two keys")

		c := idem.entry(t, "idem-overlap-1", "keys.create")
		assert.Equal(t, dispatcher.TombstoneStatus, c.Status)
		assert.Empty(t, c.WireBody)
		idem.assertNeverHolds(t, first.RawKey)
	})
}

// With the first keys.create still inside its handler, a second one with the
// same key and user waits out the dispatcher's claim wait and answers a
// retryable CONFLICT that says the command is still running. The handler ran
// once.
func TestKeysCreateOverlapAnswersStillRunning(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		keys := newBlockingKeys(t, s.Keys())
		deps, _ := setup(t, blockingStore{Store: s, keys: keys})
		d, _ := idemDispatcher(t, deps, dispatcher.WithIdempotencyWait(50*time.Millisecond))
		req := idemCommand(t, "keys.create", "idem-overlap-2", validCreate())

		firstDone := dispatchAsync(d, req)
		waitFor(t, keys.entered, "the first keys.create to reach its key write")

		// The first is held, so this returns only once the wait runs out.
		data, _, err := d.Dispatch(context.Background(), req, principal())
		require.Error(t, err, "the overlapping keys.create ran its handler too")
		assert.Equal(t, dashcontract.CodeConflict, codeOf(t, err))
		var ce *dashcontract.Error
		require.ErrorAs(t, err, &ce)
		assert.True(t, ce.Retryable)
		assert.Contains(t, ce.Message, "still running")
		assert.Empty(t, data)

		keys.unblock()
		r1 := receive(t, firstDone, "the first keys.create")
		require.NoError(t, r1.err)

		total, err := s.Keys().Count(context.Background(), &createTenantFilter)
		require.NoError(t, err)
		assert.EqualValues(t, 1, total)
	})
}
