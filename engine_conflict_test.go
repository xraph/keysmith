package keysmith_test

import (
	"context"
	"errors"
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
	"github.com/xraph/keysmith/store/memory"
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

		// The refused rotation's record stays as history, with its window
		// ended. Its new hash belongs to no key and its raw key was never
		// handed out.
		recs, err := plain.ListRotations(ctx, &rotation.ListFilter{KeyID: &orig.Key.ID})
		require.NoError(t, err)
		require.Len(t, recs, 1)
		assert.False(t, recs[0].GraceEnds.After(time.Now()), "the refused rotation's window is ended")
	})
}

// A compromise rotation with no grace that wins against a manual one must
// still kill the old key. The loser recorded a 24h window on the same old
// hash before its write was refused; left open, that window would keep the
// compromised key validating for a day.
func TestCompromiseRotationThatWinsARaceKeepsTheOldKeyDead(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		plain, ctx := newEngine(t, s)
		orig := mustCreate(t, plain, ctx, nil)
		var winner *key.CreateResult
		eng := racedEngine(t, s, func(other *keysmith.Engine, ctx context.Context) error {
			var err error
			winner, err = other.RotateKey(ctx, orig.Key.ID, rotation.ReasonCompromise, keysmith.WithGrace(0))
			return err
		})

		_, err := eng.RotateKey(ctx, orig.Key.ID, rotation.ReasonManual, keysmith.WithGrace(24*time.Hour))
		require.ErrorIs(t, err, keysmith.ErrKeyConflict)

		_, err = plain.ValidateKey(ctx, orig.RawKey)
		require.ErrorIs(t, err, keysmith.ErrInvalidKey, "the compromised key stays dead")
		vr, err := plain.ValidateKey(ctx, winner.RawKey)
		require.NoError(t, err, "the winner's key validates")
		assert.False(t, vr.ViaPreviousKey)

		recs, err := plain.ListRotations(ctx, &rotation.ListFilter{KeyID: &orig.Key.ID})
		require.NoError(t, err)
		assert.Len(t, recs, 2, "both records stay as history")
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

// failingEndGrace wraps a rotation store so EndGraceByID fails.
type failingEndGrace struct {
	rotation.Store
	err error
}

func (f failingEndGrace) EndGraceByID(context.Context, id.RotationID, time.Time) error { return f.err }

type endGraceStore struct {
	failingStore
	rotations rotation.Store
}

func (s endGraceStore) Rotations() rotation.Store { return s.rotations }

// When ending the refused rotation's window fails too, the caller hears
// about both. Memory only: this is fault injection, not a backend property.
func TestRotateReportsTheConflictAndAFailedCleanup(t *testing.T) {
	s := memory.New()
	plain, ctx := newEngine(t, s)
	orig := mustCreate(t, plain, ctx, nil)
	endErr := errors.New("end grace boom")
	once := &sync.Once{}
	eng, _ := newEngine(t, endGraceStore{
		failingStore: failingStore{Store: s, keys: keysAfterGet{Store: s.Keys(), once: once, hook: func() {
			_, err := plain.RotateKey(ctx, orig.Key.ID, rotation.ReasonCompromise, keysmith.WithGrace(0))
			require.NoError(t, err)
		}}},
		rotations: failingEndGrace{Store: s.Rotations(), err: endErr},
	})

	_, err := eng.RotateKey(ctx, orig.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
	require.ErrorIs(t, err, keysmith.ErrKeyConflict)
	require.ErrorIs(t, err, endErr)
}
