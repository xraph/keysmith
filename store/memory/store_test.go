package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store/memory"
	"github.com/xraph/keysmith/usage"
)

func ctx() context.Context { return context.Background() }

// ── Key Store ───────────────────────────────────────────

func TestKeyStore_CreateAndGet(t *testing.T) {
	s := memory.New()
	k := &key.Key{
		ID:          id.NewKeyID(),
		TenantID:    "t1",
		Name:        "Test",
		KeyHash:     "hash123",
		Prefix:      "sk",
		Hint:        "f456",
		Environment: key.EnvTest,
		State:       key.StateActive,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	require.NoError(t, s.Keys().Create(ctx(), k))

	got, err := s.Keys().Get(ctx(), k.ID)
	require.NoError(t, err)
	assert.Equal(t, k.Name, got.Name)
	assert.Equal(t, k.KeyHash, got.KeyHash)
}

func TestKeyStore_GetByHash(t *testing.T) {
	s := memory.New()
	k := &key.Key{
		ID:      id.NewKeyID(),
		KeyHash: "unique_hash",
		State:   key.StateActive,
	}
	require.NoError(t, s.Keys().Create(ctx(), k))

	got, err := s.Keys().GetByHash(ctx(), "unique_hash")
	require.NoError(t, err)
	assert.Equal(t, k.ID.String(), got.ID.String())
}

func TestKeyStore_GetByHash_NotFound(t *testing.T) {
	s := memory.New()
	_, err := s.Keys().GetByHash(ctx(), "nonexistent")
	assert.Error(t, err)
}

func TestKeyStore_GetByPrefix(t *testing.T) {
	s := memory.New()
	k := &key.Key{
		ID:     id.NewKeyID(),
		Prefix: "sk",
		Hint:   "abcd",
	}
	require.NoError(t, s.Keys().Create(ctx(), k))

	got, err := s.Keys().GetByPrefix(ctx(), "sk", "abcd")
	require.NoError(t, err)
	assert.Equal(t, k.ID.String(), got.ID.String())
}

func TestKeyStore_Update(t *testing.T) {
	s := memory.New()
	k := &key.Key{
		ID:      id.NewKeyID(),
		Name:    "Original",
		KeyHash: "hash1",
	}
	require.NoError(t, s.Keys().Create(ctx(), k))

	k.Name = "Updated"
	k.KeyHash = "hash2"
	require.NoError(t, s.Keys().Update(ctx(), k))

	got, err := s.Keys().Get(ctx(), k.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", got.Name)

	// Hash index should be updated.
	got2, err := s.Keys().GetByHash(ctx(), "hash2")
	require.NoError(t, err)
	assert.Equal(t, k.ID.String(), got2.ID.String())

	// Old hash should not work.
	_, err = s.Keys().GetByHash(ctx(), "hash1")
	assert.Error(t, err)
}

func TestKeyStore_UpdateState(t *testing.T) {
	s := memory.New()
	k := &key.Key{
		ID:    id.NewKeyID(),
		State: key.StateActive,
	}
	require.NoError(t, s.Keys().Create(ctx(), k))

	require.NoError(t, s.Keys().UpdateState(ctx(), k.ID, key.StateSuspended))

	got, err := s.Keys().Get(ctx(), k.ID)
	require.NoError(t, err)
	assert.Equal(t, key.StateSuspended, got.State)
}

func TestKeyStore_Delete(t *testing.T) {
	s := memory.New()
	k := &key.Key{
		ID:      id.NewKeyID(),
		KeyHash: "del_hash",
	}
	require.NoError(t, s.Keys().Create(ctx(), k))

	require.NoError(t, s.Keys().Delete(ctx(), k.ID))

	_, err := s.Keys().Get(ctx(), k.ID)
	assert.Error(t, err)

	_, err = s.Keys().GetByHash(ctx(), "del_hash")
	assert.Error(t, err)
}

func TestKeyStore_List(t *testing.T) {
	s := memory.New()
	for i := 0; i < 5; i++ {
		k := &key.Key{
			ID:        id.NewKeyID(),
			TenantID:  "t1",
			State:     key.StateActive,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, s.Keys().Create(ctx(), k))
	}

	keys, err := s.Keys().List(ctx(), &key.ListFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Len(t, keys, 5)
}

func TestKeyStore_ListWithPagination(t *testing.T) {
	s := memory.New()
	for i := 0; i < 5; i++ {
		k := &key.Key{
			ID:        id.NewKeyID(),
			TenantID:  "t1",
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, s.Keys().Create(ctx(), k))
	}

	keys, err := s.Keys().List(ctx(), &key.ListFilter{TenantID: "t1", Limit: 2})
	require.NoError(t, err)
	assert.Len(t, keys, 2)
}

func TestKeyStore_Count(t *testing.T) {
	s := memory.New()
	for i := 0; i < 3; i++ {
		require.NoError(t, s.Keys().Create(ctx(), &key.Key{
			ID:       id.NewKeyID(),
			TenantID: "t1",
		}))
	}

	count, err := s.Keys().Count(ctx(), &key.ListFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestKeyStore_ListExpired(t *testing.T) {
	s := memory.New()
	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	require.NoError(t, s.Keys().Create(ctx(), &key.Key{
		ID:        id.NewKeyID(),
		State:     key.StateActive,
		ExpiresAt: &past,
	}))
	require.NoError(t, s.Keys().Create(ctx(), &key.Key{
		ID:        id.NewKeyID(),
		State:     key.StateActive,
		ExpiresAt: &future,
	}))

	expired, err := s.Keys().ListExpired(ctx(), time.Now())
	require.NoError(t, err)
	assert.Len(t, expired, 1)
}

func TestKeyStore_ListByPolicy(t *testing.T) {
	s := memory.New()
	polID := id.NewPolicyID()

	require.NoError(t, s.Keys().Create(ctx(), &key.Key{
		ID:       id.NewKeyID(),
		PolicyID: &polID,
	}))
	require.NoError(t, s.Keys().Create(ctx(), &key.Key{
		ID: id.NewKeyID(),
	}))

	keys, err := s.Keys().ListByPolicy(ctx(), polID)
	require.NoError(t, err)
	assert.Len(t, keys, 1)
}

func TestKeyStore_DeleteByTenant(t *testing.T) {
	s := memory.New()
	for i := 0; i < 3; i++ {
		require.NoError(t, s.Keys().Create(ctx(), &key.Key{
			ID:       id.NewKeyID(),
			TenantID: "doomed",
			KeyHash:  "h" + string(rune('a'+i)),
		}))
	}
	require.NoError(t, s.Keys().Create(ctx(), &key.Key{
		ID:       id.NewKeyID(),
		TenantID: "safe",
		KeyHash:  "safe_hash",
	}))

	require.NoError(t, s.Keys().DeleteByTenant(ctx(), "doomed"))

	count, err := s.Keys().Count(ctx(), &key.ListFilter{TenantID: "doomed"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	count, err = s.Keys().Count(ctx(), &key.ListFilter{TenantID: "safe"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

// ── Policy Store ────────────────────────────────────────

func TestPolicyStore_CRUD(t *testing.T) {
	s := memory.New()
	pol := &policy.Policy{
		ID:       id.NewPolicyID(),
		TenantID: "t1",
		Name:     "Standard",
	}

	require.NoError(t, s.Policies().Create(ctx(), pol))

	got, err := s.Policies().Get(ctx(), pol.ID)
	require.NoError(t, err)
	assert.Equal(t, "Standard", got.Name)

	pol.Name = "Updated"
	require.NoError(t, s.Policies().Update(ctx(), pol))

	got, err = s.Policies().Get(ctx(), pol.ID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", got.Name)

	require.NoError(t, s.Policies().Delete(ctx(), pol.ID))

	_, err = s.Policies().Get(ctx(), pol.ID)
	assert.Error(t, err)
}

func TestPolicyStore_GetByName(t *testing.T) {
	s := memory.New()
	pol := &policy.Policy{
		ID:       id.NewPolicyID(),
		TenantID: "t1",
		Name:     "FindMe",
	}
	require.NoError(t, s.Policies().Create(ctx(), pol))

	got, err := s.Policies().GetByName(ctx(), "t1", "FindMe")
	require.NoError(t, err)
	assert.Equal(t, pol.ID.String(), got.ID.String())
}

func TestPolicyStore_ListAndCount(t *testing.T) {
	s := memory.New()
	for i := 0; i < 3; i++ {
		require.NoError(t, s.Policies().Create(ctx(), &policy.Policy{
			ID:       id.NewPolicyID(),
			TenantID: "t1",
		}))
	}

	pols, err := s.Policies().List(ctx(), &policy.ListFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Len(t, pols, 3)

	count, err := s.Policies().Count(ctx(), &policy.ListFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

// ── Usage Store ─────────────────────────────────────────

func TestUsageStore_RecordAndQuery(t *testing.T) {
	s := memory.New()
	kid := id.NewKeyID()
	rec := &usage.Record{
		ID:        id.NewUsageID(),
		KeyID:     kid,
		TenantID:  "t1",
		Endpoint:  "/api/v1",
		Method:    "GET",
		CreatedAt: time.Now(),
	}

	require.NoError(t, s.Usages().Record(ctx(), rec))

	records, err := s.Usages().Query(ctx(), &usage.QueryFilter{KeyID: &kid})
	require.NoError(t, err)
	assert.Len(t, records, 1)
}

func TestUsageStore_RecordBatch(t *testing.T) {
	s := memory.New()
	kid := id.NewKeyID()
	recs := []*usage.Record{
		{ID: id.NewUsageID(), KeyID: kid, CreatedAt: time.Now()},
		{ID: id.NewUsageID(), KeyID: kid, CreatedAt: time.Now()},
	}

	require.NoError(t, s.Usages().RecordBatch(ctx(), recs))

	count, err := s.Usages().Count(ctx(), &usage.QueryFilter{KeyID: &kid})
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestUsageStore_Purge(t *testing.T) {
	s := memory.New()
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()

	require.NoError(t, s.Usages().Record(ctx(), &usage.Record{
		ID: id.NewUsageID(), CreatedAt: old,
	}))
	require.NoError(t, s.Usages().Record(ctx(), &usage.Record{
		ID: id.NewUsageID(), CreatedAt: recent,
	}))

	purged, err := s.Usages().Purge(ctx(), time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(1), purged)

	count, err := s.Usages().Count(ctx(), nil)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestUsageStore_DailyCount(t *testing.T) {
	s := memory.New()
	kid := id.NewKeyID()
	now := time.Now()

	require.NoError(t, s.Usages().Record(ctx(), &usage.Record{
		ID: id.NewUsageID(), KeyID: kid, CreatedAt: now,
	}))
	require.NoError(t, s.Usages().Record(ctx(), &usage.Record{
		ID: id.NewUsageID(), KeyID: kid, CreatedAt: now.Add(-48 * time.Hour),
	}))

	count, err := s.Usages().DailyCount(ctx(), kid, now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestUsageStore_MonthlyCount(t *testing.T) {
	s := memory.New()
	kid := id.NewKeyID()
	now := time.Now()

	for i := 0; i < 5; i++ {
		require.NoError(t, s.Usages().Record(ctx(), &usage.Record{
			ID: id.NewUsageID(), KeyID: kid, CreatedAt: now,
		}))
	}

	count, err := s.Usages().MonthlyCount(ctx(), kid, now)
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
}

// ── Rotation Store ──────────────────────────────────────

func TestRotationStore_CreateAndGet(t *testing.T) {
	s := memory.New()
	rec := &rotation.Record{
		ID:        id.NewRotationID(),
		KeyID:     id.NewKeyID(),
		TenantID:  "t1",
		Reason:    rotation.ReasonManual,
		GraceTTL:  time.Hour,
		GraceEnds: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}

	require.NoError(t, s.Rotations().Create(ctx(), rec))

	got, err := s.Rotations().Get(ctx(), rec.ID)
	require.NoError(t, err)
	assert.Equal(t, rec.KeyID.String(), got.KeyID.String())
	assert.Equal(t, rotation.ReasonManual, got.Reason)
}

func TestRotationStore_List(t *testing.T) {
	s := memory.New()
	kid := id.NewKeyID()
	for i := 0; i < 3; i++ {
		require.NoError(t, s.Rotations().Create(ctx(), &rotation.Record{
			ID:        id.NewRotationID(),
			KeyID:     kid,
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}))
	}

	recs, err := s.Rotations().List(ctx(), &rotation.ListFilter{KeyID: &kid})
	require.NoError(t, err)
	assert.Len(t, recs, 3)
}

func TestRotationStore_LatestForKey(t *testing.T) {
	s := memory.New()
	kid := id.NewKeyID()
	now := time.Now()

	require.NoError(t, s.Rotations().Create(ctx(), &rotation.Record{
		ID: id.NewRotationID(), KeyID: kid, CreatedAt: now.Add(-time.Hour),
	}))
	latest := &rotation.Record{
		ID: id.NewRotationID(), KeyID: kid, CreatedAt: now,
	}
	require.NoError(t, s.Rotations().Create(ctx(), latest))

	got, err := s.Rotations().LatestForKey(ctx(), kid)
	require.NoError(t, err)
	assert.Equal(t, latest.ID.String(), got.ID.String())
}

func TestRotationStore_ListPendingGrace(t *testing.T) {
	s := memory.New()
	now := time.Now()

	// Pending grace (ends in the future).
	require.NoError(t, s.Rotations().Create(ctx(), &rotation.Record{
		ID:        id.NewRotationID(),
		GraceEnds: now.Add(time.Hour),
		CreatedAt: now,
	}))
	// Past grace (already ended).
	require.NoError(t, s.Rotations().Create(ctx(), &rotation.Record{
		ID:        id.NewRotationID(),
		GraceEnds: now.Add(-time.Hour),
		CreatedAt: now,
	}))

	pending, err := s.Rotations().ListPendingGrace(ctx(), now)
	require.NoError(t, err)
	assert.Len(t, pending, 1)
}

// ── Scope Store ─────────────────────────────────────────

func TestScopeStore_CRUD(t *testing.T) {
	s := memory.New()
	sc := &scope.Scope{
		ID:       id.NewScopeID(),
		TenantID: "t1",
		Name:     "read:users",
	}

	require.NoError(t, s.Scopes().Create(ctx(), sc))

	got, err := s.Scopes().Get(ctx(), sc.ID)
	require.NoError(t, err)
	assert.Equal(t, "read:users", got.Name)

	sc.Name = "write:users"
	require.NoError(t, s.Scopes().Update(ctx(), sc))

	got, err = s.Scopes().Get(ctx(), sc.ID)
	require.NoError(t, err)
	assert.Equal(t, "write:users", got.Name)

	require.NoError(t, s.Scopes().Delete(ctx(), sc.ID))

	_, err = s.Scopes().Get(ctx(), sc.ID)
	assert.Error(t, err)
}

func TestScopeStore_GetByName(t *testing.T) {
	s := memory.New()
	sc := &scope.Scope{
		ID:       id.NewScopeID(),
		TenantID: "t1",
		Name:     "read:users",
	}
	require.NoError(t, s.Scopes().Create(ctx(), sc))

	got, err := s.Scopes().GetByName(ctx(), "t1", "read:users")
	require.NoError(t, err)
	assert.Equal(t, sc.ID.String(), got.ID.String())
}

func TestScopeStore_AssignAndRemove(t *testing.T) {
	s := memory.New()
	kid := id.NewKeyID()

	// Create scopes.
	for _, name := range []string{"read:users", "write:users", "admin"} {
		require.NoError(t, s.Scopes().Create(ctx(), &scope.Scope{
			ID:       id.NewScopeID(),
			TenantID: "t1",
			Name:     name,
		}))
	}

	// Assign.
	require.NoError(t, s.Scopes().AssignToKey(ctx(), kid, []string{"read:users", "write:users"}))

	listed, err := s.Scopes().ListByKey(ctx(), kid)
	require.NoError(t, err)
	assert.Len(t, listed, 2)

	// Remove.
	require.NoError(t, s.Scopes().RemoveFromKey(ctx(), kid, []string{"read:users"}))

	listed, err = s.Scopes().ListByKey(ctx(), kid)
	require.NoError(t, err)
	assert.Len(t, listed, 1)
	assert.Equal(t, "write:users", listed[0].Name)
}

func TestScopeStore_List(t *testing.T) {
	s := memory.New()
	for _, name := range []string{"read:users", "write:users"} {
		require.NoError(t, s.Scopes().Create(ctx(), &scope.Scope{
			ID:       id.NewScopeID(),
			TenantID: "t1",
			Name:     name,
		}))
	}

	scopes, err := s.Scopes().List(ctx(), &scope.ListFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Len(t, scopes, 2)
}

// ── Lifecycle ───────────────────────────────────────────

func TestStore_MigratePingClose(t *testing.T) {
	s := memory.New()
	require.NoError(t, s.Migrate(ctx()))
	require.NoError(t, s.Ping(ctx()))
	require.NoError(t, s.Close())
}
