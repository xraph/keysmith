package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/sqlitedriver"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/usage"

	// Migrate needs the sqlite migration executor registered, and a host that
	// only imports this package should not have to know that.
	_ "github.com/xraph/grove/drivers/sqlitedriver/sqlitemigrate"
)

// compile-time interface check
var _ store.Store = (*Store)(nil)

// Store implements store.Store using SQLite via Grove ORM.
type Store struct {
	db  *grove.DB
	sdb *sqlitedriver.SqliteDB
}

// New creates a new SQLite store backed by Grove ORM.
func New(db *grove.DB) *Store {
	return &Store{
		db:  db,
		sdb: sqlitedriver.Unwrap(db),
	}
}

// DB returns the underlying grove database for direct access.
func (s *Store) DB() *grove.DB { return s.db }

// Keys returns the key store.
func (s *Store) Keys() key.Store { return &keyStore{sdb: s.sdb} }

// Policies returns the policy store.
func (s *Store) Policies() policy.Store { return &policyStore{sdb: s.sdb} }

// Usages returns the usage store.
func (s *Store) Usages() usage.Store { return &usageStore{sdb: s.sdb} }

// Rotations returns the rotation store.
func (s *Store) Rotations() rotation.Store { return &rotationStore{sdb: s.sdb} }

// Scopes returns the scope store.
func (s *Store) Scopes() scope.Store { return &scopeStore{sdb: s.sdb} }

// Migrate creates the required tables and indexes using the grove orchestrator.
func (s *Store) Migrate(ctx context.Context) error {
	executor, err := migrate.NewExecutorFor(s.sdb)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: create migration executor: %w", err)
	}
	orch := migrate.NewOrchestrator(executor, Migrations)
	if _, err := orch.Migrate(ctx); err != nil {
		return fmt.Errorf("keysmith/sqlite: migration failed: %w", err)
	}
	return nil
}

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func errNotFound(entity string) error { return store.NotFound(entity) }

// isNoRows checks for the standard sql.ErrNoRows sentinel.
func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

// dbTime normalizes a time before it is bound to a query or written to a
// model. modernc/sqlite stores a time.Time as t.String() unless the DSN sets
// _time_format, and this store does not own the DSN. That string only scans
// back when it is UTC with no monotonic reading: a monotonic reading adds
// " m=+..." and a local zone adds a name, and either one fails the scan for
// every row a query touches. t.UTC() fixes both and keeps the instant. UTC
// strings with trimmed fractions also sort as text, which the time
// comparisons in this package rely on.
func dbTime(t time.Time) time.Time { return t.UTC() }

func dbTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := dbTime(*t)
	return &u
}
