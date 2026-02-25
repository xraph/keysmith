// Package postgres provides a PostgreSQL implementation of store.Store using grove ORM.
package postgres

import (
	"context"
	"fmt"

	"github.com/xraph/grove/drivers/pgdriver"

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

// Migrate runs all embedded SQL migration statements in order.
func (s *Store) Migrate(ctx context.Context) error {
	for i, sql := range migrationSQL {
		if _, err := s.db.Exec(ctx, sql); err != nil {
			return fmt.Errorf("keysmith/postgres: exec migration %d: %w", i+1, err)
		}
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
