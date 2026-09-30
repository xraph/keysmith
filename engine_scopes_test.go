package keysmith_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
)

func createScope(t *testing.T, eng *keysmith.Engine, ctx context.Context, name string) { //nolint:revive // the signature is shared by the tests in this file
	t.Helper()
	require.NoError(t, eng.CreateScope(ctx, &scope.Scope{Name: name}))
}

func TestCreateKeyWithUnknownScopeWritesNothing(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		_, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, Scopes: []string{"nope"}})
		assert.ErrorIs(t, err, keysmith.ErrScopeNotFound)
		n, err := s.Keys().Count(context.Background(), &key.ListFilter{TenantID: "t1"})
		require.NoError(t, err)
		assert.Zero(t, n, "a failed create must not leave a key behind")
	})
}

func TestScopeFromAnotherTenantIsNotFound(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		other := keysmith.WithTenant(context.Background(), "app", "t2")
		createScope(t, eng, other, "read")
		r := mustCreate(t, eng, ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive})
		assert.ErrorIs(t, eng.AssignScopes(ctx, r.Key.ID, []string{"read"}), keysmith.ErrScopeNotFound)
		got, err := s.Scopes().ListByKey(context.Background(), r.Key.ID)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestPolicyAllowedScopesAreEnforced(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		createScope(t, eng, ctx, "read")
		createScope(t, eng, ctx, "write")
		pol := &policy.Policy{Name: "readonly", AllowedScopes: []string{"read"}}
		require.NoError(t, eng.CreatePolicy(ctx, pol))

		_, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID, Scopes: []string{"write"}})
		assert.ErrorIs(t, err, keysmith.ErrScopeNotAllowed)

		r, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID, Scopes: []string{"read"}})
		require.NoError(t, err)
		assert.ErrorIs(t, eng.AssignScopes(ctx, r.Key.ID, []string{"write"}), keysmith.ErrScopeNotAllowed)
	})
}

func TestEmptyAllowedScopesAllowsAnyExistingScope(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		createScope(t, eng, ctx, "write")
		pol := &policy.Policy{Name: "open"}
		require.NoError(t, eng.CreatePolicy(ctx, pol))
		_, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID, Scopes: []string{"write"}})
		assert.NoError(t, err)
	})
}
