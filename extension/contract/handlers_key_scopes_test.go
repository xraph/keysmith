package contract

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/store"
)

func scopesReq(keyID id.KeyID, names ...string) keysScopesRequest {
	return keysScopesRequest{ID: keyID.String(), Scopes: names}
}

func scopesAssign(deps Deps, in keysScopesRequest) (keyResponse, error) {
	return keysScopesAssignHandler(deps)(context.Background(), in, principal())
}

func scopesRemove(deps Deps, in keysScopesRequest) (keyResponse, error) {
	return keysScopesRemoveHandler(deps)(context.Background(), in, principal())
}

func TestKeysScopesAssignShowsSortedInTheNextDetail(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		mkScope(t, eng, "t1", "write", "")
		mkScope(t, eng, "t1", "admin", "")
		created := create(t, eng, "t1", nil)

		out, err := scopesAssign(deps, scopesReq(created.Key.ID, "write", "admin"))
		require.NoError(t, err)
		assert.Equal(t, created.Key.ID.String(), out.Key.ID)
		assert.Equal(t, []string{"admin", "write"}, out.Key.Scopes)

		// Assigning one the key already has changes nothing.
		out, err = scopesAssign(deps, scopesReq(created.Key.ID, "read", "write"))
		require.NoError(t, err)
		assert.Equal(t, []string{"admin", "read", "write"}, out.Key.Scopes)

		detail := stateDetail(t, deps, created.Key.ID)
		assert.Equal(t, out.Key, detail.Key)
		assert.Equal(t, []string{"admin", "read", "write"}, detail.Key.Scopes)
	})
}

func TestKeysScopesAssignTrimsAndDeduplicates(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		created := create(t, eng, "t1", nil)

		out, err := scopesAssign(deps, scopesReq(created.Key.ID, " read ", "read", "", "  "))
		require.NoError(t, err)
		assert.Equal(t, []string{"read"}, out.Key.Scopes)
	})
}

func TestKeysScopesNeedAtLeastOneScope(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		created := create(t, eng, "t1", nil)

		for _, names := range [][]string{nil, {}, {""}, {"  ", "\t"}} {
			_, err := scopesAssign(deps, scopesReq(created.Key.ID, names...))
			assert.Equal(t, "scopes must name at least one scope", badRequestMessage(t, err))
			_, err = scopesRemove(deps, scopesReq(created.Key.ID, names...))
			assert.Equal(t, "scopes must name at least one scope", badRequestMessage(t, err))
		}
	})
}

func TestKeysScopesAssignScopeErrors(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		mkScope(t, eng, "t1", "write", "")
		mkScope(t, eng, "t2", "theirs", "")
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "readonly", AllowedScopes: []string{"read"}})
		plain := create(t, eng, "t1", nil)
		bound := create(t, eng, "t1", &keysmith.CreateKeyInput{
			Name: "bound", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID,
		})

		// "zzz" sorts after "read", so "read" is checked first: a refusal
		// that still assigned it would show below.
		_, err := scopesAssign(deps, scopesReq(plain.Key.ID, "read", "zzz"))
		assert.Equal(t, `scope "zzz" does not exist in this tenant`, badRequestMessage(t, err))

		_, err = scopesAssign(deps, scopesReq(plain.Key.ID, "theirs"))
		assert.Equal(t, `scope "theirs" does not exist in this tenant`, badRequestMessage(t, err))

		_, err = scopesAssign(deps, scopesReq(bound.Key.ID, "read", "write"))
		assert.Equal(t, "a scope is outside this policy's allowed scopes", badRequestMessage(t, err))

		// A refused assignment leaves nothing behind, not even the valid name.
		assert.Empty(t, stateDetail(t, deps, plain.Key.ID).Key.Scopes)
		assert.Empty(t, stateDetail(t, deps, bound.Key.ID).Key.Scopes)

		out, err := scopesAssign(deps, scopesReq(bound.Key.ID, "read"))
		require.NoError(t, err)
		assert.Equal(t, []string{"read"}, out.Key.Scopes)
	})
}

func TestKeysScopesRemove(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		mkScope(t, eng, "t1", "write", "")
		mkScope(t, eng, "t1", "admin", "")
		created := create(t, eng, "t1", &keysmith.CreateKeyInput{
			Name: "k", Prefix: "sk", Environment: key.EnvLive, Scopes: []string{"read", "write"},
		})

		out, err := scopesRemove(deps, scopesReq(created.Key.ID, "write"))
		require.NoError(t, err)
		assert.Equal(t, []string{"read"}, out.Key.Scopes)
		assert.Equal(t, out.Key, stateDetail(t, deps, created.Key.ID).Key)

		// Removing one the key does not have, or one that does not exist, is
		// not an error.
		out, err = scopesRemove(deps, scopesReq(created.Key.ID, "admin", "nope", "write"))
		require.NoError(t, err)
		assert.Equal(t, []string{"read"}, out.Key.Scopes)

		out, err = scopesRemove(deps, scopesReq(created.Key.ID, "read"))
		require.NoError(t, err)
		assert.Equal(t, []string{}, out.Key.Scopes)
	})
}

func TestKeysScopesRefuseAnotherTenantsKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		theirs := create(t, eng, "t2", nil)

		_, err := scopesAssign(deps, scopesReq(theirs.Key.ID, "read"))
		require.Error(t, err)
		_, err = scopesRemove(deps, scopesReq(theirs.Key.ID, "read"))
		require.Error(t, err)
	})
}

func TestKeysScopesRefuseARevokedKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		created := create(t, eng, "t1", &keysmith.CreateKeyInput{
			Name: "k", Prefix: "sk", Environment: key.EnvLive, Scopes: []string{"read"},
		})
		_, err := keysRevokeHandler(deps)(context.Background(), stateRevokeReq(created.Key.ID, "leaked"), principal())
		require.NoError(t, err)

		_, err = scopesAssign(deps, scopesReq(created.Key.ID, "read"))
		assert.Equal(t, "a revoked key's scopes cannot be changed", stateConflictMessage(t, err))
		_, err = scopesRemove(deps, scopesReq(created.Key.ID, "read"))
		assert.Equal(t, "a revoked key's scopes cannot be changed", stateConflictMessage(t, err))
		assert.Equal(t, []string{"read"}, stateDetail(t, deps, created.Key.ID).Key.Scopes)
	})
}

// A stale write can put the stored state back to active while RevokedAt stays
// set. Every state command goes by the effective state, so the key still
// counts as revoked.
func TestStateCommandsTreatARevokedKeyWithAStaleActiveStateAsRevoked(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		created := create(t, eng, "t1", nil)
		_, err := keysRevokeHandler(deps)(context.Background(), stateRevokeReq(created.Key.ID, "leaked"), principal())
		require.NoError(t, err)
		require.NoError(t, s.Keys().UpdateState(context.Background(), created.Key.ID, key.StateActive))
		stored, err := s.Keys().Get(context.Background(), created.Key.ID)
		require.NoError(t, err)
		require.Equal(t, key.StateActive, stored.State)
		require.NotNil(t, stored.RevokedAt)

		_, err = keysSuspendHandler(deps)(context.Background(), stateIDReq(created.Key.ID), principal())
		assert.Equal(t, "only an active key can be suspended", stateConflictMessage(t, err))
		_, err = keysRevokeHandler(deps)(context.Background(), stateRevokeReq(created.Key.ID, "again"), principal())
		assert.Equal(t, "this key is already revoked", stateConflictMessage(t, err))
		_, err = keysReactivateHandler(deps)(context.Background(), stateIDReq(created.Key.ID), principal())
		assert.Equal(t, "only a suspended key can be reactivated", stateConflictMessage(t, err))
		_, err = scopesAssign(deps, scopesReq(created.Key.ID, "read"))
		assert.Equal(t, "a revoked key's scopes cannot be changed", stateConflictMessage(t, err))
		_, err = scopesRemove(deps, scopesReq(created.Key.ID, "read"))
		assert.Equal(t, "a revoked key's scopes cannot be changed", stateConflictMessage(t, err))
	})
}
