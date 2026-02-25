package postgres

import (
	"context"

	"github.com/xraph/grove/migrate"
)

// Migrations is the grove migration group for the Keysmith store.
// It can be registered with the grove extension for orchestrated migration
// management (locking, version tracking, rollback support).
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
    description     TEXT,
    prefix          TEXT NOT NULL,
    hint            TEXT NOT NULL,
    key_hash        TEXT NOT NULL UNIQUE,
    environment     TEXT NOT NULL DEFAULT 'live',
    state           TEXT NOT NULL DEFAULT 'active',
    policy_id       TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_by      TEXT,
    expires_at      TIMESTAMPTZ,
    last_used_at    TIMESTAMPTZ,
    rotated_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_keysmith_keys_tenant ON keysmith_keys (tenant_id);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_hash ON keysmith_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_state ON keysmith_keys (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_env ON keysmith_keys (tenant_id, environment);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_expires ON keysmith_keys (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_policy ON keysmith_keys (policy_id) WHERE policy_id IS NOT NULL;
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
    description       TEXT,
    rate_limit        INT NOT NULL DEFAULT 0,
    rate_limit_window BIGINT NOT NULL DEFAULT 0,
    burst_limit       INT NOT NULL DEFAULT 0,
    allowed_scopes    JSONB NOT NULL DEFAULT '[]',
    allowed_ips       JSONB NOT NULL DEFAULT '[]',
    allowed_origins   JSONB NOT NULL DEFAULT '[]',
    allowed_methods   JSONB NOT NULL DEFAULT '[]',
    allowed_paths     JSONB NOT NULL DEFAULT '[]',
    max_key_lifetime  BIGINT NOT NULL DEFAULT 0,
    rotation_period   BIGINT NOT NULL DEFAULT 0,
    grace_period      BIGINT NOT NULL DEFAULT 86400000000000,
    daily_quota       BIGINT NOT NULL DEFAULT 0,
    monthly_quota     BIGINT NOT NULL DEFAULT 0,
    metadata          JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

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
    description TEXT,
    parent      TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

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
    status_code INT NOT NULL,
    ip_address  TEXT,
    user_agent  TEXT,
    latency_ms  BIGINT NOT NULL DEFAULT 0,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_keysmith_usage_key ON keysmith_usage (key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_keysmith_usage_tenant ON keysmith_usage (tenant_id, created_at DESC);

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
    grace_ttl_ms BIGINT NOT NULL DEFAULT 86400000,
    grace_ends   TIMESTAMPTZ NOT NULL,
    rotated_by   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_keysmith_rotations_key ON keysmith_rotations (key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_keysmith_rotations_grace ON keysmith_rotations (grace_ends) WHERE grace_ends > NOW();
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

// migrationSQL contains the raw SQL statements executed by the Store.Migrate
// method for direct migration without the grove orchestrator. Each entry
// corresponds to one of the original embedded SQL migration files.
var migrationSQL = []string{
	// 001_keys.sql
	`CREATE TABLE IF NOT EXISTS keysmith_keys (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    app_id          TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT,
    prefix          TEXT NOT NULL,
    hint            TEXT NOT NULL,
    key_hash        TEXT NOT NULL UNIQUE,
    environment     TEXT NOT NULL DEFAULT 'live',
    state           TEXT NOT NULL DEFAULT 'active',
    policy_id       TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_by      TEXT,
    expires_at      TIMESTAMPTZ,
    last_used_at    TIMESTAMPTZ,
    rotated_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_keysmith_keys_tenant ON keysmith_keys (tenant_id);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_hash ON keysmith_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_state ON keysmith_keys (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_env ON keysmith_keys (tenant_id, environment);
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_expires ON keysmith_keys (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_keysmith_keys_policy ON keysmith_keys (policy_id) WHERE policy_id IS NOT NULL;`,

	// 002_policies.sql
	`CREATE TABLE IF NOT EXISTS keysmith_policies (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL,
    app_id            TEXT NOT NULL,
    name              TEXT NOT NULL,
    description       TEXT,
    rate_limit        INT NOT NULL DEFAULT 0,
    rate_limit_window BIGINT NOT NULL DEFAULT 0,
    burst_limit       INT NOT NULL DEFAULT 0,
    allowed_scopes    JSONB NOT NULL DEFAULT '[]',
    allowed_ips       JSONB NOT NULL DEFAULT '[]',
    allowed_origins   JSONB NOT NULL DEFAULT '[]',
    allowed_methods   JSONB NOT NULL DEFAULT '[]',
    allowed_paths     JSONB NOT NULL DEFAULT '[]',
    max_key_lifetime  BIGINT NOT NULL DEFAULT 0,
    rotation_period   BIGINT NOT NULL DEFAULT 0,
    grace_period      BIGINT NOT NULL DEFAULT 86400000000000,
    daily_quota       BIGINT NOT NULL DEFAULT 0,
    monthly_quota     BIGINT NOT NULL DEFAULT 0,
    metadata          JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_keysmith_policies_tenant ON keysmith_policies (tenant_id);`,

	// 003_scopes.sql
	`CREATE TABLE IF NOT EXISTS keysmith_scopes (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT,
    parent      TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_keysmith_scopes_tenant ON keysmith_scopes (tenant_id);
CREATE INDEX IF NOT EXISTS idx_keysmith_scopes_parent ON keysmith_scopes (tenant_id, parent);

CREATE TABLE IF NOT EXISTS keysmith_key_scopes (
    key_id    TEXT NOT NULL REFERENCES keysmith_keys(id) ON DELETE CASCADE,
    scope_id  TEXT NOT NULL REFERENCES keysmith_scopes(id) ON DELETE CASCADE,
    PRIMARY KEY (key_id, scope_id)
);`,

	// 004_usage.sql
	`CREATE TABLE IF NOT EXISTS keysmith_usage (
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

CREATE INDEX IF NOT EXISTS idx_keysmith_usage_agg_tenant ON keysmith_usage_agg (tenant_id, period, period_start DESC);`,

	// 005_rotations.sql
	`CREATE TABLE IF NOT EXISTS keysmith_rotations (
    id           TEXT PRIMARY KEY,
    key_id       TEXT NOT NULL REFERENCES keysmith_keys(id) ON DELETE CASCADE,
    tenant_id    TEXT NOT NULL,
    old_key_hash TEXT NOT NULL,
    new_key_hash TEXT NOT NULL,
    reason       TEXT NOT NULL,
    grace_ttl_ms BIGINT NOT NULL DEFAULT 86400000,
    grace_ends   TIMESTAMPTZ NOT NULL,
    rotated_by   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_keysmith_rotations_key ON keysmith_rotations (key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_keysmith_rotations_grace ON keysmith_rotations (grace_ends) WHERE grace_ends > NOW();`,
}
