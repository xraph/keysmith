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

// EndGraceByID ends one record's window and leaves the others on the same
// key alone. It never moves a window that already ended later, and a
// missing record is not an error.
func TestEndGraceByIDEndsOnlyThatRecord(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		// Whole milliseconds, because mongo keeps no finer.
		now := time.Now().UTC().Truncate(time.Millisecond)
		target := rec(k.ID, "target", now.Add(24*time.Hour))
		sibling := rec(k.ID, "sibling", now.Add(time.Hour))
		ended := rec(k.ID, "ended", now.Add(-time.Hour))
		for _, r := range []*rotation.Record{target, sibling, ended} {
			require.NoError(t, s.Rotations().Create(ctx, r))
		}

		require.NoError(t, s.Rotations().EndGraceByID(ctx, target.ID, now))
		require.NoError(t, s.Rotations().EndGraceByID(ctx, ended.ID, now))
		require.NoError(t, s.Rotations().EndGraceByID(ctx, id.NewRotationID(), now), "a missing record is not an error")

		ends := func(r *rotation.Record) time.Time {
			got, err := s.Rotations().Get(ctx, r.ID)
			require.NoError(t, err)
			return got.GraceEnds
		}
		assert.WithinDuration(t, now, ends(target), time.Millisecond, "the target's window ends at at")
		assert.WithinDuration(t, sibling.GraceEnds, ends(sibling), time.Millisecond, "a sibling on the same key keeps its window")
		assert.WithinDuration(t, ended.GraceEnds, ends(ended), time.Millisecond, "an ended window is not pushed later")

		_, err := s.Rotations().GetInGraceByOldHash(ctx, "target", now.Add(time.Second))
		require.ErrorIs(t, err, store.ErrRotationNotFound)
		_, err = s.Rotations().GetInGraceByOldHash(ctx, "sibling", now.Add(time.Second))
		require.NoError(t, err)
	})
}

// Paging a tenant's rotations by offset returns every row exactly once, even
// when they all share one created_at, so the order has to be total. The
// Rotations page pages these.
func TestPagingRotationsReturnsEveryRowOnce(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)
		// The SQL stores need the key rows the records reference.
		mine, theirs := newKey("t1"), newKey("t2")
		require.NoError(t, s.Keys().Create(ctx, mine))
		require.NoError(t, s.Keys().Create(ctx, theirs))
		want := map[string]bool{}
		for range 250 {
			r := rec(mine.ID, "old-"+id.NewRotationID().String(), now.Add(time.Hour))
			r.CreatedAt = now
			require.NoError(t, s.Rotations().Create(ctx, r))
			want[r.ID.String()] = true
		}
		other := rec(theirs.ID, "old-theirs", now.Add(time.Hour))
		other.TenantID = "t2"
		other.CreatedAt = now
		require.NoError(t, s.Rotations().Create(ctx, other))

		for range 5 {
			seen := map[string]int{}
			var order []string
			for offset := 0; ; offset += 33 {
				page, err := s.Rotations().List(ctx, &rotation.ListFilter{TenantID: "t1", Limit: 33, Offset: offset})
				require.NoError(t, err)
				for _, r := range page {
					seen[r.ID.String()]++
					order = append(order, r.ID.String())
				}
				if len(page) < 33 {
					break
				}
			}
			require.Len(t, seen, len(want))
			for rid, n := range seen {
				require.True(t, want[rid], "only t1's rotations")
				require.Equal(t, 1, n, "rotation %s came back %d times", rid, n)
			}
			// Every row shares one created_at, so the id alone orders them.
			for i := 1; i < len(order); i++ {
				require.Greater(t, order[i-1], order[i], "row %d is not below row %d in id order", i, i-1)
			}
		}
	})
}
