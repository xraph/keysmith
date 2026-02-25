package sqlite

import (
	"context"

	"github.com/xraph/grove/migrate"
)

// Migrations is the grove migration group for the Keysmith store (SQLite).
var Migrations = migrate.NewGroup("keysmith")

func init() {
	Migrations.MustRegister(
		&migrate.Migration{
			Name:    "create_keys",
			Version: "20240101000001",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
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
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS keysmith_keys`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "create_policies",
			Version: "20240101000002",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
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
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS keysmith_policies`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "create_scopes",
			Version: "20240101000003",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS keysmith_scopes (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    parent      TEXT,
    metadata    TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),

    UNIQUE(tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_keysmith_scopes_tenant ON keysmith_scopes (tenant_id);
CREATE INDEX IF NOT EXISTS idx_keysmith_scopes_parent ON keysmith_scopes (tenant_id, parent);

CREATE TABLE IF NOT EXISTS keysmith_key_scopes (
    key_id    TEXT NOT NULL REFERENCES keysmith_keys(id) ON DELETE CASCADE,
    scope_id  TEXT NOT NULL REFERENCES keysmith_scopes(id) ON DELETE CASCADE,
    PRIMARY KEY (key_id, scope_id)
);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
DROP TABLE IF EXISTS keysmith_key_scopes;
DROP TABLE IF EXISTS keysmith_scopes;
`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "create_usage",
			Version: "20240101000004",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS keysmith_usage (
    id          TEXT PRIMARY KEY,
    key_id      TEXT NOT NULL REFERENCES keysmith_keys(id) ON DELETE CASCADE,
    tenant_id   TEXT NOT NULL,
    endpoint    TEXT NOT NULL,
    method      TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    ip_address  TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    latency_ms  INTEGER NOT NULL DEFAULT 0,
    metadata    TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_keysmith_usage_key ON keysmith_usage (key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_keysmith_usage_tenant ON keysmith_usage (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS keysmith_usage_agg (
    key_id        TEXT NOT NULL,
    tenant_id     TEXT NOT NULL,
    period        TEXT NOT NULL,
    period_start  TEXT NOT NULL,
    request_count INTEGER NOT NULL DEFAULT 0,
    error_count   INTEGER NOT NULL DEFAULT 0,
    total_latency INTEGER NOT NULL DEFAULT 0,
    p50_latency   INTEGER NOT NULL DEFAULT 0,
    p99_latency   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (key_id, period, period_start)
);

CREATE INDEX IF NOT EXISTS idx_keysmith_usage_agg_tenant ON keysmith_usage_agg (tenant_id, period, period_start DESC);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
DROP TABLE IF EXISTS keysmith_usage_agg;
DROP TABLE IF EXISTS keysmith_usage;
`)
				return err
			},
		},
		&migrate.Migration{
			Name:    "create_rotations",
			Version: "20240101000005",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
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
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS keysmith_rotations`)
				return err
			},
		},
	)
}
