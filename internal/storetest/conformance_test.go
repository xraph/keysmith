package storetest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store"
)

func newKey(tenant string) *key.Key {
	now := time.Now() // local, with a monotonic reading: the case that broke sqlite
	exp := now.Add(48 * time.Hour)
	return &key.Key{
		ID: id.NewKeyID(), TenantID: tenant, AppID: "app", Name: "k",
		Prefix: "sk", Hint: "a3f8", KeyHash: "hash-" + id.NewKeyID().String(),
		Environment: key.EnvLive, State: key.StateActive,
		ExpiresAt: &exp, CreatedAt: now, UpdatedAt: now,
	}
}

func TestKeyRoundTripKeepsInstants(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))

		got, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)
		// Mongo keeps milliseconds, so compare instants within a millisecond.
		assert.WithinDuration(t, k.CreatedAt, got.CreatedAt, time.Millisecond)
		require.NotNil(t, got.ExpiresAt)
		assert.WithinDuration(t, *k.ExpiresAt, *got.ExpiresAt, time.Millisecond)

		byHash, err := s.Keys().GetByHash(ctx, k.KeyHash)
		require.NoError(t, err)
		assert.Equal(t, k.ID.String(), byHash.ID.String())
	})
}

func TestListExpiredComparesInstants(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		past := newKey("t1")
		p := time.Now().Add(-time.Hour)
		past.ExpiresAt = &p
		future := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, past))
		require.NoError(t, s.Keys().Create(ctx, future))

		expired, err := s.Keys().ListExpired(ctx, time.Now())
		require.NoError(t, err)
		ids := keyIDs(expired)
		assert.Contains(t, ids, past.ID.String())
		assert.NotContains(t, ids, future.ID.String())
	})
}

// An empty TenantID in a list filter is not "no tenant". On every backend it
// means "every tenant". This test records that fact so the contract layer,
// which must never pass an empty tenant, has something to defend against.
func TestEmptyTenantFilterMatchesEveryTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		a, b := newKey("tenant-a"), newKey("tenant-b")
		require.NoError(t, s.Keys().Create(ctx, a))
		require.NoError(t, s.Keys().Create(ctx, b))

		all, err := s.Keys().List(ctx, &key.ListFilter{TenantID: ""})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{a.ID.String(), b.ID.String()}, keyIDs(all))

		onlyA, err := s.Keys().List(ctx, &key.ListFilter{TenantID: "tenant-a"})
		require.NoError(t, err)
		assert.Equal(t, []string{a.ID.String()}, keyIDs(onlyA))
	})
}

func keyIDs(ks []*key.Key) []string {
	out := make([]string, len(ks))
	for i, k := range ks {
		out[i] = k.ID.String()
	}
	return out
}

func TestNotFoundIsASentinel(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		_, err := s.Keys().Get(ctx, id.NewKeyID())
		assert.ErrorIs(t, err, store.ErrKeyNotFound)
		_, err = s.Keys().GetByHash(ctx, "nope")
		assert.ErrorIs(t, err, store.ErrKeyNotFound)
		_, err = s.Policies().Get(ctx, id.NewPolicyID())
		assert.ErrorIs(t, err, store.ErrPolicyNotFound)
		_, err = s.Scopes().GetByName(ctx, "t1", "nope")
		assert.ErrorIs(t, err, store.ErrScopeNotFound)
		_, err = s.Rotations().LatestForKey(ctx, id.NewKeyID())
		assert.ErrorIs(t, err, store.ErrRotationNotFound)
	})
}
