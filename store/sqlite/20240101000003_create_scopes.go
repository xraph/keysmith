package sqlite

import (
	"context"

	"github.com/xraph/grove/migrate"
)

func init() {
	Migrations.Add(
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
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
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
DROP TABLE IF EXISTS keysmith_key_scopes;
DROP TABLE IF EXISTS keysmith_scopes;
`)
			return err
		},
	)
}
