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
