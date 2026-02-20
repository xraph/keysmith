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
