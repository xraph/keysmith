package keysmith

import (
	"time"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
)

// CreateKeyInput contains the parameters for creating a new API key.
type CreateKeyInput struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Prefix      string          `json:"prefix"`
	Environment key.Environment `json:"environment"`
	PolicyID    *id.PolicyID    `json:"policy_id,omitempty"`
	Scopes      []string        `json:"scopes,omitempty"`
	Metadata    map[string]any  `json:"metadata,omitempty"`
	CreatedBy   string          `json:"created_by,omitempty"`
	TenantID    string          `json:"tenant_id,omitempty"`
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
}

// ValidationResult is returned from key validation.
type ValidationResult struct {
	Key    *key.Key       `json:"key"`
	Scopes []string       `json:"scopes"`
	Policy *policy.Policy `json:"policy,omitempty"`
}
