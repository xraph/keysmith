package extension

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	log "github.com/xraph/go-utils/log"
	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/sqlitedriver"

	sqlitestore "github.com/xraph/keysmith/store/sqlite"
)

func openGroveSQLite(t *testing.T, dsn string) *grove.DB {
	t.Helper()
	sdb := sqlitedriver.New()
	require.NoError(t, sdb.Open(context.Background(), dsn))
	db, err := grove.Open(sdb)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func sqliteWarnings(tl *log.TestLogger) []string {
	var out []string
	for _, l := range tl.GetLogsByLevel("WARN") {
		if strings.Contains(l.Message, "sqlite") {
			out = append(out, l.Message)
		}
	}
	return out
}

// A grove database opened from a bare path has no busy timeout, so the
// extension says so at startup and names the fix. Startup carries on.
func TestSQLiteGroveDatabaseWithoutABusyTimeoutWarns(t *testing.T) {
	tl, ok := log.NewTestLogger().(*log.TestLogger)
	require.True(t, ok)
	db := openGroveSQLite(t, filepath.Join(t.TempDir(), "keysmith.db"))

	s, err := New().buildStoreFromGroveDB(context.Background(), db, tl)
	require.NoError(t, err)
	require.NotNil(t, s)

	warns := sqliteWarnings(tl)
	require.Len(t, warns, 1)
	assert.Contains(t, warns[0], "sqlite.DSN")
	assert.Contains(t, warns[0], "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate")
}

func TestSQLiteGroveDatabaseOpenedWithDSNIsQuiet(t *testing.T) {
	tl, ok := log.NewTestLogger().(*log.TestLogger)
	require.True(t, ok)
	db := openGroveSQLite(t, sqlitestore.DSN(filepath.Join(t.TempDir(), "keysmith.db")))

	_, err := New().buildStoreFromGroveDB(context.Background(), db, tl)
	require.NoError(t, err)
	assert.Empty(t, sqliteWarnings(tl))
}
