// Package key defines the core API key entity and its lifecycle states.
package key

import (
	"time"

	"github.com/xraph/keysmith/id"
)

// State represents the lifecycle state of an API key.
type State string

const (
	// StateActive indicates the key is valid and usable.
	StateActive State = "active"

	// StateRotated indicates the old key after rotation; grace period applies.
	StateRotated State = "rotated"

	// StateExpired indicates the key has passed its expiration time.
	StateExpired State = "expired"

	// StateRevoked indicates the key has been permanently disabled.
	StateRevoked State = "revoked"

	// StateSuspended indicates the key is temporarily disabled.
	StateSuspended State = "suspended"
)

// Environment represents the key environment.
type Environment string

const (
	// EnvLive is the production environment.
	EnvLive Environment = "live"

	// EnvTest is the testing environment.
	EnvTest Environment = "test"

	// EnvStaging is the staging environment.
	EnvStaging Environment = "staging"
)

// Key is the core API key entity. The raw key value is never persisted;
// only the hash is stored. The raw key is returned exactly once at creation.
type Key struct {
	ID          id.KeyID       `json:"id" db:"id"`
	TenantID    string         `json:"tenant_id" db:"tenant_id"`
	AppID       string         `json:"app_id" db:"app_id"`
	Name        string         `json:"name" db:"name"`
	Description string         `json:"description,omitempty" db:"description"`
	Prefix      string         `json:"prefix" db:"prefix"`
	Hint        string         `json:"hint" db:"hint"`
	KeyHash     string         `json:"-" db:"key_hash"`
	Environment Environment    `json:"environment" db:"environment"`
	State       State          `json:"state" db:"state"`
	PolicyID    *id.PolicyID   `json:"policy_id,omitempty" db:"policy_id"`
	Scopes      []string       `json:"scopes,omitempty" db:"-"`
	Metadata    map[string]any `json:"metadata,omitempty" db:"metadata"`
	CreatedBy   string         `json:"created_by,omitempty" db:"created_by"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty" db:"expires_at"`
	LastUsedAt  *time.Time     `json:"last_used_at,omitempty" db:"last_used_at"`
	RotatedAt   *time.Time     `json:"rotated_at,omitempty" db:"rotated_at"`
	RevokedAt   *time.Time     `json:"revoked_at,omitempty" db:"revoked_at"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}

// CreateResult is returned from key creation. The RawKey is shown exactly once.
type CreateResult struct {
	Key    *Key   `json:"key"`
	RawKey string `json:"raw_key"`
}

// ListFilter contains filters for listing keys.
type ListFilter struct {
	TenantID    string       `json:"tenant_id,omitempty"`
	Environment Environment  `json:"environment,omitempty"`
	State       State        `json:"state,omitempty"`
	PolicyID    *id.PolicyID `json:"policy_id,omitempty"`
	CreatedBy   string       `json:"created_by,omitempty"`
	Limit       int          `json:"limit,omitempty"`
	Offset      int          `json:"offset,omitempty"`
}
