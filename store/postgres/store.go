// Package postgres provides a PostgreSQL implementation of store.Store using grove ORM.
package postgres

import (
	"context"
	"fmt"

	"github.com/xraph/grove/drivers/pgdriver"
	"github.com/xraph/grove/drivers/pgdriver/pgmigrate"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/usage"
)

var _ store.Store = (*Store)(nil)

// Store is the PostgreSQL-backed store implementation using grove ORM.
type Store struct {
	db *pgdriver.PgDB
}

// New creates a new PostgreSQL store with the given grove pgdriver instance.
func New(db *pgdriver.PgDB) *Store {
	return &Store{db: db}
}

// NewFromDSN creates a new PostgreSQL store by connecting to the given DSN.
func NewFromDSN(ctx context.Context, dsn string) (*Store, error) {
	db := pgdriver.New()
	if err := db.Open(ctx, dsn); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: connect: %w", err)
	}
	return &Store{db: db}, nil
}

// Keys returns the key store.
func (s *Store) Keys() key.Store { return &keyStore{db: s.db} }

// Policies returns the policy store.
func (s *Store) Policies() policy.Store { return &policyStore{db: s.db} }

// Usages returns the usage store.
func (s *Store) Usages() usage.Store { return &usageStore{db: s.db} }

// Rotations returns the rotation store.
func (s *Store) Rotations() rotation.Store { return &rotationStore{db: s.db} }

// Scopes returns the scope store.
func (s *Store) Scopes() scope.Store { return &scopeStore{db: s.db} }

// Migrate applies the pending migrations in [Migrations] through grove's
// orchestrator. It holds grove's migration lock (a Postgres advisory lock)
// while it runs, so nodes that start together against one database take
// turns, and every one after the first finds nothing left to apply.
func (s *Store) Migrate(ctx context.Context) error {
	orch := migrate.NewOrchestrator(pgmigrate.New(s.db), Migrations)
	if _, err := orch.Migrate(ctx); err != nil {
		return fmt.Errorf("keysmith/postgres: migrate: %w", err)
	}
	return nil
}

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

// Close releases the connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}
