package sqlite

import (
	"fmt"
	"strings"
	"time"
)

// BusyTimeout is how long a connection opened with DSN waits for another
// connection's write lock before it gives up with SQLITE_BUSY.
const BusyTimeout = 5 * time.Second

// DSN returns the data source name to open the database at path with, for
// sqlitedriver.SqliteDB.Open. Open the database you hand to New with it:
//
//	sdb := sqlitedriver.New()
//	err := sdb.Open(ctx, sqlite.DSN("keysmith.db"))
//
// database/sql pools connections and every pooled connection has its own
// settings, so the DSN is the one place that reaches all of them. Grove's
// driver runs PRAGMA foreign_keys=ON once at Open, on whichever connection
// it gets, and sets no busy timeout. DSN adds three settings, applied by
// modernc.org/sqlite to each connection it opens:
//
//   - busy_timeout: a write waits up to BusyTimeout for the write lock. The
//     engine's ValidateKey updates last_used_at in the background, so the
//     next write regularly finds the lock taken.
//   - foreign_keys: ON DELETE CASCADE holds on every connection, so deleting
//     a key or a scope never leaves keysmith_key_scopes rows behind.
//   - _txlock=immediate: transactions take the write lock at BEGIN. SQLite
//     skips the busy timeout when a transaction that has already read tries
//     to write, which AssignToKey does, and fails at once with SQLITE_BUSY.
//     Read-only transactions still begin deferred.
//
// path is a file name or a file: URI. Query parameters already on it are
// kept. If you open the database some other way, such as from a grove
// extension's config, put the same three parameters on that DSN.
func DSN(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%s_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)&_txlock=immediate",
		path, sep, BusyTimeout.Milliseconds())
}
