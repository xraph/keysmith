package keysmith

import "errors"

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

	// ErrPolicyNotFound is returned when a policy cannot be found.
	ErrPolicyNotFound = errors.New("keysmith: policy not found")

	// ErrKeyNotFound is returned when a key cannot be found.
	ErrKeyNotFound = errors.New("keysmith: key not found")

	// ErrScopeNotFound is returned when a scope cannot be found.
	ErrScopeNotFound = errors.New("keysmith: scope not found")

	// ErrScopeNotAllowed is returned when a scope is not permitted by the policy.
	ErrScopeNotAllowed = errors.New("keysmith: scope not allowed by policy")

	// ErrIPNotAllowed is returned when the IP address is not in the allowlist.
	ErrIPNotAllowed = errors.New("keysmith: IP address not allowed")

	// ErrOriginNotAllowed is returned when the origin is not in the allowlist.
	ErrOriginNotAllowed = errors.New("keysmith: origin not allowed")

	// ErrRotationNotFound is returned when a rotation record cannot be found.
	ErrRotationNotFound = errors.New("keysmith: rotation record not found")
)
