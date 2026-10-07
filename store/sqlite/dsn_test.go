package sqlite

import (
	"context"
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
