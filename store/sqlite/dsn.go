package sqlite

import (
	"context"
	"fmt"
	"net/url"
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
// kept, and so is any of the three settings it already sets: the caller's
// _txlock, busy_timeout or foreign_keys wins, and DSN adds only the ones
// that are missing. If you open the database some other way, such as from
// a grove extension's config, put the same three parameters on that DSN.
func DSN(path string) string {
	var hasTxLock, hasBusy, hasFK bool
	if i := strings.IndexByte(path, '?'); i >= 0 {
		// A query that doesn't parse is left for modernc to reject at Open.
		q, _ := url.ParseQuery(path[i+1:])
		_, hasTxLock = q["_txlock"]
		for _, p := range q["_pragma"] {
			p = strings.ToLower(strings.TrimSpace(p))
			hasBusy = hasBusy || strings.HasPrefix(p, "busy_timeout")
			hasFK = hasFK || strings.HasPrefix(p, "foreign_keys")
		}
	}

	var add []string
	if !hasBusy {
		add = append(add, fmt.Sprintf("_pragma=busy_timeout(%d)", BusyTimeout.Milliseconds()))
	}
	if !hasFK {
		add = append(add, "_pragma=foreign_keys(1)")
	}
	if !hasTxLock {
		add = append(add, "_txlock=immediate")
	}
	if len(add) == 0 {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + strings.Join(add, "&")
}

// BusyTimeout reads PRAGMA busy_timeout on one pooled connection. Grove's
// driver never sets a busy timeout, so zero means the DSN didn't set one
// either, and then no connection in the pool has it. The extension reads it
// at startup to warn about a database opened without DSN.
func (s *Store) BusyTimeout(ctx context.Context) (time.Duration, error) {
	var ms int64
	if err := s.sdb.NewRaw("PRAGMA busy_timeout").Scan(ctx, &ms); err != nil {
		return 0, fmt.Errorf("keysmith/sqlite: read busy_timeout: %w", err)
	}
	return time.Duration(ms) * time.Millisecond, nil
}
