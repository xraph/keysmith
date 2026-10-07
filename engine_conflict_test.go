package keysmith_test

import (
	"context"
	"sync"
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
)

// keysAfterGet wraps a key store so the first Get runs hook once it has read
// the row. Whatever hook writes lands between the caller's read and its write.
type keysAfterGet struct {
	key.Store
	once *sync.Once
	hook func()
}

func (k keysAfterGet) Get(ctx context.Context, keyID id.KeyID) (*key.Key, error) {
	got, err := k.Store.Get(ctx, keyID)
	k.once.Do(k.hook)
	return got, err
}

// racedEngine returns an engine whose first key read lets other write the
// key before the read returns. other works on s directly, so its own reads
// are not intercepted.
func racedEngine(t *testing.T, s store.Store, race func(other *keysmith.Engine, ctx context.Context) error) *keysmith.Engine {
	t.Helper()
	other, ctx := newEngine(t, s)
	eng, _ := newEngine(t, failingStore{Store: s, keys: keysAfterGet{
		Store: s.Keys(),
		once:  &sync.Once{},
		hook:  func() { require.NoError(t, race(other, ctx)) },
	}})
	return eng
}

// A revoke that lands between a rotation's read and its write used to lose:
// the rotation wrote its older row back with the state it had read, and the
// revoked key came back to life under a new raw key.
func TestRotateThatLostARaceToARevokeIsRefused(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		plain, ctx := newEngine(t, s)
		orig := mustCreate(t, plain, ctx, nil)
		eng := racedEngine(t, s, func(other *keysmith.Engine, ctx context.Context) error {
			return other.RevokeKey(ctx, orig.Key.ID, "compromised")
		})

		res, err := eng.RotateKey(ctx, orig.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
		require.ErrorIs(t, err, keysmith.ErrKeyConflict)
		assert.Nil(t, res, "no raw key leaves a refused rotation")

		k, err := plain.GetKey(ctx, orig.Key.ID)
		require.NoError(t, err)
		assert.Equal(t, key.StateRevoked, k.State)
		assert.NotNil(t, k.RevokedAt)
		assert.Equal(t, orig.Key.KeyHash, k.KeyHash, "the rotation's hash never reached the key")
		_, err = plain.ValidateKey(ctx, orig.RawKey)
		require.ErrorIs(t, err, keysmith.ErrKeyInactive)

		// rotation.Store cannot delete one record, so the rotation's record
		// stays. Its new hash belongs to no key and its raw key was never
		// handed out, and the key it names is revoked.
		recs, err := plain.ListRotations(ctx, &rotation.ListFilter{KeyID: &orig.Key.ID})
		require.NoError(t, err)
		assert.Len(t, recs, 1)
	})
}

func TestSuspendAndReactivateThatLostARaceToARevokeAreRefused(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		plain, ctx := newEngine(t, s)
		revoke := func(kid id.KeyID) func(*keysmith.Engine, context.Context) error {
			return func(other *keysmith.Engine, ctx context.Context) error {
				return other.RevokeKey(ctx, kid, "gone")
			}
		}

		active := mustCreate(t, plain, ctx, nil)
		err := racedEngine(t, s, revoke(active.Key.ID)).SuspendKey(ctx, active.Key.ID)
		require.ErrorIs(t, err, keysmith.ErrKeyConflict)

		suspended := mustCreate(t, plain, ctx, nil)
		require.NoError(t, plain.SuspendKey(ctx, suspended.Key.ID))
		err = racedEngine(t, s, revoke(suspended.Key.ID)).ReactivateKey(ctx, suspended.Key.ID)
		require.ErrorIs(t, err, keysmith.ErrKeyConflict)

		for _, kid := range []id.KeyID{active.Key.ID, suspended.Key.ID} {
			k, gErr := plain.GetKey(ctx, kid)
			require.NoError(t, gErr)
			assert.Equal(t, key.StateRevoked, k.State)
		}
	})
}

// The other way round: a revoke that read the key before a rotation wrote it
// is refused too, rather than writing the old hash back over the new one.
// The caller reads again and the second revoke goes through.
func TestRevokeThatLostARaceToARotationIsRefused(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		plain, ctx := newEngine(t, s)
		orig := mustCreate(t, plain, ctx, nil)
		var rotated *key.CreateResult
		eng := racedEngine(t, s, func(other *keysmith.Engine, ctx context.Context) error {
			var err error
			rotated, err = other.RotateKey(ctx, orig.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
			return err
		})

		require.ErrorIs(t, eng.RevokeKey(ctx, orig.Key.ID, "gone"), keysmith.ErrKeyConflict)
		_, err := plain.ValidateKey(ctx, rotated.RawKey)
		require.NoError(t, err, "the rotation stands")

		require.NoError(t, eng.RevokeKey(ctx, orig.Key.ID, "gone"))
		_, err = plain.ValidateKey(ctx, rotated.RawKey)
		require.ErrorIs(t, err, keysmith.ErrKeyInactive)
		_, err = plain.ValidateKey(ctx, orig.RawKey)
		require.ErrorIs(t, err, keysmith.ErrInvalidKey, "the revoke closed the previous key's window")
	})
}
