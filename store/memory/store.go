// Package memory provides an in-memory implementation of store.Store for testing.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/usage"
)

var _ store.Store = (*Store)(nil)

// Store is an in-memory store implementation for testing.
type Store struct {
	mu sync.RWMutex

	keys      map[string]*key.Key         // keyID string -> Key
	hashIndex map[string]string           // keyHash -> keyID string
	policies  map[string]*policy.Policy   // policyID string -> Policy
	usages    []*usage.Record             // append-only
	rotations map[string]*rotation.Record // rotationID string -> Record
	scopes    map[string]*scope.Scope     // scopeID string -> Scope
	keyScopes map[string]map[string]bool  // keyID -> set of scope names
}

// New creates a new in-memory store.
func New() *Store {
	return &Store{
		keys:      make(map[string]*key.Key),
		hashIndex: make(map[string]string),
		policies:  make(map[string]*policy.Policy),
		rotations: make(map[string]*rotation.Record),
		scopes:    make(map[string]*scope.Scope),
		keyScopes: make(map[string]map[string]bool),
	}
}

// ── Lifecycle ─────────────────────────────────────

func (s *Store) Keys() key.Store           { return (*keyStore)(s) }
func (s *Store) Policies() policy.Store    { return (*policyStore)(s) }
func (s *Store) Usages() usage.Store       { return (*usageStore)(s) }
func (s *Store) Rotations() rotation.Store { return (*rotationStore)(s) }
func (s *Store) Scopes() scope.Store       { return (*scopeStore)(s) }

func (s *Store) Migrate(_ context.Context) error { return nil }
func (s *Store) Ping(_ context.Context) error    { return nil }
func (s *Store) Close() error                    { return nil }

// ══════════════════════════════════════════════════
// Key Store
// ══════════════════════════════════════════════════

type keyStore Store

func (s *keyStore) store() *Store { return (*Store)(s) }

func (s *keyStore) Create(_ context.Context, k *key.Key) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	cp := *k
	st.keys[k.ID.String()] = &cp
	st.hashIndex[k.KeyHash] = k.ID.String()
	return nil
}

func (s *keyStore) Get(_ context.Context, keyID id.KeyID) (*key.Key, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	k, ok := st.keys[keyID.String()]
	if !ok {
		return nil, errNotFound("key")
	}
	cp := *k
	return &cp, nil
}

func (s *keyStore) GetByHash(_ context.Context, hash string) (*key.Key, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	kid, ok := st.hashIndex[hash]
	if !ok {
		return nil, errNotFound("key")
	}
	k, ok := st.keys[kid]
	if !ok {
		return nil, errNotFound("key")
	}
	cp := *k
	return &cp, nil
}

func (s *keyStore) GetByPrefix(_ context.Context, prefix, hint string) (*key.Key, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	for _, k := range st.keys {
		if k.Prefix == prefix && k.Hint == hint {
			cp := *k
			return &cp, nil
		}
	}
	return nil, errNotFound("key")
}

func (s *keyStore) Update(_ context.Context, k *key.Key) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	old, ok := st.keys[k.ID.String()]
	if !ok {
		return errNotFound("key")
	}
	// Update hash index if hash changed.
	if old.KeyHash != k.KeyHash {
		delete(st.hashIndex, old.KeyHash)
		st.hashIndex[k.KeyHash] = k.ID.String()
	}
	cp := *k
	st.keys[k.ID.String()] = &cp
	return nil
}

func (s *keyStore) UpdateState(_ context.Context, keyID id.KeyID, state key.State) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	k, ok := st.keys[keyID.String()]
	if !ok {
		return errNotFound("key")
	}
	k.State = state
	k.UpdatedAt = time.Now()
	return nil
}

func (s *keyStore) UpdateLastUsed(_ context.Context, keyID id.KeyID, at time.Time) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	k, ok := st.keys[keyID.String()]
	if !ok {
		return errNotFound("key")
	}
	k.LastUsedAt = &at
	return nil
}

func (s *keyStore) Delete(_ context.Context, keyID id.KeyID) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	k, ok := st.keys[keyID.String()]
	if !ok {
		return errNotFound("key")
	}
	delete(st.hashIndex, k.KeyHash)
	delete(st.keys, keyID.String())
	delete(st.keyScopes, keyID.String())
	return nil
}

func (s *keyStore) List(_ context.Context, filter *key.ListFilter) ([]*key.Key, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make([]*key.Key, 0, len(st.keys))
	for _, k := range st.keys {
		if !matchKeyFilter(k, filter) {
			continue
		}
		cp := *k
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return applyPagination(result, filter.Offset, filter.Limit), nil
}

func (s *keyStore) Count(_ context.Context, filter *key.ListFilter) (int64, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	var count int64
	for _, k := range st.keys {
		if matchKeyFilter(k, filter) {
			count++
		}
	}
	return count, nil
}

func (s *keyStore) ListExpired(_ context.Context, before time.Time) ([]*key.Key, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	var result []*key.Key
	for _, k := range st.keys {
		if k.State == key.StateActive && k.ExpiresAt != nil && k.ExpiresAt.Before(before) {
			cp := *k
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (s *keyStore) ListByPolicy(_ context.Context, policyID id.PolicyID) ([]*key.Key, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	var result []*key.Key
	pid := policyID.String()
	for _, k := range st.keys {
		if k.PolicyID != nil && k.PolicyID.String() == pid {
			cp := *k
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (s *keyStore) DeleteByTenant(_ context.Context, tenantID string) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	for kid, k := range st.keys {
		if k.TenantID == tenantID {
			delete(st.hashIndex, k.KeyHash)
			delete(st.keys, kid)
			delete(st.keyScopes, kid)
		}
	}
	return nil
}

func matchKeyFilter(k *key.Key, f *key.ListFilter) bool {
	if f == nil {
		return true
	}
	if f.TenantID != "" && k.TenantID != f.TenantID {
		return false
	}
	if f.Environment != "" && k.Environment != f.Environment {
		return false
	}
	if f.State != "" && k.State != f.State {
		return false
	}
	if f.PolicyID != nil && (k.PolicyID == nil || k.PolicyID.String() != f.PolicyID.String()) {
		return false
	}
	if f.CreatedBy != "" && k.CreatedBy != f.CreatedBy {
		return false
	}
	return true
}

// ══════════════════════════════════════════════════
// Policy Store
// ══════════════════════════════════════════════════

type policyStore Store

func (s *policyStore) store() *Store { return (*Store)(s) }

func (s *policyStore) Create(_ context.Context, pol *policy.Policy) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	cp := *pol
	st.policies[pol.ID.String()] = &cp
	return nil
}

func (s *policyStore) Get(_ context.Context, polID id.PolicyID) (*policy.Policy, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	p, ok := st.policies[polID.String()]
	if !ok {
		return nil, errNotFound("policy")
	}
	cp := *p
	return &cp, nil
}

func (s *policyStore) GetByName(_ context.Context, tenantID, name string) (*policy.Policy, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	for _, p := range st.policies {
		if p.TenantID == tenantID && p.Name == name {
			cp := *p
			return &cp, nil
		}
	}
	return nil, errNotFound("policy")
}

func (s *policyStore) Update(_ context.Context, pol *policy.Policy) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	if _, ok := st.policies[pol.ID.String()]; !ok {
		return errNotFound("policy")
	}
	cp := *pol
	st.policies[pol.ID.String()] = &cp
	return nil
}

func (s *policyStore) Delete(_ context.Context, polID id.PolicyID) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	if _, ok := st.policies[polID.String()]; !ok {
		return errNotFound("policy")
	}
	delete(st.policies, polID.String())
	return nil
}

func (s *policyStore) List(_ context.Context, filter *policy.ListFilter) ([]*policy.Policy, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make([]*policy.Policy, 0, len(st.policies))
	for _, p := range st.policies {
		if filter != nil && filter.TenantID != "" && p.TenantID != filter.TenantID {
			continue
		}
		cp := *p
		result = append(result, &cp)
	}
	offset, limit := 0, 0
	if filter != nil {
		offset, limit = filter.Offset, filter.Limit
	}
	return applyPagination(result, offset, limit), nil
}

func (s *policyStore) Count(_ context.Context, filter *policy.ListFilter) (int64, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	var count int64
	for _, p := range st.policies {
		if filter != nil && filter.TenantID != "" && p.TenantID != filter.TenantID {
			continue
		}
		count++
	}
	return count, nil
}

// ══════════════════════════════════════════════════
// Usage Store
// ══════════════════════════════════════════════════

type usageStore Store

func (s *usageStore) store() *Store { return (*Store)(s) }

func (s *usageStore) Record(_ context.Context, rec *usage.Record) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	cp := *rec
	st.usages = append(st.usages, &cp)
	return nil
}

func (s *usageStore) RecordBatch(_ context.Context, recs []*usage.Record) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	for _, rec := range recs {
		cp := *rec
		st.usages = append(st.usages, &cp)
	}
	return nil
}

func (s *usageStore) Query(_ context.Context, filter *usage.QueryFilter) ([]*usage.Record, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make([]*usage.Record, 0, len(st.usages))
	for _, rec := range st.usages {
		if !matchUsageFilter(rec, filter) {
			continue
		}
		cp := *rec
		result = append(result, &cp)
	}
	offset, limit := 0, 0
	if filter != nil {
		offset, limit = filter.Offset, filter.Limit
	}
	return applyPagination(result, offset, limit), nil
}

func (s *usageStore) Aggregate(_ context.Context, _ *usage.QueryFilter) ([]*usage.Aggregation, error) {
	return nil, nil
}

func (s *usageStore) Count(_ context.Context, filter *usage.QueryFilter) (int64, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	var count int64
	for _, rec := range st.usages {
		if matchUsageFilter(rec, filter) {
			count++
		}
	}
	return count, nil
}

func (s *usageStore) Purge(_ context.Context, before time.Time) (int64, error) {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	var kept []*usage.Record
	var purged int64
	for _, rec := range st.usages {
		if rec.CreatedAt.Before(before) {
			purged++
		} else {
			kept = append(kept, rec)
		}
	}
	st.usages = kept
	return purged, nil
}

func (s *usageStore) DailyCount(_ context.Context, keyID id.KeyID, date time.Time) (int64, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	kid := keyID.String()
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	var count int64
	for _, rec := range st.usages {
		if rec.KeyID.String() == kid && !rec.CreatedAt.Before(dayStart) && rec.CreatedAt.Before(dayEnd) {
			count++
		}
	}
	return count, nil
}

func (s *usageStore) MonthlyCount(_ context.Context, keyID id.KeyID, month time.Time) (int64, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	kid := keyID.String()
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, month.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)

	var count int64
	for _, rec := range st.usages {
		if rec.KeyID.String() == kid && !rec.CreatedAt.Before(monthStart) && rec.CreatedAt.Before(monthEnd) {
			count++
		}
	}
	return count, nil
}

func matchUsageFilter(rec *usage.Record, f *usage.QueryFilter) bool {
	if f == nil {
		return true
	}
	if f.KeyID != nil && rec.KeyID.String() != f.KeyID.String() {
		return false
	}
	if f.TenantID != "" && rec.TenantID != f.TenantID {
		return false
	}
	if f.After != nil && rec.CreatedAt.Before(*f.After) {
		return false
	}
	if f.Before != nil && rec.CreatedAt.After(*f.Before) {
		return false
	}
	return true
}

// ══════════════════════════════════════════════════
// Rotation Store
// ══════════════════════════════════════════════════

type rotationStore Store

func (s *rotationStore) store() *Store { return (*Store)(s) }

func (s *rotationStore) Create(_ context.Context, rec *rotation.Record) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	cp := *rec
	st.rotations[rec.ID.String()] = &cp
	return nil
}

func (s *rotationStore) Get(_ context.Context, rotID id.RotationID) (*rotation.Record, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	r, ok := st.rotations[rotID.String()]
	if !ok {
		return nil, errNotFound("rotation")
	}
	cp := *r
	return &cp, nil
}

func (s *rotationStore) List(_ context.Context, filter *rotation.ListFilter) ([]*rotation.Record, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make([]*rotation.Record, 0, len(st.rotations))
	for _, r := range st.rotations {
		if !matchRotationFilter(r, filter) {
			continue
		}
		cp := *r
		result = append(result, &cp)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	offset, limit := 0, 0
	if filter != nil {
		offset, limit = filter.Offset, filter.Limit
	}
	return applyPagination(result, offset, limit), nil
}

func (s *rotationStore) ListPendingGrace(_ context.Context, now time.Time) ([]*rotation.Record, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	var result []*rotation.Record
	for _, r := range st.rotations {
		if r.GraceEnds.After(now) {
			cp := *r
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (s *rotationStore) LatestForKey(_ context.Context, keyID id.KeyID) (*rotation.Record, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	kid := keyID.String()
	var latest *rotation.Record
	for _, r := range st.rotations {
		if r.KeyID.String() == kid {
			if latest == nil || r.CreatedAt.After(latest.CreatedAt) {
				cp := *r
				latest = &cp
			}
		}
	}
	if latest == nil {
		return nil, errNotFound("rotation")
	}
	return latest, nil
}

func matchRotationFilter(r *rotation.Record, f *rotation.ListFilter) bool {
	if f == nil {
		return true
	}
	if f.KeyID != nil && r.KeyID.String() != f.KeyID.String() {
		return false
	}
	if f.TenantID != "" && r.TenantID != f.TenantID {
		return false
	}
	if f.Reason != "" && r.Reason != f.Reason {
		return false
	}
	return true
}

// ══════════════════════════════════════════════════
// Scope Store
// ══════════════════════════════════════════════════

type scopeStore Store

func (s *scopeStore) store() *Store { return (*Store)(s) }

func (s *scopeStore) Create(_ context.Context, sc *scope.Scope) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	cp := *sc
	st.scopes[sc.ID.String()] = &cp
	return nil
}

func (s *scopeStore) Get(_ context.Context, scopeID id.ScopeID) (*scope.Scope, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	sc, ok := st.scopes[scopeID.String()]
	if !ok {
		return nil, errNotFound("scope")
	}
	cp := *sc
	return &cp, nil
}

func (s *scopeStore) GetByName(_ context.Context, tenantID, name string) (*scope.Scope, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	for _, sc := range st.scopes {
		if sc.TenantID == tenantID && sc.Name == name {
			cp := *sc
			return &cp, nil
		}
	}
	return nil, errNotFound("scope")
}

func (s *scopeStore) Update(_ context.Context, sc *scope.Scope) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	if _, ok := st.scopes[sc.ID.String()]; !ok {
		return errNotFound("scope")
	}
	cp := *sc
	st.scopes[sc.ID.String()] = &cp
	return nil
}

func (s *scopeStore) Delete(_ context.Context, scopeID id.ScopeID) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	if _, ok := st.scopes[scopeID.String()]; !ok {
		return errNotFound("scope")
	}
	delete(st.scopes, scopeID.String())
	return nil
}

func (s *scopeStore) List(_ context.Context, filter *scope.ListFilter) ([]*scope.Scope, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make([]*scope.Scope, 0, len(st.scopes))
	for _, sc := range st.scopes {
		if filter != nil {
			if filter.TenantID != "" && sc.TenantID != filter.TenantID {
				continue
			}
			if filter.Parent != "" && sc.Parent != filter.Parent {
				continue
			}
		}
		cp := *sc
		result = append(result, &cp)
	}
	offset, limit := 0, 0
	if filter != nil {
		offset, limit = filter.Offset, filter.Limit
	}
	return applyPagination(result, offset, limit), nil
}

func (s *scopeStore) ListByKey(_ context.Context, keyID id.KeyID) ([]*scope.Scope, error) {
	st := s.store()
	st.mu.RLock()
	defer st.mu.RUnlock()

	names := st.keyScopes[keyID.String()]
	result := make([]*scope.Scope, 0, len(st.scopes))
	for _, sc := range st.scopes {
		if names[sc.Name] {
			cp := *sc
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (s *scopeStore) AssignToKey(_ context.Context, keyID id.KeyID, scopeNames []string) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	kid := keyID.String()
	if st.keyScopes[kid] == nil {
		st.keyScopes[kid] = make(map[string]bool)
	}
	for _, name := range scopeNames {
		st.keyScopes[kid][name] = true
	}
	return nil
}

func (s *scopeStore) RemoveFromKey(_ context.Context, keyID id.KeyID, scopeNames []string) error {
	st := s.store()
	st.mu.Lock()
	defer st.mu.Unlock()

	kid := keyID.String()
	for _, name := range scopeNames {
		delete(st.keyScopes[kid], name)
	}
	return nil
}

// ══════════════════════════════════════════════════
// Helpers
// ══════════════════════════════════════════════════

type notFoundError struct{ entity string }

func (e *notFoundError) Error() string { return e.entity + " not found" }

func errNotFound(entity string) error { return &notFoundError{entity: entity} }

func applyPagination[T any](items []*T, offset, limit int) []*T {
	if offset > len(items) {
		return nil
	}
	items = items[offset:]
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}
