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
