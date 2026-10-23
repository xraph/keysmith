package keysmith

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	log "github.com/xraph/go-utils/log"
	gometrics "github.com/xraph/go-utils/metrics"
	"go.opentelemetry.io/otel/trace"

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
	metrics     gometrics.Metrics
	tel         *telemetry
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
	e.tel = newTelemetry(e.metrics)
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
	ctx, op := e.startOperation(ctx, opCreate, spanCreateKey)
	res, err := e.createKey(ctx, input)
	op.end(err)
	return res, err
}

func (e *Engine) createKey(ctx context.Context, input *CreateKeyInput) (*key.CreateResult, error) {
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

	// MaxKeyLifetime is a cap. An explicit expiry beyond it is refused
	// before anything is written; no expiry defaults to the cap below.
	now := time.Now()
	if pol != nil && pol.MaxKeyLifetime > 0 && input.ExpiresAt != nil &&
		input.ExpiresAt.After(now.Add(pol.MaxKeyLifetime)) {
		return nil, ErrKeyLifetimeExceeded
	}

	rawKey, err := e.generator.Generate(input.Prefix, input.Environment)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	hash, err := e.hasher.Hash(rawKey)
	if err != nil {
		return nil, fmt.Errorf("hash key: %w", err)
	}

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
		e.hookDone(ctx, hookKeyCreateFailed, e.hooks.FireKeyCreateFailed(ctx, k, err))
		return nil, fmt.Errorf("store key: %w", err)
	}

	// Assign scopes.
	if len(input.Scopes) > 0 {
		if err := e.store.Scopes().AssignToKey(ctx, k.ID, input.Scopes); err != nil {
			// The raw key is never returned on this path, so the stored
			// key is unreachable. Delete it rather than leave a live key
			// that has none of the scopes the caller asked for. The caller
			// may have cancelled ctx, which must not stop the cleanup.
			assignErr := fmt.Errorf("assign scopes: %w", err)
			if delErr := e.store.Keys().Delete(context.WithoutCancel(ctx), k.ID); delErr != nil {
				return nil, errors.Join(assignErr, fmt.Errorf("delete key after failed scope assignment: %w", delErr))
			}
			return nil, assignErr
		}
		k.Scopes = input.Scopes
	}

	e.hookDone(ctx, hookKeyCreated, e.hooks.FireKeyCreated(ctx, k))

	return &key.CreateResult{Key: k, RawKey: rawKey}, nil
}

// lookupFailure turns a failed key-store lookup into the error ValidateKey
// returns, or nil when err is the store's not-found result (notFound), which
// means the key is simply unknown. A cancelled or expired ctx wins over
// whatever the store reported, so the caller sees context.Canceled or
// context.DeadlineExceeded, and a not-found result under such a ctx is
// dropped rather than passed on. Anything else is a store failure and is wrapped,
// so callers can tell it from ErrInvalidKey with errors.Is.
func lookupFailure(ctx context.Context, err, notFound error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		// A miss seen under a dead context says nothing about the key, so
		// the not-found sentinel stays out of the chain: callers map it to
		// a 404 or a rejection. Any other store error is kept for diagnosis.
		if errors.Is(err, notFound) {
			return fmt.Errorf("keysmith: lookup key: %w", ctxErr)
		}
		return fmt.Errorf("keysmith: lookup key: %w", errors.Join(ctxErr, err))
	}
	if errors.Is(err, notFound) {
		return nil
	}
	return fmt.Errorf("keysmith: lookup key: %w", err)
}

// ValidateKey validates a raw API key and returns the key record if valid.
// This is the hot path — optimized for speed.
//
// ErrInvalidKey means the key is not known. If the key store cannot answer,
// or ctx is cancelled or past its deadline, ValidateKey returns an error
// that wraps the cause (a store error, context.Canceled or
// context.DeadlineExceeded) and is not ErrInvalidKey, and it does not fire
// the KeyValidationFailed hook. Callers should answer such an error with a
// retryable 5xx, not a 401.
func (e *Engine) ValidateKey(ctx context.Context, rawKey string) (*ValidationResult, error) {
	start := time.Now()
	ctx, span := startSpan(ctx, spanValidateKey)
	res, out, err := e.validateKey(ctx, rawKey)
	e.tel.validated(out.result, out.viaPrevious, time.Since(start))
	if span.IsRecording() {
		span.SetAttributes(attrResult.String(out.result), attrViaPreviousKey.Bool(out.viaPrevious))
		if out.result == validationError {
			markError(span, out.cause)
		}
	}
	span.End()
	return res, err
}

// validation is what ValidateKey reports to its span and metrics. cause is
// the failure behind result "error". It is kept apart from the returned
// error, which wraps it, so the span records the store's own error type and
// not keysmith's wrapper.
type validation struct {
	result      string
	viaPrevious bool
	cause       error
}

// lookupFailed sorts a lookup that lookupFailure refused to call a miss. A
// done ctx is the cause whatever the store said, as it is for the returned
// error, so a caller that hung up reads "cancelled" and one that ran out of
// time reads "error". Otherwise the store's error is the cause.
func lookupFailed(ctx context.Context, err error) validation {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return storeFailed(ctxErr)
	}
	return storeFailed(err)
}

// storeFailed sorts a read that failed during validation: "cancelled" when
// the caller's context was cancelled, "error" with the cause otherwise.
func storeFailed(err error) validation {
	if cancelled(err) {
		return validation{result: validationCancelled}
	}
	return validation{result: validationError, cause: err}
}

func (e *Engine) validateKey(ctx context.Context, rawKey string) (*ValidationResult, validation, error) {
	hash, err := e.hasher.Hash(rawKey)
	if err != nil {
		return nil, validation{result: validationError, cause: err}, fmt.Errorf("hash key: %w", err)
	}

	var out validation
	var graceEnds *time.Time
	k, err := e.store.Keys().GetByHash(ctx, hash)
	if err != nil {
		// Only a miss says the key does not exist. A store that cannot
		// answer (an outage, a cancelled request) must not read as an
		// invalid key, or a caller treats a retryable fault as a
		// permanent refusal.
		if lookupErr := lookupFailure(ctx, err, store.ErrKeyNotFound); lookupErr != nil {
			return nil, lookupFailed(ctx, err), lookupErr
		}

		// The hash is not any key's current hash. It may still be the hash a
		// key had before a rotation whose grace window is open.
		rec, recErr := e.store.Rotations().GetInGraceByOldHash(ctx, hash, time.Now())
		if recErr != nil {
			if lookupErr := lookupFailure(ctx, recErr, store.ErrRotationNotFound); lookupErr != nil {
				return nil, lookupFailed(ctx, recErr), lookupErr
			}
			e.hookDone(ctx, hookKeyValidationFailed, e.hooks.FireKeyValidationFailed(ctx, rawKey, err))
			return nil, validation{result: validationInvalid}, ErrInvalidKey
		}
		// A record with no OldHint was written before the grace fix, when
		// RotateKey recorded a window on every rotation (compromise ones
		// too) but killed the old key at once. That window was never
		// honoured, so it must not open now. The close_legacy_grace_windows
		// migration closes these rows; this guard covers a store where it
		// has not run, such as mongo's index-only Store.Migrate.
		if rec.OldHint == "" {
			e.hookDone(ctx, hookKeyValidationFailed, e.hooks.FireKeyValidationFailed(ctx, rawKey, err))
			return nil, validation{result: validationInvalid}, ErrInvalidKey
		}
		k, err = e.store.Keys().Get(ctx, rec.KeyID)
		if err != nil {
			if lookupErr := lookupFailure(ctx, err, store.ErrKeyNotFound); lookupErr != nil {
				return nil, lookupFailed(ctx, err), lookupErr
			}
			e.hookDone(ctx, hookKeyValidationFailed, e.hooks.FireKeyValidationFailed(ctx, rawKey, err))
			return nil, validation{result: validationInvalid}, ErrInvalidKey
		}
		out.viaPrevious = true
		ends := rec.GraceEnds
		graceEnds = &ends
	}

	// Check state. Suspended, revoked and expired keys fail here whichever
	// hash the caller presented. RevokedAt is final even when State says
	// otherwise: a stale full-row write racing a revoke can put State back
	// and leave RevokedAt set.
	if k.State != key.StateActive || k.RevokedAt != nil {
		e.hookDone(ctx, hookKeyValidationFailed, e.hooks.FireKeyValidationFailed(ctx, rawKey, ErrKeyInactive))
		out.result = validationInactive
		return nil, out, ErrKeyInactive
	}

	// Check expiration.
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		e.tel.operation(opExpire, operationResult(e.store.Keys().UpdateState(ctx, k.ID, key.StateExpired)))
		e.hookDone(ctx, hookKeyExpired, e.hooks.FireKeyExpired(ctx, k))
		out.result = validationExpired
		return nil, out, ErrKeyExpired
	}

	// Load the policy. A read failure fails closed: carrying on with a nil
	// policy would skip the rate limit and hand the host a result it reads
	// as "no restrictions". A policy that no longer exists (a dangling
	// PolicyID) is not an outage, so the key validates without one.
	var pol *policy.Policy
	if k.PolicyID != nil {
		var polErr error
		pol, polErr = e.store.Policies().Get(ctx, *k.PolicyID)
		if polErr != nil {
			if !errors.Is(polErr, ErrPolicyNotFound) {
				failed := storeFailed(polErr)
				out.result, out.cause = failed.result, failed.cause
				return nil, out, fmt.Errorf("load policy: %w", polErr)
			}
			pol = nil
		}
	}

	// Rate-limit check.
	if pol != nil && e.ratelimiter != nil && pol.RateLimit > 0 {
		if limit := e.allow(ctx, k.ID.String(), pol); limit != rateLimitAllowed {
			e.hookDone(ctx, hookKeyRateLimited, e.hooks.FireKeyRateLimited(ctx, k))
			out.result = validationRateLimited
			if limit == rateLimitCancelled {
				out.result = validationCancelled
			}
			return nil, out, ErrRateLimited
		}
	}

	// Load scopes.
	scopes, _ := e.store.Scopes().ListByKey(ctx, k.ID)
	scopeNames := make([]string, len(scopes))
	for i, s := range scopes {
		scopeNames[i] = s.Name
	}

	// Update last-used timestamp asynchronously. Nobody waits for it, so a
	// failure is only counted.
	go func() {
		if luErr := e.store.Keys().UpdateLastUsed(context.WithoutCancel(ctx), k.ID, time.Now()); luErr != nil {
			e.tel.lastUsedUpdateFailed()
		}
	}()

	e.hookDone(ctx, hookKeyValidated, e.hooks.FireKeyValidated(ctx, k))

	out.result = validationOK
	return &ValidationResult{
		Key:            k,
		Scopes:         scopeNames,
		Policy:         pol,
		ViaPreviousKey: out.viaPrevious,
		GraceEnds:      graceEnds,
	}, out, nil
}

// allow asks the rate limiter whether the key may make another request and
// returns its answer as a rate limit result. A limiter that fails denies the
// request, as it always has; the span and keysmith_rate_limit_checks_total
// tell the failure apart from a real denial. limiterKey is the key's ID,
// which goes to the limiter and nowhere else.
func (e *Engine) allow(ctx context.Context, limiterKey string, pol *policy.Policy) string {
	ctx, span := startSpan(ctx, spanCheckRateLimit)
	allowed, err := e.ratelimiter.Allow(ctx, limiterKey, pol.RateLimit, pol.RateLimitWindow)
	result := rateLimitAllowed
	switch {
	case err != nil && cancelled(err):
		result = rateLimitCancelled
	case err != nil:
		result = rateLimitError
		markError(span, err)
	case !allowed:
		result = rateLimitDenied
	}
	span.SetAttributes(attrResult.String(result))
	span.End()
	e.tel.rateLimitChecked(result)
	return result
}

// RotateKey issues a new raw key for the same key record. The previous raw key
// keeps validating until its grace window ends. The window comes from
// WithGrace if given, else the key's policy GracePeriod, else 24 hours. A
// window of zero stops the previous key at once. A revoked or expired key
// cannot be rotated, and a key with RevokedAt set counts as revoked. If
// another write reaches the key between the read and the write (a revoke,
// for example), the rotation is refused with ErrKeyConflict and the key keeps
// the other write.
func (e *Engine) RotateKey(ctx context.Context, keyID id.KeyID, reason rotation.Reason, opts ...RotateOption) (*key.CreateResult, error) {
	ctx, op := e.startOperation(ctx, opRotate, spanRotateKey, attrRotationReason.String(rotationReason(reason)))
	res, err := e.rotateKey(ctx, keyID, reason, opts...)
	op.end(err)
	return res, err
}

func (e *Engine) rotateKey(ctx context.Context, keyID id.KeyID, reason rotation.Reason, opts ...RotateOption) (*key.CreateResult, error) {
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("get key: %w", err)
	}
	if k.State == key.StateRevoked || k.State == key.StateExpired || k.RevokedAt != nil ||
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
		// A policy that is gone keeps the 24h default. Any other read
		// failure stops the rotation, so the key is left as it was.
		pol, polErr := e.store.Policies().Get(ctx, *k.PolicyID)
		switch {
		case polErr == nil:
			if pol.GracePeriod > 0 {
				graceTTL = pol.GracePeriod
			}
		case !errors.Is(polErr, ErrPolicyNotFound):
			return nil, fmt.Errorf("get policy: %w", polErr)
		}
	}
	trace.SpanFromContext(ctx).SetAttributes(attrGraceSeconds.Int64(int64(graceTTL / time.Second)))

	// Generate new key.
	rawKey, err := e.generator.Generate(k.Prefix, k.Environment)
	if err != nil {
		return nil, fmt.Errorf("generate new key: %w", err)
	}

	newHash, err := e.hasher.Hash(rawKey)
	if err != nil {
		return nil, fmt.Errorf("hash new key: %w", err)
	}

	now := time.Now()
	newHint := rawKey[len(rawKey)-4:]

	// Record the rotation before touching the key. Until the key update
	// lands, the old hash is still the key's current hash, so GetByHash
	// finds it and callers on the old key never fall into a gap. If the key
	// update then fails, the record stays as a history row. When nothing
	// else wrote the key, the fallback cannot reach it: it only runs for a
	// hash that is no key's current hash, and the old hash still is one.
	// When the update lost a race (ErrKeyConflict), another write moved the
	// key on, and if that write was a rotation the old hash is no longer
	// current. This record would then hold the old key's window open as long
	// as this call asked for, past whatever the winner chose, so the
	// conflict path below ends this record's window. It ends only this one:
	// EndGrace would close the winner's window too.
	rec := &rotation.Record{
		ID:         id.NewRotationID(),
		KeyID:      k.ID,
		TenantID:   k.TenantID,
		OldKeyHash: k.KeyHash,
		NewKeyHash: newHash,
		OldHint:    k.Hint,
		NewHint:    newHint,
		RotatedBy:  cfg.rotatedBy,
		Reason:     reason,
		GraceTTL:   graceTTL,
		GraceEnds:  now.Add(graceTTL),
		CreatedAt:  now,
	}
	if err := e.store.Rotations().Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("record rotation: %w", err)
	}

	k.KeyHash = newHash
	k.Hint = newHint
	k.RotatedAt = &now
	k.UpdatedAt = now

	if err := e.store.Keys().UpdateIfVersion(ctx, k, k.Version); err != nil {
		err = fmt.Errorf("update key: %w", err)
		if errors.Is(err, ErrKeyConflict) {
			// The caller may have gone, but the window must still close.
			if endErr := e.store.Rotations().EndGraceByID(context.WithoutCancel(ctx), rec.ID, time.Now()); endErr != nil {
				err = errors.Join(err, fmt.Errorf("end grace of the refused rotation: %w", endErr))
			}
		}
		return nil, err
	}

	e.hookDone(ctx, hookKeyRotated, e.hooks.FireKeyRotated(ctx, k, rec))

	return &key.CreateResult{Key: k, RawKey: rawKey}, nil
}

// RevokeKey permanently disables a key. Revocation is terminal: no state
// change leads out of it, and revoking twice is refused so hooks fire once.
// If the key changes between the read and the write, the revoke is refused
// with ErrKeyConflict and the caller can read the key again and retry.
func (e *Engine) RevokeKey(ctx context.Context, keyID id.KeyID, reason string) error {
	ctx, op := e.startOperation(ctx, opRevoke, spanRevokeKey)
	err := e.revokeKey(ctx, keyID, reason)
	op.end(err)
	return err
}

func (e *Engine) revokeKey(ctx context.Context, keyID id.KeyID, reason string) error {
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

	if err := e.store.Keys().UpdateIfVersion(ctx, k, k.Version); err != nil {
		return fmt.Errorf("update key: %w", err)
	}

	// A revoked key must not be reachable through a previous key either.
	closed, endErr := e.store.Rotations().EndGrace(ctx, keyID, now)
	if endErr != nil {
		return fmt.Errorf("end grace: %w", endErr)
	}
	trace.SpanFromContext(ctx).SetAttributes(attrGraceWindowsClosed.Int64(closed))

	e.hookDone(ctx, hookKeyRevoked, e.hooks.FireKeyRevoked(ctx, k, reason))
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
// suspended; anything else, a revoked key above all, is refused. A key with
// RevokedAt set is revoked whatever its State says. The write is
// version-checked like RevokeKey's, so a key that changed after the read is
// refused with ErrKeyConflict.
func (e *Engine) SuspendKey(ctx context.Context, keyID id.KeyID) error {
	ctx, op := e.startOperation(ctx, opSuspend, spanSuspendKey)
	err := e.suspendKey(ctx, keyID)
	op.end(err)
	return err
}

func (e *Engine) suspendKey(ctx context.Context, keyID id.KeyID) error {
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return fmt.Errorf("get key: %w", err)
	}
	if k.State != key.StateActive || k.RevokedAt != nil {
		return ErrInvalidStateTransition
	}
	k.State = key.StateSuspended
	k.UpdatedAt = time.Now()
	if err := e.store.Keys().UpdateIfVersion(ctx, k, k.Version); err != nil {
		return fmt.Errorf("suspend key: %w", err)
	}
	e.hookDone(ctx, hookKeySuspended, e.hooks.FireKeySuspended(ctx, k))
	return nil
}

// ReactivateKey re-enables a suspended key. A key with RevokedAt set is
// revoked whatever its State says, and stays that way. The write is
// version-checked like RevokeKey's, so a key that changed after the read is
// refused with ErrKeyConflict.
func (e *Engine) ReactivateKey(ctx context.Context, keyID id.KeyID) error {
	ctx, op := e.startOperation(ctx, opReactivate, spanReactivateKey)
	err := e.reactivateKey(ctx, keyID)
	op.end(err)
	return err
}

func (e *Engine) reactivateKey(ctx context.Context, keyID id.KeyID) error {
	k, err := e.store.Keys().Get(ctx, keyID)
	if err != nil {
		return fmt.Errorf("get key: %w", err)
	}
	if k.State != key.StateSuspended || k.RevokedAt != nil {
		return ErrInvalidStateTransition
	}
	k.State = key.StateActive
	k.UpdatedAt = time.Now()
	if err := e.store.Keys().UpdateIfVersion(ctx, k, k.Version); err != nil {
		return fmt.Errorf("reactivate key: %w", err)
	}
	e.hookDone(ctx, hookKeyReactivated, e.hooks.FireKeyReactivated(ctx, k))
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
	if err := e.checkPolicyName(ctx, sc.tenantID, pol.Name, nil); err != nil {
		return err
	}
	pol.ID = id.NewPolicyID()
	pol.TenantID = sc.tenantID
	pol.AppID = sc.appID
	now := time.Now()
	pol.CreatedAt = now
	pol.UpdatedAt = now
	if err := e.store.Policies().Create(ctx, pol); err != nil {
		return fmt.Errorf("create policy: %w", err)
	}
	e.hookDone(ctx, hookPolicyCreated, e.hooks.FirePolicyCreated(ctx, pol))
	return nil
}

// GetPolicy returns a policy by ID.
func (e *Engine) GetPolicy(ctx context.Context, polID id.PolicyID) (*policy.Policy, error) {
	return e.store.Policies().Get(ctx, polID)
}

// UpdatePolicy updates an existing policy.
func (e *Engine) UpdatePolicy(ctx context.Context, pol *policy.Policy) error {
	if err := e.checkPolicyName(ctx, pol.TenantID, pol.Name, &pol.ID); err != nil {
		return err
	}
	pol.UpdatedAt = time.Now()
	if err := e.store.Policies().Update(ctx, pol); err != nil {
		return fmt.Errorf("update policy: %w", err)
	}
	e.hookDone(ctx, hookPolicyUpdated, e.hooks.FirePolicyUpdated(ctx, pol))
	return nil
}

// checkPolicyName refuses a name another policy in the tenant already has.
// self is the policy being updated, which may keep its own name; it is nil on
// create. The unique (tenant_id, name) index on the SQL and mongo backends
// stays as the backstop for two writes that race past this check.
func (e *Engine) checkPolicyName(ctx context.Context, tenantID, name string, self *id.PolicyID) error {
	found, err := e.store.Policies().GetByName(ctx, tenantID, name)
	switch {
	case errors.Is(err, ErrPolicyNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("check policy name: %w", err)
	case self != nil && found.ID.String() == self.String():
		return nil
	default:
		return ErrPolicyNameTaken
	}
}

// DeletePolicy deletes a policy by ID. It refuses with ErrPolicyInUse while
// any key that is not revoked uses the policy. A revoked key never validates
// again and cannot be deleted, so it does not hold the policy; it keeps the
// policy's ID after the delete. A key with RevokedAt set counts as revoked
// whatever its State says, as it does everywhere else in the engine.
func (e *Engine) DeletePolicy(ctx context.Context, polID id.PolicyID) error {
	keys, err := e.store.Keys().ListByPolicy(ctx, polID)
	if err != nil {
		return fmt.Errorf("list keys by policy: %w", err)
	}
	for _, k := range keys {
		if k.State != key.StateRevoked && k.RevokedAt == nil {
			return ErrPolicyInUse
		}
	}
	if err := e.store.Policies().Delete(ctx, polID); err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	e.hookDone(ctx, hookPolicyDeleted, e.hooks.FirePolicyDeleted(ctx, polID))
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
	switch _, err := e.store.Scopes().GetByName(ctx, sc.tenantID, s.Name); {
	case err == nil:
		return ErrScopeNameTaken
	case !errors.Is(err, ErrScopeNotFound):
		return fmt.Errorf("check scope name: %w", err)
	}
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

// scopePolicyPage is how many policies DeleteScope reads per call while it
// looks for one that allows the scope.
const scopePolicyPage = 100

// DeleteScope deletes a scope by ID and takes it off every key that holds
// it. It refuses with ErrScopeHasChildren while another scope in the same
// tenant names it as its parent, then with ErrScopeAllowedByPolicy while a
// policy in the tenant lists it in AllowedScopes. It answers
// ErrScopeNotFound for a missing scope.
func (e *Engine) DeleteScope(ctx context.Context, scopeID id.ScopeID) error {
	s, err := e.store.Scopes().Get(ctx, scopeID)
	if err != nil {
		return err
	}
	children, err := e.store.Scopes().List(ctx, &scope.ListFilter{TenantID: s.TenantID, Parent: s.Name, Limit: 1})
	if err != nil {
		return fmt.Errorf("list child scopes: %w", err)
	}
	if len(children) > 0 {
		return ErrScopeHasChildren
	}
	for offset := 0; ; offset += scopePolicyPage {
		pols, err := e.store.Policies().List(ctx, &policy.ListFilter{TenantID: s.TenantID, Limit: scopePolicyPage, Offset: offset})
		if err != nil {
			return fmt.Errorf("list policies: %w", err)
		}
		for _, p := range pols {
			if slices.Contains(p.AllowedScopes, s.Name) {
				return ErrScopeAllowedByPolicy
			}
		}
		if len(pols) < scopePolicyPage {
			break
		}
	}
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

// CleanupExpiredKeys finds and marks expired keys. Keysmith never calls it:
// schedule it from your host.
//
// Each run that finds expired keys, or fails to list them, is one root
// span, expire keys, linked to the span in ctx when there is one. A run
// that finds nothing, or whose caller cancelled it before the listing came
// back, starts no span, so a frequent schedule doesn't mint an empty trace
// every tick. A run where any key failed to expire with a real error gets
// an error status carrying the first such error's type.
//
// The listing runs before the run's span exists, so it runs with the span
// stripped from ctx. A scheduler's ctx often comes from an extension's
// Start, which forge v1.12.9 calls inside a "start <ext>" span that ends at
// boot; kept, it would hang the listing query's db.client span off that
// finished trace on every idle tick. Cancellation and values carry over.
func (e *Engine) CleanupExpiredKeys(ctx context.Context) error {
	start := time.Now()
	listCtx := trace.ContextWithSpanContext(ctx, trace.SpanContext{})
	keys, err := e.store.Keys().ListExpired(listCtx, start)
	if err != nil {
		if !cancelled(err) {
			_, span := startSweep(ctx, start)
			markError(span, err)
			span.End()
		}
		return fmt.Errorf("list expired keys: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}

	ctx, span := startSweep(ctx, start)
	defer span.End()
	var expired, failed int
	var firstErr error
	for _, k := range keys {
		updErr := e.store.Keys().UpdateState(ctx, k.ID, key.StateExpired)
		result := operationResult(updErr)
		e.tel.operation(opExpire, result)
		if updErr != nil {
			failed++
			if firstErr == nil && result == resultError {
				firstErr = updErr
			}
			e.logger.Warn("failed to expire key", log.String("key_id", k.ID.String()), log.Any("error", updErr))
			continue
		}
		expired++
		e.hookDone(ctx, hookKeyExpired, e.hooks.FireKeyExpired(ctx, k))
	}
	span.SetAttributes(attrKeysExpired.Int(expired), attrKeysFailed.Int(failed))
	if firstErr != nil {
		markError(span, firstErr)
	}
	return nil
}

// startSweep starts the root span of one CleanupExpiredKeys run, back-dated
// to when the run began. A caller's span, a scheduler's job span for
// instance, becomes a link rather than the parent, so a run started from a
// long-lived context never hangs off an old trace.
func startSweep(ctx context.Context, start time.Time) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{trace.WithNewRoot(), trace.WithTimestamp(start)}
	if parent := trace.SpanContextFromContext(ctx); parent.IsValid() {
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: parent}))
	}
	return startSpan(ctx, spanExpireKeys, opts...)
}
