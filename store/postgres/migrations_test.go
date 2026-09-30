package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/grove/drivers/pgdriver"
	"github.com/xraph/grove/drivers/pgdriver/pgmigrate"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
)

// openTestStore creates a throwaway database on KEYSMITH_TEST_PG_DSN and
// drops it afterwards. `make test-backends` sets the DSN.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("KEYSMITH_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("KEYSMITH_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	admin := pgdriver.New()
	require.NoError(t, admin.Open(ctx, dsn))
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "ks_" + hex.EncodeToString(b)
	_, err := admin.Exec(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	u.Path = "/" + name
	db := pgdriver.New()
	require.NoError(t, db.Open(ctx, u.String()))
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close()
	})
	s := New(db)
	require.NoError(t, s.Migrate(ctx))
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

func legacyAndModern(t *testing.T, s *Store, keyID id.KeyID) (legacy, modern *rotation.Record) {
	t.Helper()
	ctx := context.Background()
	created := time.Now().Add(-time.Hour)
	legacy = &rotation.Record{
		ID: id.NewRotationID(), KeyID: keyID, TenantID: "t1",
		OldKeyHash: "hash-legacy-" + id.NewRotationID().String(), NewKeyHash: "hash-cur",
		Reason: rotation.ReasonCompromise, GraceTTL: 24 * time.Hour,
		GraceEnds: created.Add(24 * time.Hour), CreatedAt: created,
	}
	modern = &rotation.Record{
		ID: id.NewRotationID(), KeyID: keyID, TenantID: "t1",
		OldKeyHash: "hash-modern-" + id.NewRotationID().String(), NewKeyHash: "hash-cur",
		OldHint: "b7c2", NewHint: "a3f8", Reason: rotation.ReasonManual, GraceTTL: 24 * time.Hour,
		GraceEnds: created.Add(24 * time.Hour), CreatedAt: created,
	}
	require.NoError(t, s.Rotations().Create(ctx, legacy))
	require.NoError(t, s.Rotations().Create(ctx, modern))
	return legacy, modern
}

func assertClosedAndKept(t *testing.T, s *Store, legacy, modern *rotation.Record) {
	t.Helper()
	ctx := context.Background()
	got, err := s.Rotations().Get(ctx, legacy.ID)
	require.NoError(t, err)
	assert.True(t, got.GraceEnds.Equal(got.CreatedAt), "legacy window closed: grace_ends %v, created_at %v", got.GraceEnds, got.CreatedAt)
	got, err = s.Rotations().Get(ctx, modern.ID)
	require.NoError(t, err)
	assert.WithinDuration(t, modern.GraceEnds, got.GraceEnds, time.Millisecond, "a hinted rotation keeps its window")
}

// Rotations recorded before the grace fix have no old hint and a window the
// engine never honoured. Both migration paths close those windows and leave
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

	t.Run("grove group", func(t *testing.T) {
		legacy, modern := legacyAndModern(t, s, k.ID)
		require.NoError(t, migrationByVersion(t, "20260930000003").Up(ctx, pgmigrate.New(s.db)))
		assertClosedAndKept(t, s, legacy, modern)
	})

	t.Run("Store.Migrate", func(t *testing.T) {
		legacy, modern := legacyAndModern(t, s, k.ID)
		require.NoError(t, s.Migrate(ctx))
		assertClosedAndKept(t, s, legacy, modern)
	})
}
