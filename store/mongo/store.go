package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongod "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/mongodriver"

	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/usage"
)

// Collection name constants.
const (
	colKeys      = "keysmith_keys"
	colPolicies  = "keysmith_policies"
	colScopes    = "keysmith_scopes"
	colKeyScopes = "keysmith_key_scopes"
	colUsage     = "keysmith_usage"
	colUsageAgg  = "keysmith_usage_agg"
	colRotations = "keysmith_rotations"
)

// compile-time interface check
var _ store.Store = (*Store)(nil)

// Store implements store.Store using MongoDB via Grove ORM.
type Store struct {
	db  *grove.DB
	mdb *mongodriver.MongoDB
}

// New creates a new MongoDB store backed by Grove ORM.
func New(db *grove.DB) *Store {
	return &Store{
		db:  db,
		mdb: mongodriver.Unwrap(db),
	}
}

// DB returns the underlying grove database for direct access.
func (s *Store) DB() *grove.DB { return s.db }

// Keys returns the key store.
func (s *Store) Keys() key.Store { return &keyStore{mdb: s.mdb} }

// Policies returns the policy store.
func (s *Store) Policies() policy.Store { return &policyStore{mdb: s.mdb} }

// Usages returns the usage store.
func (s *Store) Usages() usage.Store { return &usageStore{mdb: s.mdb} }

// Rotations returns the rotation store.
func (s *Store) Rotations() rotation.Store { return &rotationStore{mdb: s.mdb} }

// Scopes returns the scope store.
func (s *Store) Scopes() scope.Store { return &scopeStore{mdb: s.mdb} }

// Migrate creates indexes for all keysmith collections.
func (s *Store) Migrate(ctx context.Context) error {
	indexes := migrationIndexes()

	for col, models := range indexes {
		if len(models) == 0 {
			continue
		}

		_, err := s.mdb.Collection(col).Indexes().CreateMany(ctx, models)
		if err != nil {
			return fmt.Errorf("keysmith/mongo: migrate %s indexes: %w", col, err)
		}
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

type notFoundError struct{ entity string }

func (e *notFoundError) Error() string { return e.entity + " not found" }

func errNotFound(entity string) error { return &notFoundError{entity: entity} }

// isNoDocuments checks if an error wraps mongo.ErrNoDocuments.
func isNoDocuments(err error) bool {
	return errors.Is(err, mongod.ErrNoDocuments)
}

// now returns the current UTC time.
func now() time.Time {
	return time.Now().UTC()
}

// migrationIndexes returns the index definitions for all keysmith collections.
func migrationIndexes() map[string][]mongod.IndexModel {
	return map[string][]mongod.IndexModel{
		colKeys: {
			{
				Keys:    bson.D{{Key: "key_hash", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "state", Value: 1}}},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "environment", Value: 1}}},
			{Keys: bson.D{{Key: "prefix", Value: 1}, {Key: "hint", Value: 1}}},
			{Keys: bson.D{{Key: "policy_id", Value: 1}}},
			{Keys: bson.D{{Key: "expires_at", Value: 1}}},
		},
		colPolicies: {
			{
				Keys:    bson.D{{Key: "tenant_id", Value: 1}, {Key: "name", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}}},
		},
		colScopes: {
			{
				Keys:    bson.D{{Key: "tenant_id", Value: 1}, {Key: "name", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "parent", Value: 1}}},
		},
		colKeyScopes: {
			{
				Keys:    bson.D{{Key: "key_id", Value: 1}, {Key: "scope_id", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
			{Keys: bson.D{{Key: "key_id", Value: 1}}},
			{Keys: bson.D{{Key: "scope_id", Value: 1}}},
		},
		colUsage: {
			{Keys: bson.D{{Key: "key_id", Value: 1}, {Key: "created_at", Value: -1}}},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}},
		},
		colUsageAgg: {
			{
				Keys:    bson.D{{Key: "key_id", Value: 1}, {Key: "period", Value: 1}, {Key: "period_start", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
			{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "period", Value: 1}, {Key: "period_start", Value: -1}}},
		},
		colRotations: {
			{Keys: bson.D{{Key: "key_id", Value: 1}, {Key: "created_at", Value: -1}}},
			{Keys: bson.D{{Key: "grace_ends", Value: 1}}},
		},
	}
}
