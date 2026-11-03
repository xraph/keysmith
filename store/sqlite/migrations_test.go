package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sync"
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

// wantMigrations is every migration in the group, in order, with the version
// and name keysmith has always recorded. A database that applied one under
// these keys skips it, so none of them may change.
var wantMigrations = []struct{ version, name string }{
	{"20240101000001", "create_keys"},
	{"20240101000002", "create_policies"},
	{"20240101000003", "create_scopes"},
	{"20240101000004", "create_usage"},
	{"20240101000005", "create_rotations"},
	{"20260930000001", "rotation_hints_and_old_hash_index"},
	{"20260930000002", "drop_usage_agg"},
	{"20260930000003", "close_legacy_grace_windows"},
	{"20261007000001", "key_version"},
}

// A migration in a file Go leaves out of the build never registers, and
// nothing says so. Pin the whole list, and check every migration file in the
// package registered what its name says.
func TestMigrationsRegistered(t *testing.T) {
	got := Migrations.Migrations()
	require.Len(t, got, len(wantMigrations))
	byVersion := make(map[string]*migrate.Migration, len(got))
	for i, m := range got {
		assert.Equal(t, wantMigrations[i].version, m.Version, "migration %d", i)
		assert.Equal(t, wantMigrations[i].name, m.Name, "migration %d", i)
		assert.Equal(t, "keysmith", m.Group)
		assert.NotNil(t, m.Up, "%s has an Up", m.Name)
		byVersion[m.Version] = m
	}

	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	fileRE := regexp.MustCompile(`^(\d{14})_(.+)\.go$`)
	files := 0
	for _, e := range entries {
		match := fileRE.FindStringSubmatch(e.Name())
		if match == nil {
			continue
		}
		files++
		m, ok := byVersion[match[1]]
		if !assert.True(t, ok, "%s registered nothing", e.Name()) {
			continue
		}
		if m.Version == "20260930000003" {
			// Its name ends in _windows, so its file can't (see the file).
			assert.Equal(t, "close_legacy_grace", match[2])
			continue
		}
		assert.Equal(t, m.Name, match[2], "%s registers its own name", e.Name())
	}
	assert.Equal(t, len(wantMigrations), files, "one file per migration")
}

// openUnmigrated opens a store on path with a connection pool of its own.
func openUnmigrated(t *testing.T, path string) *Store {
	t.Helper()
	sdb := sqlitedriver.New()
	require.NoError(t, sdb.Open(context.Background(), DSN(path)))
	db, err := grove.Open(sdb)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s := openUnmigrated(t, filepath.Join(t.TempDir(), "keysmith.db"))
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

func appliedVersions(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.sdb.Query(context.Background(),
		`SELECT version FROM grove_migrations WHERE "group" = 'keysmith' ORDER BY version`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		out = append(out, v)
	}
	require.NoError(t, rows.Err())
	return out
}

func allVersions() []string {
	out := make([]string, len(wantMigrations))
	for i, m := range wantMigrations {
		out[i] = m.version
	}
	return out
}

func tableExists(t *testing.T, s *Store, name string) bool {
	t.Helper()
	var n int
	require.NoError(t, s.sdb.QueryRow(context.Background(),
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n))
	return n == 1
}

func assertMigrated(t *testing.T, s *Store) {
	t.Helper()
	assert.Equal(t, allVersions(), appliedVersions(t, s))
	assert.False(t, tableExists(t, s, "keysmith_usage_agg"), "keysmith_usage_agg is gone")
	for _, table := range []string{"keysmith_keys", "keysmith_policies", "keysmith_scopes", "keysmith_key_scopes", "keysmith_usage", "keysmith_rotations"} {
		assert.True(t, tableExists(t, s, table), "%s exists", table)
	}
}

// A fresh database gets every migration, and never the aggregate table.
func TestMigrateFresh(t *testing.T) {
	assertMigrated(t, openTestStore(t))
}

// A database keysmith v1.8.x migrated has every version recorded. The
// current group applies none of them again. SQLite has no ADD COLUMN IF NOT
// EXISTS, so a rerun of rotation_hints_and_old_hash_index or key_version would
// fail here.
func TestUpgradeFromRecordedGroup(t *testing.T) {
	ctx := context.Background()
	s := openUnmigrated(t, filepath.Join(t.TempDir(), "keysmith.db"))
	exec, err := migrate.NewExecutorFor(s.sdb)
	require.NoError(t, err)
	_, err = migrate.NewOrchestrator(exec, legacyGroup()).Migrate(ctx)
	require.NoError(t, err)

	now := time.Now()
	k := &key.Key{
		ID: id.NewKeyID(), TenantID: "t1", AppID: "app", Name: "k", Prefix: "sk",
		Hint: "a3f8", KeyHash: "hash-cur", Environment: key.EnvLive, State: key.StateActive,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, s.Keys().Create(ctx, k))

	require.NoError(t, s.Migrate(ctx))
	assertMigrated(t, s)
	_, err = s.Keys().Get(ctx, k.ID)
	require.NoError(t, err)
}

// Processes that open one database file and migrate at once take turns on
// grove's lock row, and all of them succeed.
func TestConcurrentMigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keysmith.db")
	stores := make([]*Store, 8)
	for i := range stores {
		stores[i] = openUnmigrated(t, path)
	}
	for round := range 2 {
		errs := make([]error, len(stores))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, s := range stores {
			wg.Go(func() {
				<-start
				errs[i] = s.Migrate(context.Background())
			})
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			require.NoError(t, err, "round %d, node %d", round, i)
		}
	}
	assertMigrated(t, stores[0])
}

// The group rolls all the way back and forward again, so every Down undoes
// what its Up did.
func TestRollbackAll(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	exec, err := migrate.NewExecutorFor(s.sdb)
	require.NoError(t, err)
	orch := migrate.NewOrchestrator(exec, Migrations)
	for range wantMigrations {
		_, err := orch.Rollback(ctx)
		require.NoError(t, err)
	}
	assert.Empty(t, appliedVersions(t, s))
	for _, table := range []string{"keysmith_keys", "keysmith_usage", "keysmith_usage_agg", "keysmith_rotations"} {
		assert.False(t, tableExists(t, s, table), "%s dropped", table)
	}
	require.NoError(t, s.Migrate(ctx))
	assertMigrated(t, s)
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
