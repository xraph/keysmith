package postgres

import (
	"context"

	"github.com/xraph/grove/migrate"
)

// Removes the aggregate table nothing ever wrote to. Usage is aggregated from
// keysmith_usage. Only a database that ran an older create_usage has it.
func init() {
	Migrations.Add(
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `DROP TABLE IF EXISTS keysmith_usage_agg`)
			return err
		},
		// The table was never written to, so Down puts it back empty.
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS keysmith_usage_agg (
    key_id        TEXT NOT NULL,
    tenant_id     TEXT NOT NULL,
    period        TEXT NOT NULL,
    period_start  TIMESTAMPTZ NOT NULL,
    request_count BIGINT NOT NULL DEFAULT 0,
    error_count   BIGINT NOT NULL DEFAULT 0,
    total_latency BIGINT NOT NULL DEFAULT 0,
    p50_latency   BIGINT NOT NULL DEFAULT 0,
    p99_latency   BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (key_id, period, period_start)
);

CREATE INDEX IF NOT EXISTS idx_keysmith_usage_agg_tenant ON keysmith_usage_agg (tenant_id, period, period_start DESC);
`)
			return err
		},
	)
}
