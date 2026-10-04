package keysmith

import (
	"errors"

	"github.com/xraph/keysmith/store"
)

var (
	// ErrInvalidKey is returned when the provided API key is not valid.
	ErrInvalidKey = errors.New("keysmith: invalid API key")

	// ErrKeyInactive is returned when the key is not in an active state.
	ErrKeyInactive = errors.New("keysmith: key is not active")

	// ErrKeyExpired is returned when the key has expired.
	ErrKeyExpired = errors.New("keysmith: key has expired")

	// ErrKeyRevoked is returned when the key has been permanently revoked.
	ErrKeyRevoked = errors.New("keysmith: key has been revoked")

	// ErrKeySuspended is returned when the key is temporarily suspended.
	ErrKeySuspended = errors.New("keysmith: key is suspended")

	// ErrRateLimited is returned when the key exceeds its rate limit.
	ErrRateLimited = errors.New("keysmith: rate limit exceeded")

	// ErrQuotaExceeded is returned when the key exceeds its usage quota.
	ErrQuotaExceeded = errors.New("keysmith: usage quota exceeded")

	// ErrInvalidStateTransition is returned for illegal key state changes.
	ErrInvalidStateTransition = errors.New("keysmith: invalid state transition")

	// ErrPolicyInUse is returned when deleting a policy assigned to active keys.
	ErrPolicyInUse = errors.New("keysmith: policy is assigned to active keys")

	// ErrPolicyNameTaken is returned when a policy is created or renamed to a
	// name another policy in the same tenant already has.
	ErrPolicyNameTaken = errors.New("keysmith: a policy with this name already exists in the tenant")

	// ErrScopeNameTaken is returned when a scope is created with a name
	// another scope in the same tenant already has.
	ErrScopeNameTaken = errors.New("keysmith: a scope with this name already exists in the tenant")

	// ErrPolicyNotFound is returned when a policy cannot be found.
	ErrPolicyNotFound = store.ErrPolicyNotFound

	// ErrKeyNotFound is returned when a key cannot be found.
	ErrKeyNotFound = store.ErrKeyNotFound

	// ErrScopeNotFound is returned when a scope cannot be found.
	ErrScopeNotFound = store.ErrScopeNotFound

	// ErrKeyLifetimeExceeded is returned when a key is created with an expiry
	// further out than its policy's MaxKeyLifetime allows.
	ErrKeyLifetimeExceeded = errors.New("keysmith: expiry is beyond the policy's maximum key lifetime")

	// ErrScopeNotAllowed is returned when a scope is not permitted by the policy.
	ErrScopeNotAllowed = errors.New("keysmith: scope not allowed by policy")

	// ErrIPNotAllowed is returned when the IP address is not in the allowlist.
	ErrIPNotAllowed = errors.New("keysmith: IP address not allowed")

	// ErrOriginNotAllowed is returned when the origin is not in the allowlist.
	ErrOriginNotAllowed = errors.New("keysmith: origin not allowed")

	// ErrRotationNotFound is returned when a rotation record cannot be found.
	ErrRotationNotFound = store.ErrRotationNotFound
)
