package sqlite

import (
	"context"

	"github.com/xraph/grove/migrate"
)

// Closes the grace windows of rotations recorded before the grace fix.
// RotateKey then recorded a window on every rotation but killed the old key at
// once, and those rows have no old hint. Both columns hold UTC text in one
// format, which orders as time. The closed windows were never honoured, so
// there's nothing for Down to restore.
//
// This one uses Define, not Add. Its recorded name is
// close_legacy_grace_windows, and a file ending in _windows.go only builds on
// Windows, so Add can't take the name from this file.
func init() {
	Migrations.MustRegister(migrate.Define("20260930000003", "close_legacy_grace_windows",
		func(ctx context.Context, db *migrate.DB) error {
			_, err := db.Exec(ctx, `UPDATE keysmith_rotations SET grace_ends = created_at WHERE old_hint = '' AND grace_ends > created_at`)
			return err
		},
		nil,
	))
}
