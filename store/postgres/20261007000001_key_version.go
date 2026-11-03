package postgres

import (
	"context"

	"github.com/xraph/grove/migrate"
)

// Adds the write counter that version-checked key updates compare. Existing
// keys start at 0.
func init() {
	Migrations.Add(
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `ALTER TABLE keysmith_keys ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 0`)
			return err
		},
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `ALTER TABLE keysmith_keys DROP COLUMN IF EXISTS version`)
			return err
		},
	)
}
