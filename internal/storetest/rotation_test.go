package storetest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
)

func rec(keyID id.KeyID, oldHash string, ends time.Time) *rotation.Record {
	return &rotation.Record{
		ID: id.NewRotationID(), KeyID: keyID, TenantID: "t1",
		OldKeyHash: oldHash, NewKeyHash: "new-" + oldHash,
		OldHint: "a3f8", NewHint: "9c01", Reason: rotation.ReasonManual,
		GraceTTL: time.Hour, GraceEnds: ends, RotatedBy: "user_1", CreatedAt: time.Now(),
	}
}

func TestRotationHintsRoundTrip(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		r := rec(k.ID, "old-1", time.Now().Add(time.Hour))
		require.NoError(t, s.Rotations().Create(ctx, r))
		got, err := s.Rotations().Get(ctx, r.ID)
		require.NoError(t, err)
		assert.Equal(t, "a3f8", got.OldHint)
		assert.Equal(t, "9c01", got.NewHint)
		assert.Equal(t, "user_1", got.RotatedBy)
	})
}

func TestGetInGraceByOldHash(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		open := rec(k.ID, "old-open", time.Now().Add(time.Hour))
		closed := rec(k.ID, "old-closed", time.Now().Add(-time.Minute))
		require.NoError(t, s.Rotations().Create(ctx, open))
		require.NoError(t, s.Rotations().Create(ctx, closed))

		got, err := s.Rotations().GetInGraceByOldHash(ctx, "old-open", time.Now())
		require.NoError(t, err)
		assert.Equal(t, open.ID.String(), got.ID.String())

		_, err = s.Rotations().GetInGraceByOldHash(ctx, "old-closed", time.Now())
		assert.ErrorIs(t, err, store.ErrRotationNotFound)
		_, err = s.Rotations().GetInGraceByOldHash(ctx, "never", time.Now())
		assert.ErrorIs(t, err, store.ErrRotationNotFound)
	})
}

func TestEndGraceClosesEveryOpenWindow(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k, other := newKey("t1"), newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		require.NoError(t, s.Keys().Create(ctx, other))
		require.NoError(t, s.Rotations().Create(ctx, rec(k.ID, "a", time.Now().Add(time.Hour))))
		require.NoError(t, s.Rotations().Create(ctx, rec(k.ID, "b", time.Now().Add(2*time.Hour))))
		require.NoError(t, s.Rotations().Create(ctx, rec(other.ID, "c", time.Now().Add(time.Hour))))

		n, err := s.Rotations().EndGrace(ctx, k.ID, time.Now())
		require.NoError(t, err)
		assert.EqualValues(t, 2, n)

		_, err = s.Rotations().GetInGraceByOldHash(ctx, "a", time.Now().Add(time.Second))
		assert.ErrorIs(t, err, store.ErrRotationNotFound)
		_, err = s.Rotations().GetInGraceByOldHash(ctx, "c", time.Now().Add(time.Second))
		assert.NoError(t, err, "another key's window must stay open")
	})
}
