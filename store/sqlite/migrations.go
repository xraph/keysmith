package sqlite

import "github.com/xraph/grove/migrate"

// Migrations is the grove migration group for the Keysmith SQLite store.
// Each migration lives in a file of its own, named
// <14 digit version>_<name>.go, and registers itself with Migrations.Add.
//
// Store.Migrate runs the group through grove's orchestrator, which takes the
// migration lock first. A host that runs its migrations centrally can register
// this group with its own orchestrator instead and turn Store.Migrate off (the
// Forge extension's WithDisableMigrate).
//
// The versions and names match the ones keysmith has always recorded in
// grove_migrations, so a database that already applied them skips them.
var Migrations = migrate.NewGroup("keysmith")
