package postgres

import (
	"context"

	"github.com/xraph/grove/migrate"
)

// Adds the old and new key hints to rotation records, and an index for
// looking a rotation up by the hash of the key it replaced.
func init() {
	Migrations.Add(
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
ALTER TABLE keysmith_rotations ADD COLUMN IF NOT EXISTS old_hint TEXT NOT NULL DEFAULT '';
ALTER TABLE keysmith_rotations ADD COLUMN IF NOT EXISTS new_hint TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_keysmith_rotations_old_hash ON keysmith_rotations (old_key_hash);
`)
			return err
		},
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `
DROP INDEX IF EXISTS idx_keysmith_rotations_old_hash;
ALTER TABLE keysmith_rotations DROP COLUMN IF EXISTS old_hint;
ALTER TABLE keysmith_rotations DROP COLUMN IF EXISTS new_hint;
`)
			return err
		},
	)
}
