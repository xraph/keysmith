package keysmith_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
)

func TestOldKeyValidatesInsideTheWindow(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		orig := mustCreate(t, eng, ctx, nil)
		rot, err := eng.RotateKey(ctx, orig.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour), keysmith.WithRotatedBy("user_1"))
		require.NoError(t, err)

		vr, err := eng.ValidateKey(ctx, orig.RawKey)
		require.NoError(t, err)
		assert.True(t, vr.ViaPreviousKey)
		require.NotNil(t, vr.GraceEnds)
		assert.WithinDuration(t, time.Now().Add(time.Hour), *vr.GraceEnds, 5*time.Second)
		assert.Equal(t, orig.Key.ID.String(), vr.Key.ID.String())

		vr, err = eng.ValidateKey(ctx, rot.RawKey)
		require.NoError(t, err)
		assert.False(t, vr.ViaPreviousKey)

		recs, err := eng.ListRotations(ctx, &rotation.ListFilter{KeyID: &orig.Key.ID})
		require.NoError(t, err)
		require.Len(t, recs, 1)
		assert.Equal(t, orig.RawKey[len(orig.RawKey)-4:], recs[0].OldHint)
		assert.Equal(t, rot.RawKey[len(rot.RawKey)-4:], recs[0].NewHint)
		assert.Equal(t, "user_1", recs[0].RotatedBy)
	})
}

func TestZeroGraceKillsTheOldKeyAtOnce(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		orig := mustCreate(t, eng, ctx, nil)
		_, err := eng.RotateKey(ctx, orig.Key.ID, rotation.ReasonCompromise, keysmith.WithGrace(0))
		require.NoError(t, err)
		_, err = eng.ValidateKey(ctx, orig.RawKey)
		assert.ErrorIs(t, err, keysmith.ErrInvalidKey)
	})
}

func TestDefaultGraceComesFromPolicyThen24h(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		pol := &policy.Policy{Name: "p", GracePeriod: 2 * time.Hour}
		require.NoError(t, eng.CreatePolicy(ctx, pol))
		withPol := mustCreate(t, eng, ctx, &keysmith.CreateKeyInput{Name: "a", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID})
		bare := mustCreate(t, eng, ctx, nil)
		_, err := eng.RotateKey(ctx, withPol.Key.ID, rotation.ReasonManual)
		require.NoError(t, err)
		_, err = eng.RotateKey(ctx, bare.Key.ID, rotation.ReasonManual)
		require.NoError(t, err)

		a, _ := eng.ListRotations(ctx, &rotation.ListFilter{KeyID: &withPol.Key.ID})
		b, _ := eng.ListRotations(ctx, &rotation.ListFilter{KeyID: &bare.Key.ID})
		assert.Equal(t, 2*time.Hour, a[0].GraceTTL)
		assert.Equal(t, 24*time.Hour, b[0].GraceTTL)
	})
}

func TestTwoRotationsInOneWindowKeepBothPreviousKeys(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		k0 := mustCreate(t, eng, ctx, nil)
		k1, err := eng.RotateKey(ctx, k0.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
		require.NoError(t, err)
		k2, err := eng.RotateKey(ctx, k0.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
		require.NoError(t, err)
		for _, raw := range []string{k0.RawKey, k1.RawKey, k2.RawKey} {
			_, vErr := eng.ValidateKey(ctx, raw)
			assert.NoError(t, vErr)
		}
		n, err := eng.EndGrace(ctx, k0.Key.ID)
		require.NoError(t, err)
		assert.EqualValues(t, 2, n)
		for _, raw := range []string{k0.RawKey, k1.RawKey} {
			_, vErr := eng.ValidateKey(ctx, raw)
			assert.ErrorIs(t, vErr, keysmith.ErrInvalidKey)
		}
		_, err = eng.ValidateKey(ctx, k2.RawKey)
		assert.NoError(t, err)
	})
}

func TestRevokeEndsTheWindowAndPreviousKeyStaysDead(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		orig := mustCreate(t, eng, ctx, nil)
		_, err := eng.RotateKey(ctx, orig.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
		require.NoError(t, err)
		require.NoError(t, eng.RevokeKey(ctx, orig.Key.ID, "gone"))
		_, err = eng.ValidateKey(ctx, orig.RawKey)
		assert.Error(t, err)
		recs, _ := eng.ListRotations(ctx, &rotation.ListFilter{KeyID: &orig.Key.ID})
		assert.False(t, recs[0].GraceEnds.After(time.Now()), "revoke must close the window")
	})
}

func TestSuspendedKeyFailsThroughItsPreviousHashToo(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		orig := mustCreate(t, eng, ctx, nil)
		_, err := eng.RotateKey(ctx, orig.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
		require.NoError(t, err)
		require.NoError(t, eng.SuspendKey(ctx, orig.Key.ID))
		_, err = eng.ValidateKey(ctx, orig.RawKey)
		assert.ErrorIs(t, err, keysmith.ErrKeyInactive)
	})
}

func TestRotateRefusesRevokedAndExpiredKeys(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		revoked := mustCreate(t, eng, ctx, nil)
		require.NoError(t, eng.RevokeKey(ctx, revoked.Key.ID, "x"))
		_, err := eng.RotateKey(ctx, revoked.Key.ID, rotation.ReasonManual)
		assert.ErrorIs(t, err, keysmith.ErrInvalidStateTransition)

		past := time.Now().Add(-time.Minute)
		expired := mustCreate(t, eng, ctx, &keysmith.CreateKeyInput{Name: "e", Prefix: "sk", Environment: key.EnvLive, ExpiresAt: &past})
		_, err = eng.RotateKey(ctx, expired.Key.ID, rotation.ReasonManual)
		assert.ErrorIs(t, err, keysmith.ErrInvalidStateTransition)
	})
}
