package keysmith_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
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

func TestCreateScopeRefusesADuplicateNameInTheTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		require.NoError(t, eng.CreateScope(ctx, &scope.Scope{Name: "read"}))
		require.ErrorIs(t, eng.CreateScope(ctx, &scope.Scope{Name: "read"}), keysmith.ErrScopeNameTaken)
		require.NoError(t, eng.CreateScope(keysmith.WithTenant(context.Background(), "app1", "t2"), &scope.Scope{Name: "read"}))
	})
}

func TestDeleteScopeRefusesWhileChildrenNameIt(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		parent := &scope.Scope{Name: "billing"}
		require.NoError(t, eng.CreateScope(ctx, parent))
		child := &scope.Scope{Name: "billing:read", Parent: "billing"}
		require.NoError(t, eng.CreateScope(ctx, child))
		// Another tenant's child of the same name does not count.
		require.NoError(t, eng.CreateScope(keysmith.WithTenant(context.Background(), "app1", "t2"), &scope.Scope{Name: "x", Parent: "billing"}))

		require.ErrorIs(t, eng.DeleteScope(ctx, parent.ID), keysmith.ErrScopeHasChildren)
		require.NoError(t, eng.DeleteScope(ctx, child.ID))
		require.NoError(t, eng.DeleteScope(ctx, parent.ID))
		require.ErrorIs(t, eng.DeleteScope(ctx, parent.ID), keysmith.ErrScopeNotFound)
	})
}

func TestDeleteScopeRefusesWhileAPolicyAllowsIt(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		read := &scope.Scope{Name: "read"}
		require.NoError(t, eng.CreateScope(ctx, read))
		createScope(t, eng, ctx, "write")
		pol := &policy.Policy{Name: "readonly", AllowedScopes: []string{"write", "read"}}
		require.NoError(t, eng.CreatePolicy(ctx, pol))
		// Another tenant's policy naming the same scope does not count.
		other := keysmith.WithTenant(context.Background(), "app1", "t2")
		require.NoError(t, eng.CreatePolicy(other, &policy.Policy{Name: "theirs", AllowedScopes: []string{"read"}}))

		require.ErrorIs(t, eng.DeleteScope(ctx, read.ID), keysmith.ErrScopeAllowedByPolicy)
		_, err := s.Scopes().Get(context.Background(), read.ID)
		require.NoError(t, err, "a refused delete leaves the scope")

		pol.AllowedScopes = []string{"write"}
		require.NoError(t, eng.UpdatePolicy(ctx, pol))
		require.NoError(t, eng.DeleteScope(ctx, read.ID))
	})
}

// The check pages the tenant's policies, so a policy past the first page
// still blocks.
func TestDeleteScopeFindsAnAllowingPolicyPastTheFirstPage(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		read := &scope.Scope{Name: "read"}
		require.NoError(t, eng.CreateScope(ctx, read))
		for i := range 150 {
			require.NoError(t, eng.CreatePolicy(ctx, &policy.Policy{Name: "p" + strconv.Itoa(i)}))
		}
		require.NoError(t, eng.CreatePolicy(ctx, &policy.Policy{Name: "last", AllowedScopes: []string{"read"}}))
		require.ErrorIs(t, eng.DeleteScope(ctx, read.ID), keysmith.ErrScopeAllowedByPolicy)
	})
}

// Children are checked first, so a scope with both answers ErrScopeHasChildren.
func TestDeleteScopeChecksChildrenBeforePolicies(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		eng, ctx := newEngine(t, s)
		parent := &scope.Scope{Name: "billing"}
		require.NoError(t, eng.CreateScope(ctx, parent))
		require.NoError(t, eng.CreateScope(ctx, &scope.Scope{Name: "billing:read", Parent: "billing"}))
		require.NoError(t, eng.CreatePolicy(ctx, &policy.Policy{Name: "p", AllowedScopes: []string{"billing"}}))
		require.ErrorIs(t, eng.DeleteScope(ctx, parent.ID), keysmith.ErrScopeHasChildren)
	})
}

// failingScopes wraps a scope store so AssignToKey fails.
type failingScopes struct {
	scope.Store
	err error
}

func (f failingScopes) AssignToKey(context.Context, id.KeyID, []string) error { return f.err }

// failingKeys wraps a key store so Delete fails.
type failingKeys struct {
	key.Store
	err error
}

func (f failingKeys) Delete(context.Context, id.KeyID) error { return f.err }

// failingStore swaps in the wrapped stores and leaves the rest alone.
type failingStore struct {
	store.Store
	scopes scope.Store
	keys   key.Store
}

func (f failingStore) Scopes() scope.Store {
	if f.scopes != nil {
		return f.scopes
	}
	return f.Store.Scopes()
}

func (f failingStore) Keys() key.Store {
	if f.keys != nil {
		return f.keys
	}
	return f.Store.Keys()
}

// createdRecorder counts created hook calls.
type createdRecorder struct{ created int }

func (*createdRecorder) Name() string { return "created-recorder" }

func (r *createdRecorder) OnKeyCreated(context.Context, *key.Key) error {
	r.created++
	return nil
}

func TestCreateKeyDeletesTheKeyWhenScopeAssignmentFails(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		assignErr := errors.New("assign boom")
		rec := &createdRecorder{}
		eng, ctx := newEngine(t, failingStore{Store: s, scopes: failingScopes{Store: s.Scopes(), err: assignErr}}, keysmith.WithExtension(rec))
		createScope(t, eng, ctx, "read")

		_, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, Scopes: []string{"read"}})
		require.ErrorIs(t, err, assignErr)

		n, err := s.Keys().Count(context.Background(), &key.ListFilter{TenantID: "t1"})
		require.NoError(t, err)
		assert.Zero(t, n, "the key must not outlive its failed scope assignment")
		assert.Zero(t, rec.created, "no created hook fires for a key that was deleted")
	})
}

func TestCreateKeyReportsBothErrorsWhenTheCleanupFailsToo(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		assignErr := errors.New("assign boom")
		deleteErr := errors.New("delete boom")
		rec := &createdRecorder{}
		eng, ctx := newEngine(t, failingStore{
			Store:  s,
			scopes: failingScopes{Store: s.Scopes(), err: assignErr},
			keys:   failingKeys{Store: s.Keys(), err: deleteErr},
		}, keysmith.WithExtension(rec))
		createScope(t, eng, ctx, "read")

		_, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, Scopes: []string{"read"}})
		require.ErrorIs(t, err, assignErr)
		require.ErrorIs(t, err, deleteErr)
		assert.Zero(t, rec.created)
	})
}
