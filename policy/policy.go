// Package policy defines key policies with rate limits, scopes, and restrictions.
package policy

import (
	"time"

	"github.com/xraph/keysmith/id"
)

// Policy defines the rules attached to one or more API keys.
// Policies are tenant-scoped and reusable across keys.
type Policy struct {
	ID              id.PolicyID    `json:"id" db:"id"`
	TenantID        string         `json:"tenant_id" db:"tenant_id"`
	AppID           string         `json:"app_id" db:"app_id"`
	Name            string         `json:"name" db:"name"`
	Description     string         `json:"description,omitempty" db:"description"`
	RateLimit       int            `json:"rate_limit" db:"rate_limit"`
	RateLimitWindow time.Duration  `json:"rate_limit_window" db:"rate_limit_window"`
	BurstLimit      int            `json:"burst_limit" db:"burst_limit"`
	AllowedScopes   []string       `json:"allowed_scopes,omitempty" db:"-"`
	AllowedIPs      []string       `json:"allowed_ips,omitempty" db:"-"`
	AllowedOrigins  []string       `json:"allowed_origins,omitempty" db:"-"`
	AllowedMethods  []string       `json:"allowed_methods,omitempty" db:"-"`
	AllowedPaths    []string       `json:"allowed_paths,omitempty" db:"-"`
	MaxKeyLifetime  time.Duration  `json:"max_key_lifetime,omitempty" db:"max_key_lifetime"`
	RotationPeriod  time.Duration  `json:"rotation_period,omitempty" db:"rotation_period"`
	GracePeriod     time.Duration  `json:"grace_period" db:"grace_period"`
	DailyQuota      int64          `json:"daily_quota,omitempty" db:"daily_quota"`
	MonthlyQuota    int64          `json:"monthly_quota,omitempty" db:"monthly_quota"`
	Metadata        map[string]any `json:"metadata,omitempty" db:"metadata"`
	CreatedAt       time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at" db:"updated_at"`
}

// ListFilter contains filters for listing policies.
type ListFilter struct {
	TenantID string `json:"tenant_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}
