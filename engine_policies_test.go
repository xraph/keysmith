package keysmith_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
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

func TestDeletePolicyIgnoresRevokedKeys(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		pol := &policy.Policy{Name: "Retired"}
		require.NoError(t, eng.CreatePolicy(ctx, pol))
		res, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvTest, PolicyID: &pol.ID})
		require.NoError(t, err)

		require.ErrorIs(t, eng.DeletePolicy(ctx, pol.ID), keysmith.ErrPolicyInUse) // active key blocks
		require.NoError(t, eng.SuspendKey(ctx, res.Key.ID))
		require.ErrorIs(t, eng.DeletePolicy(ctx, pol.ID), keysmith.ErrPolicyInUse) // suspended blocks
		require.NoError(t, eng.RevokeKey(ctx, res.Key.ID, "done"))
		require.NoError(t, eng.DeletePolicy(ctx, pol.ID)) // revoked does not

		_, err = eng.GetPolicy(ctx, pol.ID)
		require.ErrorIs(t, err, keysmith.ErrPolicyNotFound)
		k, err := eng.GetKey(ctx, res.Key.ID)
		require.NoError(t, err)
		require.NotNil(t, k.PolicyID, "a revoked key keeps the id of the policy it used")
	})
}

func TestDeletePolicyIgnoresARevokedKeyWhoseStateWasReset(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		pol := &policy.Policy{Name: "Retired"}
		require.NoError(t, eng.CreatePolicy(ctx, pol))
		res, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvTest, PolicyID: &pol.ID})
		require.NoError(t, err)
		require.NoError(t, eng.RevokeKey(ctx, res.Key.ID, "done"))

		// A stale full-row write can put State back to active, but RevokedAt
		// stays set and the key stays revoked.
		require.NoError(t, s.Keys().UpdateState(context.Background(), res.Key.ID, key.StateActive))
		k, err := s.Keys().Get(context.Background(), res.Key.ID)
		require.NoError(t, err)
		require.Equal(t, key.StateActive, k.State)
		require.NotNil(t, k.RevokedAt)

		require.NoError(t, eng.DeletePolicy(ctx, pol.ID))
	})
}
