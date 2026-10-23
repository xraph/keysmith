package keysmith_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

var errDBDown = errors.New("db down")

// outageKeys fails the lookups ValidateKey makes. A nil error field leaves
// that lookup alone. getByHashFn, when set, wins over getByHashErr.
type outageKeys struct {
	key.Store
	getByHashErr error
	getByHashFn  func(ctx context.Context) error
	getErr       error
}

func (k outageKeys) GetByHash(ctx context.Context, hash string) (*key.Key, error) {
	if k.getByHashFn != nil {
		if err := k.getByHashFn(ctx); err != nil {
			return nil, err
		}
	}
	if k.getByHashErr != nil {
		return nil, k.getByHashErr
	}
	return k.Store.GetByHash(ctx, hash)
}

func (k outageKeys) Get(ctx context.Context, kid id.KeyID) (*key.Key, error) {
	if k.getErr != nil {
		return nil, k.getErr
	}
	return k.Store.Get(ctx, kid)
}

type outageRotations struct {
	rotation.Store
	graceErr error
}

func (r outageRotations) GetInGraceByOldHash(ctx context.Context, hash string, now time.Time) (*rotation.Record, error) {
	if r.graceErr != nil {
		return nil, r.graceErr
	}
	return r.Store.GetInGraceByOldHash(ctx, hash, now)
}

// outageStore wraps a real store and swaps in the faulty accessors.
type outageStore struct {
	store.Store
	keys      key.Store
	rotations rotation.Store
}

func (s outageStore) Keys() key.Store {
	if s.keys != nil {
		return s.keys
	}
	return s.Store.Keys()
}

func (s outageStore) Rotations() rotation.Store {
	if s.rotations != nil {
		return s.rotations
	}
	return s.Store.Rotations()
}

type failureCounter struct{ n atomic.Int64 }

func (*failureCounter) Name() string { return "failure-counter" }

func (f *failureCounter) OnKeyValidationFailed(context.Context, string, error) error {
	f.n.Add(1)
	return nil
}

// A key store that cannot answer is not a key that does not exist. The
// caller has to be able to tell the two apart: an MQTT broker that reads
// "invalid key" during an outage tells every device to stop retrying.
func TestValidateKeyReportsStoreOutage(t *testing.T) {
	s := memory.New()
	eng, ctx := newEngine(t, s)
	r := mustCreate(t, eng, ctx, nil)

	hooks := &failureCounter{}
	broken, _ := newEngine(t, outageStore{Store: s, keys: outageKeys{Store: s.Keys(), getByHashErr: errDBDown}}, keysmith.WithExtension(hooks))

	vr, err := broken.ValidateKey(ctx, r.RawKey)
	require.Error(t, err)
	assert.Nil(t, vr)
	assert.NotErrorIs(t, err, keysmith.ErrInvalidKey, "an outage is not an invalid key")
	assert.ErrorIs(t, err, errDBDown, "the store error stays reachable")
	assert.Zero(t, hooks.n.Load(), "an outage is not a failed validation attempt")
}

// Every backend's miss has to read as not-found, or a bad key would now
// come back as an outage.
func TestValidateKeyUnknownKeyIsInvalidOnEveryBackend(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		_ = mustCreate(t, eng, ctx, nil)

		_, err := eng.ValidateKey(ctx, "sk_live_unknownunknownunknown")
		assert.ErrorIs(t, err, keysmith.ErrInvalidKey)
	})
}

func TestValidateKeyNotFoundFromStoreIsInvalid(t *testing.T) {
	s := memory.New()
	hooks := &failureCounter{}
	eng, ctx := newEngine(t, outageStore{Store: s, keys: outageKeys{Store: s.Keys(), getByHashErr: store.ErrKeyNotFound}}, keysmith.WithExtension(hooks))

	_, err := eng.ValidateKey(ctx, "sk_live_whatever")
	assert.ErrorIs(t, err, keysmith.ErrInvalidKey)
	assert.EqualValues(t, 1, hooks.n.Load(), "a key that does not exist is a failed attempt")

	// A backend that wraps its miss still counts.
	wrapped := outageStore{Store: s, keys: outageKeys{Store: s.Keys(), getByHashErr: errors.Join(errors.New("keysmith/x: get key by hash"), store.ErrKeyNotFound)}}
	eng2, _ := newEngine(t, wrapped)
	_, err = eng2.ValidateKey(ctx, "sk_live_whatever")
	assert.ErrorIs(t, err, keysmith.ErrInvalidKey)
}

// The grace lookup runs when the hash is no key's current hash. If that
// lookup cannot answer, the engine does not know the key is unknown.
func TestValidateKeyReportsGraceLookupOutage(t *testing.T) {
	s := memory.New()
	eng, ctx := newEngine(t, s)
	r := mustCreate(t, eng, ctx, nil)
	rot, err := eng.RotateKey(ctx, r.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, rot.RawKey)

	t.Run("rotation lookup fails", func(t *testing.T) {
		hooks := &failureCounter{}
		broken, _ := newEngine(t, outageStore{Store: s, rotations: outageRotations{Store: s.Rotations(), graceErr: errDBDown}}, keysmith.WithExtension(hooks))

		// The old raw key's hash is no longer current, so the fallback runs.
		_, err := broken.ValidateKey(ctx, r.RawKey)
		require.Error(t, err)
		assert.NotErrorIs(t, err, keysmith.ErrInvalidKey)
		assert.ErrorIs(t, err, errDBDown)
		assert.Zero(t, hooks.n.Load())
	})

	t.Run("rotation record missing is invalid", func(t *testing.T) {
		eng2, _ := newEngine(t, outageStore{Store: s, rotations: outageRotations{Store: s.Rotations(), graceErr: store.ErrRotationNotFound}})
		_, err := eng2.ValidateKey(ctx, r.RawKey)
		assert.ErrorIs(t, err, keysmith.ErrInvalidKey)
	})

	t.Run("loading the rotated key fails", func(t *testing.T) {
		broken, _ := newEngine(t, outageStore{Store: s, keys: outageKeys{Store: s.Keys(), getErr: errDBDown}})
		_, err := broken.ValidateKey(ctx, r.RawKey)
		require.Error(t, err)
		assert.NotErrorIs(t, err, keysmith.ErrInvalidKey)
		assert.ErrorIs(t, err, errDBDown)
	})

	t.Run("rotated key deleted is invalid", func(t *testing.T) {
		eng2, _ := newEngine(t, outageStore{Store: s, keys: outageKeys{Store: s.Keys(), getErr: store.ErrKeyNotFound}})
		_, err := eng2.ValidateKey(ctx, r.RawKey)
		assert.ErrorIs(t, err, keysmith.ErrInvalidKey)
	})
}

// A cancelled or timed-out request is the caller's doing. It surfaces as the
// context error, whether the store reports it, reports a miss, or does not
// look at the context at all.
func TestValidateKeyCancelledContext(t *testing.T) {
	s := memory.New()
	eng, ctx := newEngine(t, s)
	r := mustCreate(t, eng, ctx, nil)

	t.Run("store reports the cancellation", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		broken, _ := newEngine(t, outageStore{Store: s, keys: outageKeys{Store: s.Keys(), getByHashFn: func(c context.Context) error { return c.Err() }}})
		_, err := broken.ValidateKey(cctx, r.RawKey)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, keysmith.ErrInvalidKey)
	})

	t.Run("store reports a miss after the cancellation", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := eng.ValidateKey(cctx, "sk_live_unknownunknownunknown")
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, keysmith.ErrInvalidKey)
		assert.NotErrorIs(t, err, keysmith.ErrKeyNotFound, "a miss seen under a cancelled context is not a not-found verdict")
		assert.NotErrorIs(t, err, keysmith.ErrRotationNotFound)
	})

	t.Run("deadline", func(t *testing.T) {
		dctx, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
		defer cancel()
		_, err := eng.ValidateKey(dctx, "sk_live_unknownunknownunknown")
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.NotErrorIs(t, err, keysmith.ErrInvalidKey)
	})
}
