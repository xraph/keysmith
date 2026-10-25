package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/extensions/dashboard"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"

	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store"
)

// recordingStore wraps forge's in-memory idempotency store and keeps a copy
// of every entry written to it, so a test can read what was stored. It sits
// under dashboard.AdaptIdempotencyStore, the adapter forge's dashboard
// extension wires its own dispatcher with, so these tests run the host's
// wiring and not a copy of it. It is a Claimer like the store it wraps, so
// the adapter hands the dispatcher a claimer and the claim path runs.
type recordingStore struct {
	inner *idempotency.InMemoryStore

	// enter, when set, runs at the start of every Lookup and Claim the
	// dispatcher makes, so a test can tell when a dispatch reached the store.
	enter func()

	mu     sync.Mutex
	stored []idempotency.Cached
}

var _ idempotency.Claimer = (*recordingStore)(nil)

func (s *recordingStore) record(c idempotency.Cached) {
	s.mu.Lock()
	s.stored = append(s.stored, c)
	s.mu.Unlock()
}

func (s *recordingStore) Lookup(ctx context.Context, key, identity string) (*idempotency.Cached, bool) {
	if s.enter != nil {
		s.enter()
	}
	return s.inner.Lookup(ctx, key, identity)
}

func (s *recordingStore) Store(ctx context.Context, key, identity string, c idempotency.Cached) error {
	s.record(c)
	return s.inner.Store(ctx, key, identity, c)
}

func (s *recordingStore) Claim(ctx context.Context, key, identity string) (idempotency.Claim, error) {
	if s.enter != nil {
		s.enter()
	}
	claim, err := s.inner.Claim(ctx, key, identity)
	if err != nil || claim.End == nil {
		return claim, err
	}
	end := claim.End
	claim.End = func(ctx context.Context, c *idempotency.Cached) error {
		if c != nil {
			s.record(*c)
		}
		return end(ctx, c)
	}
	return claim, nil
}

// entry reads back what the real store holds for key under the dispatcher's
// identity for principal() and intent.
func (s *recordingStore) entry(t *testing.T, key, intent string) *idempotency.Cached {
	t.Helper()
	// "user_1:"+intent mirrors forge's principalIdentity (dispatcher.go):
	// the principal's subject, a colon, then the intent.
	c, ok := s.inner.Lookup(context.Background(), key, "user_1:"+intent)
	require.True(t, ok, "no idempotency entry for %s", intent)
	return c
}

// assertNeverHolds fails if the dispatcher stored nothing, or if any entry it
// stored carries raw.
func (s *recordingStore) assertNeverHolds(t *testing.T, raw string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.stored, "the dispatcher stored no idempotency entry")
	for i, c := range s.stored {
		assert.False(t, bytes.Contains(c.WireBody, []byte(raw)), "stored entry %d holds the raw key", i)
	}
}

func idemDispatcher(t *testing.T, deps Deps, opts ...dispatcher.Option) (*dispatcher.Dispatcher, *recordingStore) {
	t.Helper()
	s := &recordingStore{inner: idempotency.NewInMemoryStore()}
	adapted := dashboard.AdaptIdempotencyStore(s)
	_, claims := adapted.(dispatcher.IdempotencyClaimer)
	require.True(t, claims, "the adapter must hand the dispatcher a claimer")
	opts = append([]dispatcher.Option{dispatcher.WithIdempotencyStore(adapted)}, opts...)
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
	var ce *dashcontract.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, dispatcher.ReasonAlreadyRan, ce.Details[dispatcher.ReasonDetail])
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

		// Forge keeps an ordinary success inside its bound replay envelope,
		// with the full response next to the binding digest. Only a secret
		// command leaves a bodyless tombstone.
		c := idem.entry(t, "idem-scope-1", "scopes.create")
		var env struct {
			Format   string          `json:"format"`
			Response json.RawMessage `json:"response"`
		}
		require.NoError(t, json.Unmarshal(c.WireBody, &env))
		assert.Equal(t, "forge.dashboard.idempotency", env.Format)
		assert.NotEmpty(t, env.Response)
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
		var ce *dashcontract.Error
		require.ErrorAs(t, r2.err, &ce)
		assert.Contains(t, []any{dispatcher.ReasonStillRunning, dispatcher.ReasonAlreadyRan}, ce.Details[dispatcher.ReasonDetail])
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
		assert.Equal(t, dispatcher.ReasonStillRunning, ce.Details[dispatcher.ReasonDetail])
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
