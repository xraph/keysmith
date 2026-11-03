package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sync"
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
		assert.True(t, m.Transactional, "%s runs in a transaction", m.Name)
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

// freshDatabase creates a throwaway database on KEYSMITH_TEST_PG_DSN, drops it
// when the test ends and returns its DSN. `make test-backends` sets the DSN.
func freshDatabase(t *testing.T) string {
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
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close()
	})
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	u.Path = "/" + name
	return u.String()
}

// openUnmigrated opens a store with a connection pool of its own on dsn.
func openUnmigrated(t *testing.T, dsn string) *Store {
	t.Helper()
	db := pgdriver.New()
	require.NoError(t, db.Open(context.Background(), dsn))
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

// openTestStore opens a migrated store on a fresh database.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s := openUnmigrated(t, freshDatabase(t))
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

// forget deletes the applied record of a migration, so the next Migrate runs
// it again, the way it runs on a database the v1.8.x raw path built.
func forget(t *testing.T, s *Store, version string) {
	t.Helper()
	_, err := s.db.Exec(context.Background(),
		`DELETE FROM grove_migrations WHERE "group" = 'keysmith' AND version = $1`, version)
	require.NoError(t, err)
}

func appliedVersions(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(context.Background(),
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
	var exists bool
	require.NoError(t, s.db.QueryRow(context.Background(),
		`SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists))
	return exists
}

// assertMigrated checks a database the group has fully migrated: every
// version recorded once, the key version column there and no aggregate table.
func assertMigrated(t *testing.T, s *Store) {
	t.Helper()
	assert.Equal(t, allVersions(), appliedVersions(t, s))
	assert.False(t, tableExists(t, s, "keysmith_usage_agg"), "keysmith_usage_agg is gone")
	for _, table := range []string{"keysmith_keys", "keysmith_policies", "keysmith_scopes", "keysmith_key_scopes", "keysmith_usage", "keysmith_rotations"} {
		assert.True(t, tableExists(t, s, table), "%s exists", table)
	}
	var cols int
	require.NoError(t, s.db.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'keysmith_keys' AND column_name = 'version'`).Scan(&cols))
	assert.Equal(t, 1, cols, "keysmith_keys has a version column")
}

// migrateTogether runs Migrate on every store at once and returns each error.
func migrateTogether(stores []*Store) []error {
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
	return errs
}

// Nodes that start together against one database each run Store.Migrate at
// the same moment. Before Migrate took grove's lock they ran the raw schema
// side by side, and the creates and drops of keysmith_usage_agg collided on
// pg_type on every overlapping start, not only the first. Every round here
// is an overlapping start of eight nodes: the first on an empty database, the
// rest on one that's already migrated. Each node has a connection pool of its
// own, as a separate process would.
func TestConcurrentMigrate(t *testing.T) {
	const (
		databases = 4
		nodes     = 8
		rounds    = 3
	)
	for d := range databases {
		t.Run(fmt.Sprintf("database %d", d), func(t *testing.T) {
			dsn := freshDatabase(t)
			stores := make([]*Store, nodes)
			for i := range stores {
				stores[i] = openUnmigrated(t, dsn)
			}
			for round := range rounds {
				for i, err := range migrateTogether(stores) {
					require.NoError(t, err, "round %d, node %d", round, i)
				}
			}
			assertMigrated(t, stores[0])
		})
	}
}

// A database keysmith v1.8.x built with its raw schema has every table and
// no record in grove_migrations. The group runs over it once, changes nothing
// a key or rotation holds, records every version, closes a legacy grace window
// as the raw path used to on every start, and then has nothing left to do.
func TestUpgradeFromRawSchema(t *testing.T) {
	ctx := context.Background()
	s := openUnmigrated(t, freshDatabase(t))
	for i, stmt := range legacyMigrationSQL {
		_, err := s.db.Exec(ctx, stmt)
		require.NoError(t, err, "legacy statement %d", i+1)
	}
	// A v1.8.x node ran that on every start, so run it twice, as two
	// restarts would.
	for _, stmt := range legacyMigrationSQL {
		_, err := s.db.Exec(ctx, stmt)
		require.NoError(t, err)
	}
	require.False(t, tableExists(t, s, "grove_migrations"), "the raw path records nothing")

	k := seedKey(t, s)
	legacy, modern := legacyAndModern(t, s, k.ID)
	before, err := s.Keys().Get(ctx, k.ID)
	require.NoError(t, err)

	require.NoError(t, s.Migrate(ctx))
	assertMigrated(t, s)
	assertClosedAndKept(t, s, legacy, modern)

	got, err := s.Keys().Get(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, k.KeyHash, got.KeyHash)
	assert.Equal(t, key.StateSuspended, got.State)
	assert.Equal(t, before.Version, got.Version, "the migration leaves the write counter alone")

	require.NoError(t, s.Migrate(ctx), "a second start finds nothing to apply")
	assert.Equal(t, allVersions(), appliedVersions(t, s))
}

// A host that ran postgres.Migrations through its own grove orchestrator on
// v1.8.x has every version recorded already. Store.Migrate applies none of
// them again. The v1.8.x group ran the same statements as legacyMigrationSQL,
// so replaying those under the same versions rebuilds that database.
func TestUpgradeFromRecordedGroup(t *testing.T) {
	ctx := context.Background()
	s := openUnmigrated(t, freshDatabase(t))
	old := migrate.NewGroup("keysmith")
	for i, stmt := range legacyMigrationSQL {
		old.MustRegister(&migrate.Migration{
			Version: wantMigrations[i].version,
			Name:    wantMigrations[i].name,
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, stmt)
				return err
			},
		})
	}
	_, err := migrate.NewOrchestrator(pgmigrate.New(s.db), old).Migrate(ctx)
	require.NoError(t, err)
	k := seedKey(t, s)

	require.NoError(t, s.Migrate(ctx))
	assertMigrated(t, s)
	_, err = s.Keys().Get(ctx, k.ID)
	require.NoError(t, err)
}

func seedKey(t *testing.T, s *Store) *key.Key {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	k := &key.Key{
		ID: id.NewKeyID(), TenantID: "t1", AppID: "app", Name: "k", Prefix: "sk",
		Hint: "a3f8", KeyHash: "hash-cur-" + id.NewKeyID().String(), Environment: key.EnvLive,
		State: key.StateActive, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, s.Keys().Create(ctx, k))
	require.NoError(t, s.Keys().UpdateState(ctx, k.ID, key.StateSuspended))
	return k
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
// engine never honoured. The migration closes those windows and leaves
// rotations recorded since alone, whether it's run on its own or by a
// Store.Migrate that hasn't recorded it yet.
func TestCloseLegacyGraceWindows(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	k := seedKey(t, s)

	t.Run("migration", func(t *testing.T) {
		legacy, modern := legacyAndModern(t, s, k.ID)
		require.NoError(t, migrationByVersion(t, "20260930000003").Up(ctx, pgmigrate.New(s.db)))
		assertClosedAndKept(t, s, legacy, modern)
	})

	t.Run("Store.Migrate", func(t *testing.T) {
		legacy, modern := legacyAndModern(t, s, k.ID)
		forget(t, s, "20260930000003")
		require.NoError(t, s.Migrate(ctx))
		assertClosedAndKept(t, s, legacy, modern)
	})
}

// The key version migration adds the column to a table that has rows, and
// those rows start at version 0. It's safe to run twice, which a database the
// raw path already gave the column relies on.
func TestKeyVersionMigration(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	k := seedKey(t, s)

	for name, up := range map[string]func() error{
		"migration": func() error { return migrationByVersion(t, "20261007000001").Up(ctx, pgmigrate.New(s.db)) },
		"Store.Migrate": func() error {
			forget(t, s, "20261007000001")
			return s.Migrate(ctx)
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.db.Exec(ctx, `ALTER TABLE keysmith_keys DROP COLUMN version`)
			require.NoError(t, err)
			require.NoError(t, up())
			got, err := s.Keys().Get(ctx, k.ID)
			require.NoError(t, err)
			assert.Equal(t, int64(0), got.Version)
			require.NoError(t, up(), "running it again changes nothing")
		})
	}
}

// The group rolls all the way back and forward again, so every Down undoes
// what its Up did.
func TestRollbackAll(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	orch := migrate.NewOrchestrator(pgmigrate.New(s.db), Migrations)
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
