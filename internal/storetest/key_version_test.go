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
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/store"
)

func storedVersion(t *testing.T, s store.Store, keyID id.KeyID) int64 {
	t.Helper()
	got, err := s.Keys().Get(context.Background(), keyID)
	require.NoError(t, err)
	return got.Version
}

func TestKeyVersionStartsAtZero(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(context.Background(), k))
		assert.Equal(t, int64(0), storedVersion(t, s, k.ID))
	})
}

func TestUpdateIfVersionWritesAndCountsUp(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))

		read, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)
		read.Name = "renamed"
		require.NoError(t, s.Keys().UpdateIfVersion(ctx, read, read.Version))
		assert.Equal(t, int64(1), read.Version, "the caller's copy learns the new version")

		got, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)
		assert.Equal(t, "renamed", got.Name)
		assert.Equal(t, int64(1), got.Version)

		// The version it handed back is good for the next write.
		read.Name = "renamed again"
		require.NoError(t, s.Keys().UpdateIfVersion(ctx, read, read.Version))
		assert.Equal(t, int64(2), storedVersion(t, s, k.ID))
	})
}

func TestUpdateIfVersionRefusesAStaleVersion(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))

		first, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)
		second, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)

		now := time.Now()
		first.State = key.StateRevoked
		first.RevokedAt = &now
		require.NoError(t, s.Keys().UpdateIfVersion(ctx, first, first.Version))

		second.Name = "stale"
		second.KeyHash = "hash-stale-" + id.NewKeyID().String()
		err = s.Keys().UpdateIfVersion(ctx, second, second.Version)
		require.ErrorIs(t, err, store.ErrKeyConflict)
		assert.Equal(t, int64(0), second.Version, "a refused write leaves the caller's version alone")

		got, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)
		assert.Equal(t, key.StateRevoked, got.State, "the refused write changed nothing")
		assert.NotNil(t, got.RevokedAt)
		assert.Equal(t, "k", got.Name)
		assert.Equal(t, k.KeyHash, got.KeyHash)
		assert.Equal(t, int64(1), got.Version)
		_, err = s.Keys().GetByHash(ctx, second.KeyHash)
		require.ErrorIs(t, err, store.ErrKeyNotFound, "the refused hash is no key's hash")

		// A version ahead of the stored one is just as stale.
		err = s.Keys().UpdateIfVersion(ctx, second, 5)
		require.ErrorIs(t, err, store.ErrKeyConflict)
	})
}

func TestUpdateIfVersionOnAMissingKeyIsNotFound(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		err := s.Keys().UpdateIfVersion(context.Background(), newKey("t1"), 0)
		require.ErrorIs(t, err, store.ErrKeyNotFound)
	})
}

// Update and UpdateState count up the version, so a version-checked writer
// that read before them notices. Update ignores the version the caller holds:
// writers that never read one keep working.
func TestUpdateAndUpdateStateCountUp(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		stale, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)

		k.Version = 41 // a caller that never set it, or set it wrong
		k.Name = "plain"
		require.NoError(t, s.Keys().Update(ctx, k))
		assert.Equal(t, int64(1), storedVersion(t, s, k.ID))
		assert.Equal(t, int64(41), k.Version, "Update leaves the caller's version alone")

		require.NoError(t, s.Keys().UpdateState(ctx, k.ID, key.StateSuspended))
		assert.Equal(t, int64(2), storedVersion(t, s, k.ID))

		stale.Name = "stale"
		require.ErrorIs(t, s.Keys().UpdateIfVersion(ctx, stale, stale.Version), store.ErrKeyConflict)
		got, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)
		assert.Equal(t, "plain", got.Name)
		assert.Equal(t, key.StateSuspended, got.State)
	})
}

// UpdateLastUsed runs on every validation. Counting it would make every
// version-checked write on a busy key a conflict.
func TestUpdateLastUsedLeavesTheVersion(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		read, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)

		require.NoError(t, s.Keys().UpdateLastUsed(ctx, k.ID, time.Now()))
		got, err := s.Keys().Get(ctx, k.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(0), got.Version)
		assert.NotNil(t, got.LastUsedAt)

		read.Name = "after use"
		require.NoError(t, s.Keys().UpdateIfVersion(ctx, read, read.Version))
	})
}

// Update builds its SET by hand on the grove backends, so it has to write
// every field the model-based update wrote, a cleared pointer included.
// UpdateIfVersion still uses the model-based update; check it the same way.
func TestKeyWritesKeepEveryField(t *testing.T) {
	writes := map[string]func(ctx context.Context, s store.Store, k *key.Key) error{
		"Update": func(ctx context.Context, s store.Store, k *key.Key) error {
			return s.Keys().Update(ctx, k)
		},
		"UpdateIfVersion": func(ctx context.Context, s store.Store, k *key.Key) error {
			return s.Keys().UpdateIfVersion(ctx, k, k.Version)
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			storetest.Each(t, func(t *testing.T, s store.Store) {
				ctx := context.Background()
				pol := &policy.Policy{ID: id.NewPolicyID(), TenantID: "t1", AppID: "app", Name: "p-" + id.NewPolicyID().String()}
				require.NoError(t, s.Policies().Create(ctx, pol))
				k := newKey("t1")
				require.NoError(t, s.Keys().Create(ctx, k))

				at := time.Now().Add(-time.Minute)
				k.TenantID = "t2"
				k.AppID = "app2"
				k.Name = "changed"
				k.Description = "a description"
				k.Prefix = "pk"
				k.Hint = "zz99"
				k.KeyHash = "hash-changed-" + id.NewKeyID().String()
				k.Environment = key.EnvTest
				k.State = key.StateSuspended
				k.PolicyID = &pol.ID
				k.Metadata = map[string]any{"team": "billing"}
				k.CreatedBy = "user_9"
				k.ExpiresAt = nil
				k.LastUsedAt = &at
				k.RotatedAt = &at
				k.RevokedAt = &at
				k.UpdatedAt = at
				require.NoError(t, write(ctx, s, k))

				got, err := s.Keys().GetByHash(ctx, k.KeyHash)
				require.NoError(t, err)
				assert.Equal(t, k.ID.String(), got.ID.String())
				assert.Equal(t, "t2", got.TenantID)
				assert.Equal(t, "app2", got.AppID)
				assert.Equal(t, "changed", got.Name)
				assert.Equal(t, "a description", got.Description)
				assert.Equal(t, "pk", got.Prefix)
				assert.Equal(t, "zz99", got.Hint)
				assert.Equal(t, key.EnvTest, got.Environment)
				assert.Equal(t, key.StateSuspended, got.State)
				require.NotNil(t, got.PolicyID)
				assert.Equal(t, pol.ID.String(), got.PolicyID.String())
				assert.Equal(t, "billing", got.Metadata["team"])
				assert.Equal(t, "user_9", got.CreatedBy)
				assert.Nil(t, got.ExpiresAt, "a cleared expiry is written as cleared")
				for field, p := range map[string]*time.Time{"last_used_at": got.LastUsedAt, "rotated_at": got.RotatedAt, "revoked_at": got.RevokedAt} {
					require.NotNil(t, p, field)
					assert.WithinDuration(t, at, *p, time.Millisecond, field)
				}
				assert.WithinDuration(t, at, got.UpdatedAt, time.Millisecond)
				assert.WithinDuration(t, k.CreatedAt, got.CreatedAt, time.Millisecond)
				assert.Equal(t, int64(1), got.Version)

				_, err = s.Keys().GetByHash(ctx, "hash-"+k.ID.String())
				require.ErrorIs(t, err, store.ErrKeyNotFound)
			})
		})
	}
}
