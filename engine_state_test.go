package keysmith_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

func newEngine(t *testing.T, s store.Store, opts ...keysmith.Option) (*keysmith.Engine, context.Context) { //nolint:unparam // opts is used by later tests
	t.Helper()
	eng, err := keysmith.NewEngine(append([]keysmith.Option{keysmith.WithStore(s)}, opts...)...)
	require.NoError(t, err)
	return eng, keysmith.WithTenant(context.Background(), "app", "t1")
}

func mustCreate(t *testing.T, eng *keysmith.Engine, ctx context.Context, in *keysmith.CreateKeyInput) *key.CreateResult { //nolint:revive // the signature is shared by later tests
	t.Helper()
	if in == nil {
		in = &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive}
	}
	r, err := eng.CreateKey(ctx, in)
	require.NoError(t, err)
	return r
}

func TestRevokedKeyCannotBeResurrected(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		r := mustCreate(t, eng, ctx, nil)
		require.NoError(t, eng.RevokeKey(ctx, r.Key.ID, "gone"))

		assert.ErrorIs(t, eng.SuspendKey(ctx, r.Key.ID), keysmith.ErrInvalidStateTransition)
		assert.ErrorIs(t, eng.ReactivateKey(ctx, r.Key.ID), keysmith.ErrInvalidStateTransition)
		_, err := eng.ValidateKey(ctx, r.RawKey)
		assert.ErrorIs(t, err, keysmith.ErrKeyInactive)

		k, err := eng.GetKey(ctx, r.Key.ID)
		require.NoError(t, err)
		assert.Equal(t, key.StateRevoked, k.State)
	})
}

func TestRevokeTwiceIsRefused(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		r := mustCreate(t, eng, ctx, nil)
		require.NoError(t, eng.RevokeKey(ctx, r.Key.ID, "gone"))
		assert.ErrorIs(t, eng.RevokeKey(ctx, r.Key.ID, "again"), keysmith.ErrInvalidStateTransition)
	})
}

func TestSuspendOnlyFromActive(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		r := mustCreate(t, eng, ctx, nil)
		require.NoError(t, eng.SuspendKey(ctx, r.Key.ID))
		assert.ErrorIs(t, eng.SuspendKey(ctx, r.Key.ID), keysmith.ErrInvalidStateTransition)
		require.NoError(t, eng.ReactivateKey(ctx, r.Key.ID))
		_, err := eng.ValidateKey(ctx, r.RawKey)
		assert.NoError(t, err)
	})
}

func TestStateChangesOnMissingKeyAreNotFound(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		assert.ErrorIs(t, eng.SuspendKey(ctx, id.NewKeyID()), keysmith.ErrKeyNotFound)
		assert.ErrorIs(t, eng.RevokeKey(ctx, id.NewKeyID(), "x"), keysmith.ErrKeyNotFound)
	})
}

type allowAll struct{}

func (allowAll) Allow(context.Context, string, int, time.Duration) (bool, error) { return true, nil }
func (allowAll) Remaining(context.Context, string, int, time.Duration) (int, error) {
	return 0, nil
}

func TestRateLimiterConfigured(t *testing.T) {
	without, err := keysmith.NewEngine(keysmith.WithStore(memory.New()))
	require.NoError(t, err)
	assert.False(t, without.RateLimiterConfigured())
	with, err := keysmith.NewEngine(keysmith.WithStore(memory.New()), keysmith.WithRateLimiter(allowAll{}))
	require.NoError(t, err)
	assert.True(t, with.RateLimiterConfigured())
}
