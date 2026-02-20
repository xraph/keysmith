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
