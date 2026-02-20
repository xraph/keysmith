// Package postgres provides a PostgreSQL implementation of store.Store using pgx.
package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/usage"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var _ store.Store = (*Store)(nil)

// Store is the PostgreSQL-backed store implementation.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a new PostgreSQL store with the given connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// NewFromDSN creates a new PostgreSQL store by connecting to the given DSN.
func NewFromDSN(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: connect: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Keys returns the key store.
func (s *Store) Keys() key.Store { return &keyStore{pool: s.pool} }

// Policies returns the policy store.
func (s *Store) Policies() policy.Store { return &policyStore{pool: s.pool} }

// Usages returns the usage store.
func (s *Store) Usages() usage.Store { return &usageStore{pool: s.pool} }

// Rotations returns the rotation store.
func (s *Store) Rotations() rotation.Store { return &rotationStore{pool: s.pool} }

// Scopes returns the scope store.
func (s *Store) Scopes() scope.Store { return &scopeStore{pool: s.pool} }

// Migrate runs all embedded SQL migration files in order.
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("keysmith/postgres: read migrations: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		sql, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("keysmith/postgres: read %s: %w", entry.Name(), err)
		}

		if _, err := s.pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("keysmith/postgres: exec %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases the connection pool.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}
