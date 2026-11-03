package postgres

import (
	"context"

	"github.com/xraph/grove/migrate"
)

// The first version of this migration also created keysmith_usage_agg, a
// table nothing ever wrote to, and 20260930000002_drop_usage_agg dropped it
// again. A fresh database never needs it, so it isn't created here any more.
// A database that already has it loses it in drop_usage_agg, as before.
func init() {
	Migrations.Add(
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS keysmith_usage (
    id          TEXT PRIMARY KEY,
    key_id      TEXT NOT NULL REFERENCES keysmith_keys(id) ON DELETE CASCADE,
    tenant_id   TEXT NOT NULL,
    endpoint    TEXT NOT NULL,
    method      TEXT NOT NULL,
    status_code INT NOT NULL,
    ip_address  TEXT,
    user_agent  TEXT,
    latency_ms  BIGINT NOT NULL DEFAULT 0,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_keysmith_usage_key ON keysmith_usage (key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_keysmith_usage_tenant ON keysmith_usage (tenant_id, created_at DESC);
`)
			return err
		},
		// Rolling back past drop_usage_agg puts the aggregate table back, so
		// this drops it too.
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
DROP TABLE IF EXISTS keysmith_usage_agg;
DROP TABLE IF EXISTS keysmith_usage;
`)
			return err
		},
	)
}
