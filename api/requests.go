package api

import "time"

// ── Key DTOs ──────────────────────────────────────

// CreateKeyRequest is the request for creating an API key.
type CreateKeyRequest struct {
	Name        string         `json:"name" description:"Human-readable key name"`
	Description string         `json:"description" description:"Optional description"`
	Prefix      string         `json:"prefix" description:"Key prefix (e.g., sk, pk)"`
	Environment string         `json:"environment" description:"Environment (live, test, staging)"`
	PolicyID    string         `json:"policy_id" description:"Optional policy ID to attach"`
	Scopes      []string       `json:"scopes" description:"Permission scopes to assign"`
	Metadata    map[string]any `json:"metadata" description:"Arbitrary metadata"`
	ExpiresAt   *time.Time     `json:"expires_at" description:"Optional expiration time"`
}

// ListKeysRequest is the request for listing keys.
type ListKeysRequest struct {
	Environment string `query:"environment" description:"Filter by environment"`
	State       string `query:"state" description:"Filter by state (active, revoked, expired)"`
	PolicyID    string `query:"policy_id" description:"Filter by policy ID"`
	Limit       int    `query:"limit" description:"Max results (default: 50)"`
	Offset      int    `query:"offset" description:"Number of results to skip"`
}

// GetKeyRequest is the request for fetching a single key.
type GetKeyRequest struct {
	KeyID string `path:"keyId" description:"Key ID"`
}

// DeleteKeyRequest is the request for deleting a key.
type DeleteKeyRequest struct {
	KeyID string `path:"keyId" description:"Key ID"`
}

// RotateKeyRequest is the request for rotating a key.
type RotateKeyRequest struct {
	KeyID  string `path:"keyId" description:"Key ID to rotate"`
	Reason string `json:"reason" description:"Rotation reason (manual, compromise, policy)"`
}

// RevokeKeyRequest is the request for revoking a key.
type RevokeKeyRequest struct {
	KeyID  string `path:"keyId" description:"Key ID to revoke"`
	Reason string `json:"reason" description:"Revocation reason"`
}

// ValidateKeyRequest is the request for validating a raw key.
type ValidateKeyRequest struct {
	RawKey string `json:"raw_key" description:"The raw API key to validate"`
}

// ── Policy DTOs ───────────────────────────────────

// CreatePolicyRequest is the request for creating a policy.
type CreatePolicyRequest struct {
	Name            string   `json:"name" description:"Policy name"`
	Description     string   `json:"description" description:"Optional description"`
	RateLimit       int      `json:"rate_limit" description:"Max requests per window"`
	RateLimitWindow string   `json:"rate_limit_window" description:"Window duration (e.g., 1m, 1h)"`
	BurstLimit      int      `json:"burst_limit" description:"Burst allowance"`
	AllowedScopes   []string `json:"allowed_scopes" description:"Scopes this policy grants"`
	AllowedIPs      []string `json:"allowed_ips" description:"IP allowlist (CIDR)"`
	AllowedOrigins  []string `json:"allowed_origins" description:"Origin allowlist"`
	MaxKeyLifetime  string   `json:"max_key_lifetime" description:"Max key lifetime (e.g., 90d)"`
	RotationPeriod  string   `json:"rotation_period" description:"Suggested rotation period (e.g., 30d)"`
	GracePeriod     string   `json:"grace_period" description:"Rotated key grace period (e.g., 24h)"`
	DailyQuota      int64    `json:"daily_quota" description:"Max requests per day (0 = unlimited)"`
	MonthlyQuota    int64    `json:"monthly_quota" description:"Max requests per month (0 = unlimited)"`
}

// UpdatePolicyRequest is the request for updating a policy.
type UpdatePolicyRequest struct {
	PolicyID string `path:"policyId" description:"Policy ID"`
	CreatePolicyRequest
}

// ListPoliciesRequest is the request for listing policies.
type ListPoliciesRequest struct {
	Limit  int `query:"limit" description:"Max results (default: 50)"`
	Offset int `query:"offset" description:"Number of results to skip"`
}

// ── Scope DTOs ────────────────────────────────────

// CreateScopeRequest is the request for creating a scope.
type CreateScopeRequest struct {
	Name        string `json:"name" description:"Scope name (e.g., read:users)"`
	Description string `json:"description" description:"Optional description"`
	Parent      string `json:"parent" description:"Parent scope (e.g., read)"`
}

// ListScopesRequest is the request for listing scopes.
type ListScopesRequest struct {
	Parent string `query:"parent" description:"Filter by parent scope"`
	Limit  int    `query:"limit" description:"Max results (default: 50)"`
	Offset int    `query:"offset" description:"Number of results to skip"`
}

// AssignScopesRequest is the request for assigning scopes to a key.
type AssignScopesRequest struct {
	KeyID  string   `path:"keyId" description:"Key ID"`
	Scopes []string `json:"scopes" description:"Scope names to assign"`
}

// RemoveScopesRequest is the request for removing scopes from a key.
type RemoveScopesRequest struct {
	KeyID  string   `path:"keyId" description:"Key ID"`
	Scopes []string `json:"scopes" description:"Scope names to remove"`
}

// ── Usage DTOs ────────────────────────────────────

// GetKeyUsageRequest is the request for fetching key usage.
type GetKeyUsageRequest struct {
	KeyID  string `path:"keyId" description:"Key ID"`
	After  string `query:"after" description:"After timestamp (ISO 8601)"`
	Before string `query:"before" description:"Before timestamp (ISO 8601)"`
	Limit  int    `query:"limit" description:"Max results (default: 100)"`
	Offset int    `query:"offset" description:"Number of results to skip"`
}

// GetKeyUsageAggregateRequest is the request for aggregated usage.
type GetKeyUsageAggregateRequest struct {
	KeyID  string `path:"keyId" description:"Key ID"`
	Period string `query:"period" description:"Aggregation period (hour, day, month)"`
	After  string `query:"after" description:"After timestamp (ISO 8601)"`
	Before string `query:"before" description:"Before timestamp (ISO 8601)"`
}

// ListUsageRequest is the request for listing tenant-wide usage.
type ListUsageRequest struct {
	Period string `query:"period" description:"Aggregation period (hour, day, month)"`
	After  string `query:"after" description:"After timestamp (ISO 8601)"`
	Before string `query:"before" description:"Before timestamp (ISO 8601)"`
}

// ── Rotation DTOs ─────────────────────────────────

// ListRotationsRequest is the request for listing rotations.
type ListRotationsRequest struct {
	KeyID  string `path:"keyId" description:"Key ID"`
	Limit  int    `query:"limit" description:"Max results (default: 50)"`
	Offset int    `query:"offset" description:"Number of results to skip"`
}
