package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/grove/driver"
	"github.com/xraph/grove/drivers/sqlitedriver"
)

// Each pooled connection runs its own pragmas. Holding several transactions
// at once pins several connections, so a setting applied to one connection
// only (the way grove's driver sets foreign_keys at Open) shows up as a 0 on
// the others.
func TestEveryConnectionHasForeignKeysAndABusyTimeout(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	const held = 8
	txs := make([]*sqlitedriver.SqliteTx, 0, held)
	t.Cleanup(func() {
		for _, tx := range txs {
			_ = tx.Rollback()
		}
	})
	for range held {
		tx, err := s.sdb.BeginTxQuery(ctx, &driver.TxOptions{ReadOnly: true})
		require.NoError(t, err)
		txs = append(txs, tx)
	}

	for i, tx := range txs {
		var fk, busy int
		require.NoError(t, tx.NewRaw("PRAGMA foreign_keys").Scan(ctx, &fk))
		require.NoError(t, tx.NewRaw("PRAGMA busy_timeout").Scan(ctx, &busy))
		assert.Equal(t, 1, fk, "connection %d: foreign_keys", i)
		assert.EqualValues(t, BusyTimeout.Milliseconds(), busy, "connection %d: busy_timeout in ms", i)
	}
}

func TestDSNKeepsTheQueryAlreadyOnThePath(t *testing.T) {
	const settings = "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	assert.Equal(t, "keysmith.db?"+settings, DSN("keysmith.db"))
	assert.Equal(t, "file:keysmith.db?mode=rwc&"+settings, DSN("file:keysmith.db?mode=rwc"))
}

// A setting the caller already put on the path wins, and DSN adds only the
// ones that are missing.
func TestDSNLeavesTheCallersOwnSettingsAlone(t *testing.T) {
	cases := []struct{ in, want string }{
		{"k.db?_txlock=deferred", "k.db?_txlock=deferred&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"},
		{"k.db?_pragma=busy_timeout(100)&_pragma=foreign_keys(0)", "k.db?_pragma=busy_timeout(100)&_pragma=foreign_keys(0)&_txlock=immediate"},
		{"file:k.db?_pragma=Busy_Timeout=100", "file:k.db?_pragma=Busy_Timeout=100&_pragma=foreign_keys(1)&_txlock=immediate"},
		{"k.db?_pragma=busy_timeout(1)&_pragma=foreign_keys(1)&_txlock=exclusive", "k.db?_pragma=busy_timeout(1)&_pragma=foreign_keys(1)&_txlock=exclusive"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, DSN(c.in), c.in)
	}
}

func TestDSNKeepsTheCallersBusyTimeoutOnTheConnection(t *testing.T) {
	ctx := context.Background()
	sdb := sqlitedriver.New()
	require.NoError(t, sdb.Open(ctx, DSN(filepath.Join(t.TempDir(), "keysmith.db")+"?_pragma=busy_timeout(100)")))
	t.Cleanup(func() { _ = sdb.Close() })
	var busy, fk int
	require.NoError(t, sdb.NewRaw("PRAGMA busy_timeout").Scan(ctx, &busy))
	require.NoError(t, sdb.NewRaw("PRAGMA foreign_keys").Scan(ctx, &fk))
	assert.Equal(t, 100, busy)
	assert.Equal(t, 1, fk)
}
