package keysmith

import (
	"context"
	"errors"
	"fmt"
	"time"

	log "github.com/xraph/go-utils/log"

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
	logger      log.Logger
}

// NewEngine creates a new Keysmith engine with the given options.
func NewEngine(opts ...Option) (*Engine, error) {
	e := &Engine{
		hasher:    DefaultHasher(),
		generator: DefaultKeyGenerator(),
		hooks:     plugin.NewManager(),
		logger:    log.NewNoopLogger(),
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

// RateLimiterConfigured reports whether a RateLimiter was injected. Without
// one, a policy's RateLimit is stored and never enforced.
func (e *Engine) RateLimiterConfigured() bool { return e.ratelimiter != nil }

// Health checks the health of the engine by pinging its store.
func (e *Engine) Health(ctx context.Context) error {
	return e.store.Ping(ctx)
}

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

	var pol *policy.Policy
	if input.PolicyID != nil {
		var polErr error
		pol, polErr = e.store.Policies().Get(ctx, *input.PolicyID)
		if polErr != nil {
			return nil, fmt.Errorf("get policy: %w", polErr)
		}
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
	if pol != nil && pol.MaxKeyLifetime > 0 && input.ExpiresAt == nil {
		expiry := now.Add(pol.MaxKeyLifetime)
		k.ExpiresAt = &expiry
	}

	// Refuse unknown or disallowed scopes before anything is written.
	if err := e.checkScopes(ctx, tenantID, pol, input.Scopes); err != nil {
		return nil, err
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

	var viaPrevious bool
	var graceEnds *time.Time
	k, err := e.store.Keys().GetByHash(ctx, hash)
	if err != nil {
		// The hash is not any key's current hash. It may still be the hash a
		// key had before a rotation whose grace window is open.
		rec, recErr := e.store.Rotations().GetInGraceByOldHash(ctx, hash, time.Now())
		// A record with no OldHint was written before the grace fix, when
		// RotateKey recorded a window on every rotation (compromise ones
		// too) but killed the old key at once. That window was never
		// honoured, so it must not open now. The close_legacy_grace_windows
		// migration closes these rows; this guard covers a store where it
		// has not run, such as mongo's index-only Store.Migrate.
		if recErr != nil || rec.OldHint == "" {
			_ = e.hooks.FireKeyValidationFailed(ctx, rawKey, err)
			return nil, ErrInvalidKey
		}
		k, err = e.store.Keys().Get(ctx, rec.KeyID)
		if err != nil {
			_ = e.hooks.FireKeyValidationFailed(ctx, rawKey, err)
			return nil, ErrInvalidKey
		}
		viaPrevious = true
		ends := rec.GraceEnds
		graceEnds = &ends
	}

	// Check state. Suspended, revoked and expired keys fail here whichever
	// hash the caller presented.
	if k.State != key.StateActive {
		_ = e.hooks.FireKeyValidationFailed(ctx, rawKey, ErrKeyInactive)
		return nil, ErrKeyInactive
	}

	// Check expiration.
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		_ = e.store.Keys().UpdateState(ctx, k.ID, key.StateExpired)
		_ = e.hooks.FireKeyExpired(ctx, k)
		return nil, ErrKeyExpired
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
		_ = e.store.Keys().UpdateLastUsed(context.WithoutCancel(ctx), k.ID, now)
	}()

	_ = e.hooks.FireKeyValidated(ctx, k)

	return &ValidationResult{
		Key:            k,
		Scopes:         scopeNames,
		Policy:         pol,
		ViaPreviousKey: viaPrevious,
		GraceEnds:      graceEnds,
	}, nil
}

// RotateKey issues a new raw key for the same key record. The previous raw key
// keeps validating until its grace window ends. The window comes from
// WithGrace if given, else the key's policy GracePeriod, else 24 hours. A
// window of zero stops the previous key at once. A revoked or expired key
// cannot be rotated.
func (e *Engine) RotateKey(ctx context.Context, keyID id.KeyID, reason rotation.Reason, opts ...RotateOption) (*key.CreateResult, error) {
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("get key: %w", err)
	}
	if k.State == key.StateRevoked || k.State == key.StateExpired ||
		(k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt)) {
		return nil, ErrInvalidStateTransition
	}

	var cfg rotateConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	// Grace period: the caller's choice, then the policy's, then 24 hours.
	graceTTL := 24 * time.Hour
	switch {
	case cfg.grace != nil:
		graceTTL = *cfg.grace
	case k.PolicyID != nil:
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
	oldHint := k.Hint
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
		OldHint:    oldHint,
		NewHint:    k.Hint,
		RotatedBy:  cfg.rotatedBy,
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

// RevokeKey permanently disables a key. Revocation is terminal: no state
// change leads out of it, and revoking twice is refused so hooks fire once.
func (e *Engine) RevokeKey(ctx context.Context, keyID id.KeyID, reason string) error {
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return fmt.Errorf("get key: %w", err)
	}
	if k.State == key.StateRevoked {
		return ErrInvalidStateTransition
	}

	now := time.Now()
	k.State = key.StateRevoked
	k.RevokedAt = &now
	k.UpdatedAt = now

	if err := e.store.Keys().Update(ctx, k); err != nil {
		return fmt.Errorf("update key: %w", err)
	}

	// A revoked key must not be reachable through a previous key either.
	if _, err := e.store.Rotations().EndGrace(ctx, keyID, now); err != nil {
		return fmt.Errorf("end grace: %w", err)
	}

	_ = e.hooks.FireKeyRevoked(ctx, k, reason)
	return nil
}

// EndGrace closes every open grace window on a key now, so every previous
// key stops validating. It returns how many windows it closed.
func (e *Engine) EndGrace(ctx context.Context, keyID id.KeyID) (int64, error) {
	if _, err := e.store.Keys().Get(ctx, keyID); err != nil {
		return 0, fmt.Errorf("get key: %w", err)
	}
	n, err := e.store.Rotations().EndGrace(ctx, keyID, time.Now())
	if err != nil {
		return 0, fmt.Errorf("end grace: %w", err)
	}
	return n, nil
}

// SuspendKey temporarily disables an active key. Only an active key can be
// suspended; anything else, a revoked key above all, is refused.
func (e *Engine) SuspendKey(ctx context.Context, keyID id.KeyID) error {
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return fmt.Errorf("get key: %w", err)
	}
	if k.State != key.StateActive {
		return ErrInvalidStateTransition
	}
	if err := e.store.Keys().UpdateState(ctx, keyID, key.StateSuspended); err != nil {
		return fmt.Errorf("suspend key: %w", err)
	}
	k.State = key.StateSuspended
	_ = e.hooks.FireKeySuspended(ctx, k)
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
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return fmt.Errorf("get key: %w", err)
	}
	var pol *policy.Policy
	if k.PolicyID != nil {
		pol, err = e.store.Policies().Get(ctx, *k.PolicyID)
		if err != nil {
			return fmt.Errorf("get policy: %w", err)
		}
	}
	if err := e.checkScopes(ctx, k.TenantID, pol, scopeNames); err != nil {
		return err
	}
	return e.store.Scopes().AssignToKey(ctx, keyID, scopeNames)
}

// checkScopes resolves every scope name in the tenant and, when the policy
// lists allowed scopes, checks each against it. It runs before anything is
// written, so a refused create or assignment leaves no trace. An empty
// AllowedScopes means the policy does not restrict scopes.
func (e *Engine) checkScopes(ctx context.Context, tenantID string, pol *policy.Policy, names []string) error {
	var allowed map[string]bool
	if pol != nil && len(pol.AllowedScopes) > 0 {
		allowed = make(map[string]bool, len(pol.AllowedScopes))
		for _, s := range pol.AllowedScopes {
			allowed[s] = true
		}
	}
	for _, name := range names {
		if _, err := e.store.Scopes().GetByName(ctx, tenantID, name); err != nil {
			return fmt.Errorf("scope %q: %w", name, err)
		}
		if allowed != nil && !allowed[name] {
			return fmt.Errorf("scope %q: %w", name, ErrScopeNotAllowed)
		}
	}
	return nil
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
			e.logger.Warn("failed to expire key", log.String("key_id", k.ID.String()), log.Any("error", err))
			continue
		}
		_ = e.hooks.FireKeyExpired(ctx, k)
	}
	return nil
}
