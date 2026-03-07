package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/xraph/grove/drivers/mongodriver/mongomigrate"
	"github.com/xraph/grove/migrate"
)

// Migrations is the grove migration group for the Keysmith mongo store.
var Migrations = migrate.NewGroup("keysmith")

func init() {
	Migrations.MustRegister(
		&migrate.Migration{
			Name:    "create_keysmith_keys",
			Version: "20240101000001",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*keyModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colKeys, []mongo.IndexModel{
					{
						Keys:    bson.D{{Key: "key_hash", Value: 1}},
						Options: options.Index().SetUnique(true),
					},
					{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "state", Value: 1}}},
					{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "environment", Value: 1}}},
					{Keys: bson.D{{Key: "prefix", Value: 1}, {Key: "hint", Value: 1}}},
					{Keys: bson.D{{Key: "policy_id", Value: 1}}},
					{Keys: bson.D{{Key: "expires_at", Value: 1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*keyModel)(nil))
			},
		},
		&migrate.Migration{
			Name:    "create_keysmith_policies",
			Version: "20240101000002",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*policyModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colPolicies, []mongo.IndexModel{
					{
						Keys:    bson.D{{Key: "tenant_id", Value: 1}, {Key: "name", Value: 1}},
						Options: options.Index().SetUnique(true),
					},
					{Keys: bson.D{{Key: "tenant_id", Value: 1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*policyModel)(nil))
			},
		},
		&migrate.Migration{
			Name:    "create_keysmith_scopes",
			Version: "20240101000003",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*scopeModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colScopes, []mongo.IndexModel{
					{
						Keys:    bson.D{{Key: "tenant_id", Value: 1}, {Key: "name", Value: 1}},
						Options: options.Index().SetUnique(true),
					},
					{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "parent", Value: 1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*scopeModel)(nil))
			},
		},
		&migrate.Migration{
			Name:    "create_keysmith_key_scopes",
			Version: "20240101000004",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*keyScopeModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colKeyScopes, []mongo.IndexModel{
					{
						Keys:    bson.D{{Key: "key_id", Value: 1}, {Key: "scope_id", Value: 1}},
						Options: options.Index().SetUnique(true),
					},
					{Keys: bson.D{{Key: "key_id", Value: 1}}},
					{Keys: bson.D{{Key: "scope_id", Value: 1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*keyScopeModel)(nil))
			},
		},
		&migrate.Migration{
			Name:    "create_keysmith_usage",
			Version: "20240101000005",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*usageModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colUsage, []mongo.IndexModel{
					{Keys: bson.D{{Key: "key_id", Value: 1}, {Key: "created_at", Value: -1}}},
					{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*usageModel)(nil))
			},
		},
		&migrate.Migration{
			Name:    "create_keysmith_usage_agg",
			Version: "20240101000006",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*usageAggModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colUsageAgg, []mongo.IndexModel{
					{
						Keys:    bson.D{{Key: "key_id", Value: 1}, {Key: "period", Value: 1}, {Key: "period_start", Value: 1}},
						Options: options.Index().SetUnique(true),
					},
					{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "period", Value: 1}, {Key: "period_start", Value: -1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*usageAggModel)(nil))
			},
		},
		&migrate.Migration{
			Name:    "create_keysmith_rotations",
			Version: "20240101000007",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}

				if err := mexec.CreateCollection(ctx, (*rotationModel)(nil)); err != nil {
					return err
				}

				return mexec.CreateIndexes(ctx, colRotations, []mongo.IndexModel{
					{Keys: bson.D{{Key: "key_id", Value: 1}, {Key: "created_at", Value: -1}}},
					{Keys: bson.D{{Key: "grace_ends", Value: 1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*rotationModel)(nil))
			},
		},
	)
}
