package sqlite

import (
	"context"

	"github.com/xraph/grove/migrate"
)

func init() {
	Migrations.Add(
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS keysmith_keys (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    app_id          TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    prefix          TEXT NOT NULL,
    hint            TEXT NOT NULL,
    key_hash        TEXT NOT NULL UNIQUE,
    environment     TEXT NOT NULL DEFAULT 'live',
    state           TEXT NOT NULL DEFAULT 'active',
    policy_id       TEXT,
    metadata        TEXT NOT NULL DEFAULT '{}',
    created_by      TEXT NOT NULL DEFAULT '',
    expires_at      TEXT,
    last_used_at    TEXT,
    rotated_at      TEXT,
    revoked_at      TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_keysmith_keys_tenant ON keysmith_keys (tenant_id);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_hash ON keysmith_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_state ON keysmith_keys (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_env ON keysmith_keys (tenant_id, environment);
`)
			return err
		},
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `DROP TABLE IF EXISTS keysmith_keys`)
			return err
		},
	)
}
