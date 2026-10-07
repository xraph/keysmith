package sqlite_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
)

// ValidateKey writes the key's last-used time from a goroutine, so the write
// a caller makes right after it races that update for sqlite's one write
// lock. Every write here has to wait its turn, never fail with SQLITE_BUSY.
// AssignScopes covers a transaction that reads before it writes.
func TestWritesRightAfterValidateNeverHitBusy(t *testing.T) {
	ctx := keysmith.WithTenant(context.Background(), "app", "t1")
	s := storetest.OpenSQLite(t)
	require.NoError(t, s.Migrate(ctx))

	eng, err := keysmith.NewEngine(keysmith.WithStore(s))
	require.NoError(t, err)
	require.NoError(t, eng.CreateScope(ctx, &scope.Scope{Name: "read"}))
	created, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive})
	require.NoError(t, err)
	keyID, raw := created.Key.ID, created.RawKey

	validate := func(i int) {
		t.Helper()
		_, vErr := eng.ValidateKey(ctx, raw)
		require.NoError(t, vErr, "iteration %d: validate", i)
	}
	for i := range 100 {
		validate(i)
		require.NoError(t, eng.AssignScopes(ctx, keyID, []string{"read"}), "iteration %d: assign", i)
		validate(i)
		require.NoError(t, eng.RemoveScopes(ctx, keyID, []string{"read"}), "iteration %d: remove", i)
		validate(i)
		rotated, rErr := eng.RotateKey(ctx, keyID, rotation.ReasonManual)
		require.NoError(t, rErr, "iteration %d: rotate", i)
		validate(i)
		_, gErr := eng.EndGrace(ctx, keyID)
		require.NoError(t, gErr, "iteration %d: end grace", i)
		raw = rotated.RawKey
	}
}
