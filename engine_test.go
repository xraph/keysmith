package keysmith_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store/memory"
	"github.com/xraph/keysmith/usage"
)

func newTestEngine(t *testing.T) *keysmith.Engine {
	t.Helper()
	ms := memory.New()
	eng, err := keysmith.NewEngine(keysmith.WithStore(ms))
	require.NoError(t, err)
	return eng
}

func testCtx() context.Context {
	return keysmith.WithTenant(context.Background(), "app_test", "tenant_test")
}

func TestNewEngine_RequiresStore(t *testing.T) {
	_, err := keysmith.NewEngine()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "store is required")
}

func TestCreateKey(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Test Key",
		Prefix:      "sk",
		Environment: key.EnvTest,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.RawKey)
	assert.Contains(t, result.RawKey, "sk_test_")
	assert.Equal(t, "Test Key", result.Key.Name)
	assert.Equal(t, key.StateActive, result.Key.State)
	assert.Equal(t, key.EnvTest, result.Key.Environment)
	assert.Equal(t, "sk", result.Key.Prefix)
	assert.Equal(t, result.RawKey[len(result.RawKey)-4:], result.Key.Hint)
	assert.NotEmpty(t, result.Key.KeyHash)
	assert.NotEqual(t, result.RawKey, result.Key.KeyHash)
}

func TestValidateKey(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Validation Test",
		Prefix:      "sk",
		Environment: key.EnvLive,
	})
	require.NoError(t, err)

	t.Run("valid key", func(t *testing.T) {
		vr, err := eng.ValidateKey(ctx, result.RawKey)
		require.NoError(t, err)
		assert.Equal(t, result.Key.ID.String(), vr.Key.ID.String())
	})

	t.Run("invalid key", func(t *testing.T) {
		_, err := eng.ValidateKey(ctx, "sk_live_invalid")
		assert.ErrorIs(t, err, keysmith.ErrInvalidKey)
	})
}

func TestRevokeKey(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Revoke Test",
		Prefix:      "sk",
		Environment: key.EnvTest,
	})
	require.NoError(t, err)

	err = eng.RevokeKey(ctx, result.Key.ID, "test revocation")
	require.NoError(t, err)

	_, err = eng.ValidateKey(ctx, result.RawKey)
	assert.ErrorIs(t, err, keysmith.ErrKeyInactive)
}

func TestSuspendAndReactivateKey(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Suspend Test",
		Prefix:      "sk",
		Environment: key.EnvTest,
	})
	require.NoError(t, err)

	// Suspend.
	err = eng.SuspendKey(ctx, result.Key.ID)
	require.NoError(t, err)

	_, err = eng.ValidateKey(ctx, result.RawKey)
	assert.ErrorIs(t, err, keysmith.ErrKeyInactive)

	// Reactivate.
	err = eng.ReactivateKey(ctx, result.Key.ID)
	require.NoError(t, err)

	vr, err := eng.ValidateKey(ctx, result.RawKey)
	require.NoError(t, err)
	assert.Equal(t, result.Key.ID.String(), vr.Key.ID.String())
}

func TestReactivateKey_InvalidState(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Active Key",
		Prefix:      "sk",
		Environment: key.EnvTest,
	})
	require.NoError(t, err)

	err = eng.ReactivateKey(ctx, result.Key.ID)
	assert.ErrorIs(t, err, keysmith.ErrInvalidStateTransition)
}

func TestRotateKey(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	original, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Rotate Test",
		Prefix:      "sk",
		Environment: key.EnvLive,
	})
	require.NoError(t, err)

	rotated, err := eng.RotateKey(ctx, original.Key.ID, rotation.ReasonManual)
	require.NoError(t, err)

	assert.NotEqual(t, original.RawKey, rotated.RawKey)
	assert.Equal(t, original.Key.ID.String(), rotated.Key.ID.String())
	assert.NotNil(t, rotated.Key.RotatedAt)

	// New key should validate.
	vr, err := eng.ValidateKey(ctx, rotated.RawKey)
	require.NoError(t, err)
	assert.Equal(t, original.Key.ID.String(), vr.Key.ID.String())

	// Old key should fail.
	_, err = eng.ValidateKey(ctx, original.RawKey)
	assert.ErrorIs(t, err, keysmith.ErrInvalidKey)
}

func TestExpiredKey(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	past := time.Now().Add(-1 * time.Hour)
	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Expired Key",
		Prefix:      "sk",
		Environment: key.EnvTest,
		ExpiresAt:   &past,
	})
	require.NoError(t, err)

	_, err = eng.ValidateKey(ctx, result.RawKey)
	assert.ErrorIs(t, err, keysmith.ErrKeyExpired)
}

func TestListKeys(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	for i := 0; i < 3; i++ {
		_, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
			Name:        "Key",
			Prefix:      "sk",
			Environment: key.EnvTest,
		})
		require.NoError(t, err)
	}

	keys, err := eng.ListKeys(ctx, &key.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, keys, 3)
}

func TestPolicyCRUD(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	pol := &policy.Policy{
		Name:            "Standard",
		RateLimit:       100,
		RateLimitWindow: time.Minute,
		GracePeriod:     24 * time.Hour,
	}

	err := eng.CreatePolicy(ctx, pol)
	require.NoError(t, err)
	assert.NotEmpty(t, pol.ID.String())

	fetched, err := eng.GetPolicy(ctx, pol.ID)
	require.NoError(t, err)
	assert.Equal(t, "Standard", fetched.Name)
	assert.Equal(t, 100, fetched.RateLimit)

	pol.Name = "Updated"
	err = eng.UpdatePolicy(ctx, pol)
	require.NoError(t, err)

	fetched, err = eng.GetPolicy(ctx, pol.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", fetched.Name)

	err = eng.DeletePolicy(ctx, pol.ID)
	require.NoError(t, err)
}

func TestDeletePolicy_InUse(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	pol := &policy.Policy{Name: "InUse", GracePeriod: time.Hour}
	err := eng.CreatePolicy(ctx, pol)
	require.NoError(t, err)

	_, err = eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Key with policy",
		Prefix:      "sk",
		Environment: key.EnvTest,
		PolicyID:    &pol.ID,
	})
	require.NoError(t, err)

	err = eng.DeletePolicy(ctx, pol.ID)
	assert.ErrorIs(t, err, keysmith.ErrPolicyInUse)
}

func TestScopeCRUD(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	sc := &scope.Scope{Name: "read:users", Description: "Read users"}
	err := eng.CreateScope(ctx, sc)
	require.NoError(t, err)
	assert.NotEmpty(t, sc.ID.String())

	scopes, err := eng.ListScopes(ctx, &scope.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, scopes, 1)

	err = eng.DeleteScope(ctx, sc.ID)
	require.NoError(t, err)
}

func TestCreateKeyWithScopes(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	// Create scopes first.
	for _, name := range []string{"read:users", "write:users"} {
		err := eng.CreateScope(ctx, &scope.Scope{Name: name})
		require.NoError(t, err)
	}

	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Scoped Key",
		Prefix:      "sk",
		Environment: key.EnvLive,
		Scopes:      []string{"read:users", "write:users"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"read:users", "write:users"}, result.Key.Scopes)

	// Validate should return scopes.
	vr, err := eng.ValidateKey(ctx, result.RawKey)
	require.NoError(t, err)
	assert.Len(t, vr.Scopes, 2)
}

func TestRecordUsage(t *testing.T) {
	eng := newTestEngine(t)
	ctx := testCtx()

	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
		Name:        "Usage Test",
		Prefix:      "sk",
		Environment: key.EnvTest,
	})
	require.NoError(t, err)

	rec := &usage.Record{
		KeyID:      result.Key.ID,
		TenantID:   "tenant_test",
		Endpoint:   "/api/v1/users",
		Method:     "GET",
		StatusCode: 200,
		IPAddress:  "127.0.0.1",
		Latency:    50 * time.Millisecond,
	}
	err = eng.RecordUsage(ctx, rec)
	require.NoError(t, err)
	assert.NotEmpty(t, rec.ID.String())

	records, err := eng.QueryUsage(ctx, &usage.QueryFilter{
		KeyID: &result.Key.ID,
	})
	require.NoError(t, err)
	assert.Len(t, records, 1)
}
