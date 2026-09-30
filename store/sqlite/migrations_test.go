package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/sqlitedriver"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	sdb := sqlitedriver.New()
	require.NoError(t, sdb.Open(context.Background(), filepath.Join(t.TempDir(), "keysmith.db")))
	db, err := grove.Open(sdb)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	s := New(db)
	require.NoError(t, s.Migrate(context.Background()))
	return s
}

func migrationByVersion(t *testing.T, version string) *migrate.Migration {
	t.Helper()
	for _, m := range Migrations.Migrations() {
		if m.Version == version {
			return m
		}
	}
	t.Fatalf("no migration with version %s", version)
	return nil
}

// Rotations recorded before the grace fix have no old hint and a window the
// engine never honoured. The migration closes those windows and leaves
// rotations recorded since alone.
func TestCloseLegacyGraceWindows(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	now := time.Now()
	k := &key.Key{
		ID: id.NewKeyID(), TenantID: "t1", AppID: "app", Name: "k", Prefix: "sk",
		Hint: "a3f8", KeyHash: "hash-cur", Environment: key.EnvLive, State: key.StateActive,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, s.Keys().Create(ctx, k))

	created := now.Add(-time.Hour)
	legacy := &rotation.Record{
		ID: id.NewRotationID(), KeyID: k.ID, TenantID: "t1",
		OldKeyHash: "hash-legacy", NewKeyHash: "hash-cur", Reason: rotation.ReasonCompromise,
		GraceTTL: 24 * time.Hour, GraceEnds: created.Add(24 * time.Hour), CreatedAt: created,
	}
	modern := &rotation.Record{
		ID: id.NewRotationID(), KeyID: k.ID, TenantID: "t1",
		OldKeyHash: "hash-modern", NewKeyHash: "hash-cur", OldHint: "b7c2", NewHint: "a3f8",
		Reason: rotation.ReasonManual, GraceTTL: 24 * time.Hour,
		GraceEnds: created.Add(24 * time.Hour), CreatedAt: created,
	}
	require.NoError(t, s.Rotations().Create(ctx, legacy))
	require.NoError(t, s.Rotations().Create(ctx, modern))

	exec, err := migrate.NewExecutorFor(s.sdb)
	require.NoError(t, err)
	require.NoError(t, migrationByVersion(t, "20260930000003").Up(ctx, exec))

	got, err := s.Rotations().Get(ctx, legacy.ID)
	require.NoError(t, err)
	assert.True(t, got.GraceEnds.Equal(got.CreatedAt), "legacy window closed: grace_ends %v, created_at %v", got.GraceEnds, got.CreatedAt)

	got, err = s.Rotations().Get(ctx, modern.ID)
	require.NoError(t, err)
	assert.WithinDuration(t, modern.GraceEnds, got.GraceEnds, time.Millisecond, "a hinted rotation keeps its window")
}
