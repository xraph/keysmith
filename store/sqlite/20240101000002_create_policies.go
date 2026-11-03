package sqlite

import (
	"context"

	"github.com/xraph/grove/migrate"
)

func init() {
	Migrations.Add(
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS keysmith_policies (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL,
    app_id            TEXT NOT NULL,
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    rate_limit        INTEGER NOT NULL DEFAULT 0,
    rate_limit_window INTEGER NOT NULL DEFAULT 0,
    burst_limit       INTEGER NOT NULL DEFAULT 0,
    allowed_scopes    TEXT NOT NULL DEFAULT '[]',
    allowed_ips       TEXT NOT NULL DEFAULT '[]',
    allowed_origins   TEXT NOT NULL DEFAULT '[]',
    allowed_methods   TEXT NOT NULL DEFAULT '[]',
    allowed_paths     TEXT NOT NULL DEFAULT '[]',
    max_key_lifetime  INTEGER NOT NULL DEFAULT 0,
    rotation_period   INTEGER NOT NULL DEFAULT 0,
    grace_period      INTEGER NOT NULL DEFAULT 86400000000000,
    daily_quota       INTEGER NOT NULL DEFAULT 0,
    monthly_quota     INTEGER NOT NULL DEFAULT 0,
    metadata          TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at        TEXT NOT NULL DEFAULT (datetime('now')),

    UNIQUE(tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_keysmith_policies_tenant ON keysmith_policies (tenant_id);
`)
			return err
		},
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `DROP TABLE IF EXISTS keysmith_policies`)
			return err
		},
	)
}
