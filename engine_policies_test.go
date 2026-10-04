package keysmith_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/store"
)

func TestCreatePolicyRefusesADuplicateNameInTheTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		require.NoError(t, eng.CreatePolicy(ctx, &policy.Policy{Name: "Standard"}))
		err := eng.CreatePolicy(ctx, &policy.Policy{Name: "Standard"})
		require.ErrorIs(t, err, keysmith.ErrPolicyNameTaken)
		// Another tenant may use the same name.
		require.NoError(t, eng.CreatePolicy(keysmith.WithTenant(context.Background(), "app1", "t2"), &policy.Policy{Name: "Standard"}))
	})
}

func TestUpdatePolicyRefusesARenameIntoATakenName(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		a := &policy.Policy{Name: "A"}
		b := &policy.Policy{Name: "B"}
		require.NoError(t, eng.CreatePolicy(ctx, a))
		require.NoError(t, eng.CreatePolicy(ctx, b))
		b.Name = "A"
		require.ErrorIs(t, eng.UpdatePolicy(ctx, b), keysmith.ErrPolicyNameTaken)
		// Keeping its own name is not a clash.
		a.Description = "changed"
		require.NoError(t, eng.UpdatePolicy(ctx, a))
	})
}
