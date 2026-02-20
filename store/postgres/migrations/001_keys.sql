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
