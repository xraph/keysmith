package mongo

import (
	"context"
	"errors"
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
					{Keys: bson.D{{Key: "old_key_hash", Value: 1}}},
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
		// create_keysmith_rotations already shipped without this index, so
		// deployments that applied it never re-run it. This migration gives
		// them the index; fresh installs get it from both and CreateIndexes
		// is idempotent for an identical spec.
		&migrate.Migration{
			Name:    "add_keysmith_rotations_old_hash_index",
			Version: "20260930000001",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.CreateIndexes(ctx, colRotations, []mongo.IndexModel{
					{Keys: bson.D{{Key: "old_key_hash", Value: 1}}},
				})
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				// mongomigrate has no drop-index helper, so go through the
				// collection. "old_key_hash_1" is the driver's default name.
				err := mexec.DB().Collection(colRotations).Indexes().DropOne(ctx, "old_key_hash_1")
				var cmdErr mongo.CommandError
				if errors.As(err, &cmdErr) && cmdErr.Code == 27 { // 27: IndexNotFound
					return nil
				}
				return err
			},
		},
		// Usage is aggregated from keysmith_usage now, so the aggregate
		// collection nothing ever wrote to can go.
		&migrate.Migration{
			Name:    "drop_keysmith_usage_agg",
			Version: "20260930000002",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				return mexec.DropCollection(ctx, (*usageAggModel)(nil))
			},
			// Nothing ever wrote to the collection, so there is nothing to
			// restore.
			Down: func(context.Context, migrate.Executor) error { return nil },
		},
		// Before the grace fix RotateKey recorded a window on every rotation
		// but killed the old key at once. Those documents have an empty or
		// missing old_hint. Close their windows so the new fallback never
		// honours them. grove stores both times as BSON dates, so $expr
		// compares like with like. Store.Migrate only builds indexes and
		// never runs this; the engine refuses hintless records either way.
		&migrate.Migration{
			Name:    "close_legacy_grace_windows",
			Version: "20260930000003",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				_, err := mexec.DB().Collection(colRotations).UpdateMany(ctx,
					bson.M{
						"$or": bson.A{
							bson.M{"old_hint": bson.M{"$exists": false}},
							bson.M{"old_hint": ""},
						},
						"$expr": bson.M{"$gt": bson.A{"$grace_ends", "$created_at"}},
					},
					bson.A{bson.M{"$set": bson.M{"grace_ends": "$created_at"}}},
				)
				return err
			},
			// The closed windows were never honoured, so there is nothing to
			// restore.
			Down: func(context.Context, migrate.Executor) error { return nil },
		},
		// Usage and rotation pages sort newest first with the id breaking
		// ties. MongoDB cannot finish a sort that an index covers only in
		// part, so without these indexes every page, and the one-row
		// check for whether a tenant recorded any usage, reads and sorts the
		// tenant's whole history. The earlier migrations already shipped, so
		// the indexes arrive here; fresh installs get them from both, and
		// CreateIndexes is idempotent for an identical spec.
		&migrate.Migration{
			Name:    "add_keysmith_stable_sort_indexes",
			Version: "20261005000001",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				for _, col := range []string{colUsage, colRotations} {
					if err := mexec.CreateIndexes(ctx, col, stableSortIndexes()); err != nil {
						return err
					}
				}
				return nil
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				mexec, ok := exec.(*mongomigrate.Executor)
				if !ok {
					return fmt.Errorf("expected mongomigrate executor, got %T", exec)
				}
				// The driver's default names for the two specs.
				for _, col := range []string{colUsage, colRotations} {
					for _, name := range []string{"tenant_id_1_created_at_-1__id_-1", "key_id_1_created_at_-1__id_-1"} {
						err := mexec.DB().Collection(col).Indexes().DropOne(ctx, name)
						var cmdErr mongo.CommandError
						if errors.As(err, &cmdErr) && cmdErr.Code == 27 { // 27: IndexNotFound
							continue
						}
						if err != nil {
							return err
						}
					}
				}
				return nil
			},
		},
	)
}

// stableSortIndexes serve a newest-first page, {created_at: -1, _id: -1},
// filtered by tenant or by key, straight off the index with no sort stage.
func stableSortIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}},
		{Keys: bson.D{{Key: "key_id", Value: 1}, {Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}},
	}
}
