package sqlite

import (
	"context"

	"github.com/xraph/grove/migrate"
)

func init() {
	Migrations.Add(
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS keysmith_rotations (
    id           TEXT PRIMARY KEY,
    key_id       TEXT NOT NULL REFERENCES keysmith_keys(id) ON DELETE CASCADE,
    tenant_id    TEXT NOT NULL,
    old_key_hash TEXT NOT NULL,
    new_key_hash TEXT NOT NULL,
    reason       TEXT NOT NULL,
    grace_ttl_ms INTEGER NOT NULL DEFAULT 86400000,
    grace_ends   TEXT NOT NULL,
    rotated_by   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_keysmith_rotations_key ON keysmith_rotations (key_id, created_at DESC);
`)
			return err
		},
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `DROP TABLE IF EXISTS keysmith_rotations`)
			return err
		},
	)
}
