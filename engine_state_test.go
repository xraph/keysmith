package keysmith_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
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

// A rotate or suspend racing a revoke can write an older row back over the
// revoked one. Until updates are conditional, revoked_at is the authority:
// once it is set, the key is dead whatever state says.
func TestRevokedAtWinsOverAStaleState(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		r := mustCreate(t, eng, ctx, nil)
		require.NoError(t, eng.RevokeKey(ctx, r.Key.ID, "gone"))
		// Simulate a stale write that put state back but left revoked_at.
		require.NoError(t, s.Keys().UpdateState(ctx, r.Key.ID, key.StateActive))
		k, err := s.Keys().Get(ctx, r.Key.ID)
		require.NoError(t, err)
		require.Equal(t, key.StateActive, k.State)
		require.NotNil(t, k.RevokedAt)

		_, err = eng.ValidateKey(ctx, r.RawKey)
		assert.ErrorIs(t, err, keysmith.ErrKeyInactive)
		assert.ErrorIs(t, eng.SuspendKey(ctx, r.Key.ID), keysmith.ErrInvalidStateTransition)
		assert.ErrorIs(t, eng.ReactivateKey(ctx, r.Key.ID), keysmith.ErrInvalidStateTransition)
		_, err = eng.RotateKey(ctx, r.Key.ID, rotation.ReasonManual)
		assert.ErrorIs(t, err, keysmith.ErrInvalidStateTransition)
	})
}

// MaxKeyLifetime is a cap, not only a default: an explicit expiry beyond it
// is refused before anything is written.
func TestMaxKeyLifetimeCapsAnExplicitExpiry(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		pol := &policy.Policy{Name: "short", MaxKeyLifetime: 24 * time.Hour}
		require.NoError(t, eng.CreatePolicy(ctx, pol))
		in := func(name string, exp *time.Time) *keysmith.CreateKeyInput {
			return &keysmith.CreateKeyInput{Name: name, Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID, ExpiresAt: exp}
		}

		tooFar := time.Now().Add(48 * time.Hour)
		_, err := eng.CreateKey(ctx, in("far", &tooFar))
		require.ErrorIs(t, err, keysmith.ErrKeyLifetimeExceeded)
		keys, err := eng.ListKeys(ctx, &key.ListFilter{PolicyID: &pol.ID})
		require.NoError(t, err)
		assert.Empty(t, keys, "a refused create writes no key")

		near := time.Now().Add(12 * time.Hour)
		r, err := eng.CreateKey(ctx, in("near", &near))
		require.NoError(t, err)
		require.NotNil(t, r.Key.ExpiresAt)
		assert.WithinDuration(t, near, *r.Key.ExpiresAt, time.Second)

		r, err = eng.CreateKey(ctx, in("default", nil))
		require.NoError(t, err)
		require.NotNil(t, r.Key.ExpiresAt)
		assert.WithinDuration(t, time.Now().Add(24*time.Hour), *r.Key.ExpiresAt, 5*time.Second)
	})
}

// failingPolicies fails every policy read with an error that is not
// not-found, the way a dropped connection would.
type failingPolicies struct{ policy.Store }

func (failingPolicies) Get(context.Context, id.PolicyID) (*policy.Policy, error) {
	return nil, errors.New("injected: policy store unavailable")
}

// failPolicyGet is a store whose policy reads fail.
type failPolicyGet struct{ store.Store }

func (s failPolicyGet) Policies() policy.Store { return failingPolicies{s.Store.Policies()} }

// A key's policy carries its restrictions. If it cannot be read, validation
// and rotation must fail rather than carry on as if there were none. A
// policy that is simply gone keeps today's behaviour. Memory only: this is a
// fault-injection wrapper, not a backend property.
func TestPolicyReadFailureFailsClosed(t *testing.T) {
	s := memory.New()
	eng, ctx := newEngine(t, s)
	pol := &policy.Policy{Name: "p", GracePeriod: 2 * time.Hour}
	require.NoError(t, eng.CreatePolicy(ctx, pol))
	orig := mustCreate(t, eng, ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID})

	broken, _ := newEngine(t, failPolicyGet{s})
	vr, err := broken.ValidateKey(ctx, orig.RawKey)
	require.Error(t, err, "validation must not succeed without the policy")
	assert.Nil(t, vr)

	_, err = broken.RotateKey(ctx, orig.Key.ID, rotation.ReasonManual)
	require.Error(t, err)
	k, err := eng.GetKey(ctx, orig.Key.ID)
	require.NoError(t, err)
	assert.Equal(t, orig.Key.KeyHash, k.KeyHash, "a refused rotation leaves the hash alone")
	vr, err = eng.ValidateKey(ctx, orig.RawKey)
	require.NoError(t, err)
	assert.False(t, vr.ViaPreviousKey)

	// A dangling PolicyID: the policy was deleted underneath the key.
	require.NoError(t, s.Policies().Delete(ctx, pol.ID))
	vr, err = eng.ValidateKey(ctx, orig.RawKey)
	require.NoError(t, err, "a missing policy still validates")
	assert.Nil(t, vr.Policy)
	_, err = eng.RotateKey(ctx, orig.Key.ID, rotation.ReasonManual)
	require.NoError(t, err, "a missing policy still rotates")
	recs, err := eng.ListRotations(ctx, &rotation.ListFilter{KeyID: &orig.Key.ID})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, 24*time.Hour, recs[0].GraceTTL, "and falls back to the 24h default")
}

// Every backend's missing-policy error must read as not-found, or a
// dangling PolicyID would now fail validation.
func TestDanglingPolicyStillValidatesAndRotates(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		pol := &policy.Policy{Name: "p", GracePeriod: 2 * time.Hour}
		require.NoError(t, eng.CreatePolicy(ctx, pol))
		r := mustCreate(t, eng, ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID})
		require.NoError(t, s.Policies().Delete(ctx, pol.ID))

		vr, err := eng.ValidateKey(ctx, r.RawKey)
		require.NoError(t, err)
		assert.Nil(t, vr.Policy)
		_, err = eng.RotateKey(ctx, r.Key.ID, rotation.ReasonManual)
		require.NoError(t, err)
	})
}
