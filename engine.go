package keysmith

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/plugin"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/usage"
)

// Engine is the central Keysmith engine that coordinates all subsystems.
type Engine struct {
	store       store.Store
	hasher      Hasher
	generator   KeyGenerator
	ratelimiter RateLimiter
	hooks       *plugin.Manager
	logger      *slog.Logger
}

// NewEngine creates a new Keysmith engine with the given options.
func NewEngine(opts ...Option) (*Engine, error) {
	e := &Engine{
		hasher:    DefaultHasher(),
		generator: DefaultKeyGenerator(),
		hooks:     plugin.NewManager(),
		logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.store == nil {
		return nil, errors.New("keysmith: store is required")
	}
	return e, nil
}

// Store returns the underlying composite store.
func (e *Engine) Store() store.Store { return e.store }

// Start starts the engine and any background workers.
func (e *Engine) Start(_ context.Context) error { return nil }

// Stop gracefully shuts down the engine.
func (e *Engine) Stop(ctx context.Context) error {
	return e.hooks.FireShutdown(ctx)
}

// ──────────────────────────────────────────────────
// Key Management
// ──────────────────────────────────────────────────

// CreateKey generates a new API key, hashes it, stores the hash, and returns
// the raw key exactly once. The raw key is never persisted.
func (e *Engine) CreateKey(ctx context.Context, input *CreateKeyInput) (*key.CreateResult, error) {
	sc := scopeFromContext(ctx)
	tenantID := sc.tenantID
	appID := sc.appID
	if tenantID == "" {
		tenantID = input.TenantID
	}

	rawKey, err := e.generator.Generate(input.Prefix, input.Environment)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	hash, err := e.hasher.Hash(rawKey)
	if err != nil {
		return nil, fmt.Errorf("hash key: %w", err)
	}

	now := time.Now()
	k := &key.Key{
		ID:          id.NewKeyID(),
		TenantID:    tenantID,
		AppID:       appID,
		Name:        input.Name,
		Description: input.Description,
		Prefix:      input.Prefix,
		Hint:        rawKey[len(rawKey)-4:],
		KeyHash:     hash,
		Environment: input.Environment,
		State:       key.StateActive,
		PolicyID:    input.PolicyID,
		Metadata:    input.Metadata,
		CreatedBy:   input.CreatedBy,
		ExpiresAt:   input.ExpiresAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Apply policy constraints if assigned.
	if input.PolicyID != nil {
		pol, polErr := e.store.Policies().Get(ctx, *input.PolicyID)
		if polErr != nil {
			return nil, fmt.Errorf("get policy: %w", polErr)
		}
		if pol.MaxKeyLifetime > 0 && input.ExpiresAt == nil {
			expiry := now.Add(pol.MaxKeyLifetime)
			k.ExpiresAt = &expiry
		}
	}

	if err := e.store.Keys().Create(ctx, k); err != nil {
		_ = e.hooks.FireKeyCreateFailed(ctx, k, err)
		return nil, fmt.Errorf("store key: %w", err)
	}

	// Assign scopes.
	if len(input.Scopes) > 0 {
		if err := e.store.Scopes().AssignToKey(ctx, k.ID, input.Scopes); err != nil {
			return nil, fmt.Errorf("assign scopes: %w", err)
		}
		k.Scopes = input.Scopes
	}

	_ = e.hooks.FireKeyCreated(ctx, k)

	return &key.CreateResult{Key: k, RawKey: rawKey}, nil
}

// ValidateKey validates a raw API key and returns the key record if valid.
// This is the hot path — optimized for speed.
func (e *Engine) ValidateKey(ctx context.Context, rawKey string) (*ValidationResult, error) {
	hash, err := e.hasher.Hash(rawKey)
	if err != nil {
		return nil, fmt.Errorf("hash key: %w", err)
	}

	k, err := e.store.Keys().GetByHash(ctx, hash)
	if err != nil {
		_ = e.hooks.FireKeyValidationFailed(ctx, rawKey, err)
		return nil, ErrInvalidKey
	}

	// Check state.
	if k.State != key.StateActive && k.State != key.StateRotated {
		_ = e.hooks.FireKeyValidationFailed(ctx, rawKey, ErrKeyInactive)
		return nil, ErrKeyInactive
	}

	// Check expiration.
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		_ = e.store.Keys().UpdateState(ctx, k.ID, key.StateExpired)
		_ = e.hooks.FireKeyExpired(ctx, k)
		return nil, ErrKeyExpired
	}

	// Check grace period for rotated keys.
	if k.State == key.StateRotated {
		latest, rotErr := e.store.Rotations().LatestForKey(ctx, k.ID)
		if rotErr == nil && time.Now().After(latest.GraceEnds) {
			_ = e.store.Keys().UpdateState(ctx, k.ID, key.StateRevoked)
			return nil, ErrKeyRevoked
		}
	}

	// Load policy for rate-limiting.
	var pol *policy.Policy
	if k.PolicyID != nil {
		pol, _ = e.store.Policies().Get(ctx, *k.PolicyID)
	}

	// Rate-limit check.
	if pol != nil && e.ratelimiter != nil && pol.RateLimit > 0 {
		allowed, rlErr := e.ratelimiter.Allow(ctx, k.ID.String(), pol.RateLimit, pol.RateLimitWindow)
		if rlErr != nil || !allowed {
			_ = e.hooks.FireKeyRateLimited(ctx, k)
			return nil, ErrRateLimited
		}
	}

	// Load scopes.
	scopes, _ := e.store.Scopes().ListByKey(ctx, k.ID)
	scopeNames := make([]string, len(scopes))
	for i, s := range scopes {
		scopeNames[i] = s.Name
	}

	// Update last-used timestamp asynchronously.
	go func() {
		now := time.Now()
		_ = e.store.Keys().UpdateLastUsed(context.Background(), k.ID, now)
	}()

	_ = e.hooks.FireKeyValidated(ctx, k)

	return &ValidationResult{
		Key:    k,
		Scopes: scopeNames,
		Policy: pol,
	}, nil
}

// RotateKey creates a new key for the same key record, depreciates the old one
// with a grace period, and returns the new raw key.
func (e *Engine) RotateKey(ctx context.Context, keyID id.KeyID, reason rotation.Reason) (*key.CreateResult, error) {
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("get key: %w", err)
	}

	// Determine grace period from policy or default.
	graceTTL := 24 * time.Hour
	if k.PolicyID != nil {
		pol, polErr := e.store.Policies().Get(ctx, *k.PolicyID)
		if polErr == nil && pol.GracePeriod > 0 {
			graceTTL = pol.GracePeriod
		}
	}

	// Generate new key.
	rawKey, err := e.generator.Generate(k.Prefix, k.Environment)
	if err != nil {
		return nil, fmt.Errorf("generate new key: %w", err)
	}

	newHash, err := e.hasher.Hash(rawKey)
	if err != nil {
		return nil, fmt.Errorf("hash new key: %w", err)
	}

	oldHash := k.KeyHash
	now := time.Now()

	// Update the key record with the new hash.
	k.KeyHash = newHash
	k.Hint = rawKey[len(rawKey)-4:]
	k.RotatedAt = &now
	k.UpdatedAt = now

	if err := e.store.Keys().Update(ctx, k); err != nil {
		return nil, fmt.Errorf("update key: %w", err)
	}

	// Record the rotation.
	rec := &rotation.Record{
		ID:         id.NewRotationID(),
		KeyID:      k.ID,
		TenantID:   k.TenantID,
		OldKeyHash: oldHash,
		NewKeyHash: newHash,
		Reason:     reason,
		GraceTTL:   graceTTL,
		GraceEnds:  now.Add(graceTTL),
		CreatedAt:  now,
	}
	if err := e.store.Rotations().Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("record rotation: %w", err)
	}

	_ = e.hooks.FireKeyRotated(ctx, k, rec)

	return &key.CreateResult{Key: k, RawKey: rawKey}, nil
}

// RevokeKey permanently disables a key.
func (e *Engine) RevokeKey(ctx context.Context, keyID id.KeyID, reason string) error {
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return fmt.Errorf("get key: %w", err)
	}

	now := time.Now()
	k.State = key.StateRevoked
	k.RevokedAt = &now
	k.UpdatedAt = now

	if err := e.store.Keys().Update(ctx, k); err != nil {
		return fmt.Errorf("update key: %w", err)
	}

	_ = e.hooks.FireKeyRevoked(ctx, k, reason)
	return nil
}

// SuspendKey temporarily disables a key.
func (e *Engine) SuspendKey(ctx context.Context, keyID id.KeyID) error {
	if err := e.store.Keys().UpdateState(ctx, keyID, key.StateSuspended); err != nil {
		return fmt.Errorf("suspend key: %w", err)
	}
	k, _ := e.store.Keys().Get(ctx, keyID)
	if k != nil {
		_ = e.hooks.FireKeySuspended(ctx, k)
	}
	return nil
}

// ReactivateKey re-enables a suspended key.
func (e *Engine) ReactivateKey(ctx context.Context, keyID id.KeyID) error {
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return fmt.Errorf("get key: %w", err)
	}
	if k.State != key.StateSuspended {
		return ErrInvalidStateTransition
	}
	if err := e.store.Keys().UpdateState(ctx, keyID, key.StateActive); err != nil {
		return fmt.Errorf("reactivate key: %w", err)
	}
	_ = e.hooks.FireKeyReactivated(ctx, k)
	return nil
}

// GetKey returns a key by ID.
func (e *Engine) GetKey(ctx context.Context, keyID id.KeyID) (*key.Key, error) {
	return e.store.Keys().Get(ctx, keyID)
}

// ListKeys returns keys matching the filter.
func (e *Engine) ListKeys(ctx context.Context, filter *key.ListFilter) ([]*key.Key, error) {
	return e.store.Keys().List(ctx, filter)
}

// ──────────────────────────────────────────────────
// Policy Management
// ──────────────────────────────────────────────────

// CreatePolicy creates a new key policy.
func (e *Engine) CreatePolicy(ctx context.Context, pol *policy.Policy) error {
	sc := scopeFromContext(ctx)
	pol.ID = id.NewPolicyID()
	pol.TenantID = sc.tenantID
	pol.AppID = sc.appID
	now := time.Now()
	pol.CreatedAt = now
	pol.UpdatedAt = now
	if err := e.store.Policies().Create(ctx, pol); err != nil {
		return fmt.Errorf("create policy: %w", err)
	}
	_ = e.hooks.FirePolicyCreated(ctx, pol)
	return nil
}

// GetPolicy returns a policy by ID.
func (e *Engine) GetPolicy(ctx context.Context, polID id.PolicyID) (*policy.Policy, error) {
	return e.store.Policies().Get(ctx, polID)
}

// UpdatePolicy updates an existing policy.
func (e *Engine) UpdatePolicy(ctx context.Context, pol *policy.Policy) error {
	pol.UpdatedAt = time.Now()
	if err := e.store.Policies().Update(ctx, pol); err != nil {
		return fmt.Errorf("update policy: %w", err)
	}
	_ = e.hooks.FirePolicyUpdated(ctx, pol)
	return nil
}

// DeletePolicy deletes a policy by ID.
func (e *Engine) DeletePolicy(ctx context.Context, polID id.PolicyID) error {
	keys, err := e.store.Keys().ListByPolicy(ctx, polID)
	if err != nil {
		return fmt.Errorf("list keys by policy: %w", err)
	}
	if len(keys) > 0 {
		return ErrPolicyInUse
	}
	if err := e.store.Policies().Delete(ctx, polID); err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	_ = e.hooks.FirePolicyDeleted(ctx, polID)
	return nil
}

// ListPolicies returns policies matching the filter.
func (e *Engine) ListPolicies(ctx context.Context, filter *policy.ListFilter) ([]*policy.Policy, error) {
	return e.store.Policies().List(ctx, filter)
}

// ──────────────────────────────────────────────────
// Scope Management
// ──────────────────────────────────────────────────

// CreateScope creates a permission scope.
func (e *Engine) CreateScope(ctx context.Context, s *scope.Scope) error {
	sc := scopeFromContext(ctx)
	s.ID = id.NewScopeID()
	s.TenantID = sc.tenantID
	s.AppID = sc.appID
	s.CreatedAt = time.Now()
	return e.store.Scopes().Create(ctx, s)
}

// ListScopes returns scopes for the tenant.
func (e *Engine) ListScopes(ctx context.Context, filter *scope.ListFilter) ([]*scope.Scope, error) {
	return e.store.Scopes().List(ctx, filter)
}

// DeleteScope deletes a scope by ID.
func (e *Engine) DeleteScope(ctx context.Context, scopeID id.ScopeID) error {
	return e.store.Scopes().Delete(ctx, scopeID)
}

// AssignScopes assigns scopes to a key by name.
func (e *Engine) AssignScopes(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	return e.store.Scopes().AssignToKey(ctx, keyID, scopeNames)
}

// RemoveScopes removes scopes from a key by name.
func (e *Engine) RemoveScopes(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	return e.store.Scopes().RemoveFromKey(ctx, keyID, scopeNames)
}

// ──────────────────────────────────────────────────
// Usage & Analytics
// ──────────────────────────────────────────────────

// RecordUsage records a single usage event for a key.
func (e *Engine) RecordUsage(ctx context.Context, rec *usage.Record) error {
	rec.ID = id.NewUsageID()
	rec.CreatedAt = time.Now()
	return e.store.Usages().Record(ctx, rec)
}

// QueryUsage queries usage records.
func (e *Engine) QueryUsage(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Record, error) {
	return e.store.Usages().Query(ctx, filter)
}

// AggregateUsage returns aggregated usage statistics.
func (e *Engine) AggregateUsage(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Aggregation, error) {
	return e.store.Usages().Aggregate(ctx, filter)
}

// ListRotations returns rotation records matching the filter.
func (e *Engine) ListRotations(ctx context.Context, filter *rotation.ListFilter) ([]*rotation.Record, error) {
	return e.store.Rotations().List(ctx, filter)
}

// ──────────────────────────────────────────────────
// Cleanup
// ──────────────────────────────────────────────────

// CleanupExpiredKeys finds and marks expired keys.
func (e *Engine) CleanupExpiredKeys(ctx context.Context) error {
	keys, err := e.store.Keys().ListExpired(ctx, time.Now())
	if err != nil {
		return fmt.Errorf("list expired keys: %w", err)
	}
	for _, k := range keys {
		if err := e.store.Keys().UpdateState(ctx, k.ID, key.StateExpired); err != nil {
			e.logger.Warn("failed to expire key", "key_id", k.ID.String(), "error", err)
			continue
		}
		_ = e.hooks.FireKeyExpired(ctx, k)
	}
	return nil
}

// CleanupGraceExpired revokes keys whose grace period has ended.
func (e *Engine) CleanupGraceExpired(ctx context.Context) error {
	recs, err := e.store.Rotations().ListPendingGrace(ctx, time.Now())
	if err != nil {
		return fmt.Errorf("list pending grace: %w", err)
	}
	for _, rec := range recs {
		if time.Now().After(rec.GraceEnds) {
			if err := e.store.Keys().UpdateState(ctx, rec.KeyID, key.StateRevoked); err != nil {
				e.logger.Warn("failed to revoke grace-expired key", "key_id", rec.KeyID.String(), "error", err)
			}
		}
	}
	return nil
}
