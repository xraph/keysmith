package contract

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
)

func TestRequireID(t *testing.T) {
	valid := id.NewKeyID()
	cases := []struct {
		name    string
		raw     string
		message string
	}{
		{"empty", "", "id is required"},
		{"only spaces", "   ", "id is required"},
		{"not a key id", "not-a-key", "id is not a key id"},
		{"valid", valid.String(), ""},
		{"valid with surrounding spaces", "  " + valid.String() + "\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := requireID(tc.raw)
			if tc.message != "" {
				var ce *dashcontract.Error
				require.ErrorAs(t, err, &ce)
				assert.Equal(t, dashcontract.CodeBadRequest, ce.Code)
				assert.Equal(t, tc.message, ce.Message)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, valid, got)
		})
	}
}

func TestLoadKeyForTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mine := create(t, eng, "t1", nil)
		theirs := create(t, eng, "t2", nil)

		k, err := loadKeyForTenant(context.Background(), deps, "t1", mine.Key.ID.String())
		require.NoError(t, err)
		assert.Equal(t, mine.Key.ID, k.ID)

		_, otherTenant := loadKeyForTenant(context.Background(), deps, "t1", theirs.Key.ID.String())
		_, missing := loadKeyForTenant(context.Background(), deps, "t1", id.NewKeyID().String())
		require.Error(t, otherTenant)
		assert.Equal(t, dashcontract.CodeNotFound, codeOf(t, otherTenant))
		// Another tenant's key and a key that does not exist are the same answer.
		assert.Equal(t, missing, otherTenant)
		assert.Equal(t, keyNotFound(), otherTenant)

		_, err = loadKeyForTenant(context.Background(), deps, "t1", "")
		assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err))
	})
}

func TestListOpenWindowsHasNoCap(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		other := create(t, eng, "t1", nil)

		now := time.Now()
		base := now.Add(-200 * time.Hour)
		const total = 120
		var oldest id.RotationID
		for i := 0; i < total; i++ {
			rec := &rotation.Record{
				ID: id.NewRotationID(), KeyID: k.Key.ID, TenantID: "t1",
				OldKeyHash: "old", NewKeyHash: "new", OldHint: "abcd",
				Reason: rotation.ReasonManual, GraceTTL: time.Hour,
				// Oldest first: only the very first record is still open.
				GraceEnds: base.Add(time.Duration(i) * time.Minute),
				CreatedAt: base.Add(time.Duration(i) * time.Minute),
			}
			if i == 0 {
				oldest = rec.ID
				rec.GraceEnds = now.Add(30 * 24 * time.Hour)
			}
			require.NoError(t, s.Rotations().Create(context.Background(), rec))
		}
		// Another key's open window must not leak in.
		require.NoError(t, s.Rotations().Create(context.Background(), &rotation.Record{
			ID: id.NewRotationID(), KeyID: other.Key.ID, TenantID: "t1",
			OldKeyHash: "old", NewKeyHash: "new", OldHint: "zzzz",
			Reason: rotation.ReasonManual, GraceTTL: time.Hour,
			GraceEnds: now.Add(time.Hour), CreatedAt: now,
		}))

		got, err := listOpenWindows(context.Background(), eng, k.Key.ID, now)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, oldest.String(), got[0].RotationID)
		assert.Equal(t, "abcd", got[0].Hint)
	})
}

func TestListOpenWindowsSortsByGraceEndsAndSkipsHintless(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		_, eng := setup(t, s)
		k := create(t, eng, "t1", nil)
		now := time.Now()
		mk := func(hint string, ends time.Duration, age time.Duration) id.RotationID {
			rec := &rotation.Record{
				ID: id.NewRotationID(), KeyID: k.Key.ID, TenantID: "t1",
				OldKeyHash: "old", NewKeyHash: "new", OldHint: hint,
				Reason: rotation.ReasonManual, GraceTTL: time.Hour,
				GraceEnds: now.Add(ends), CreatedAt: now.Add(-age),
			}
			require.NoError(t, s.Rotations().Create(context.Background(), rec))
			return rec.ID
		}
		late := mk("late", 3*time.Hour, 3*time.Hour)
		soon := mk("soon", time.Hour, time.Hour)
		mk("", 2*time.Hour, 2*time.Hour)    // legacy record, no hint
		mk("shut", -time.Hour, 4*time.Hour) // window already closed

		got, err := listOpenWindows(context.Background(), eng, k.Key.ID, now)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, soon.String(), got[0].RotationID)
		assert.Equal(t, late.String(), got[1].RotationID)
	})
}
