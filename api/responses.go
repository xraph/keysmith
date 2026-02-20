package api

import (
	"time"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/usage"
)

// KeyResponse is the API representation of a key (raw key is never included).
type KeyResponse struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	AppID       string         `json:"app_id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Prefix      string         `json:"prefix"`
	Hint        string         `json:"hint"`
	Environment string         `json:"environment"`
	State       string         `json:"state"`
	PolicyID    string         `json:"policy_id,omitempty"`
	Scopes      []string       `json:"scopes,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedBy   string         `json:"created_by,omitempty"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time     `json:"last_used_at,omitempty"`
	RotatedAt   *time.Time     `json:"rotated_at,omitempty"`
	RevokedAt   *time.Time     `json:"revoked_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// KeyCreateResponse includes the raw key (shown only once at creation).
type KeyCreateResponse struct {
	Key    *KeyResponse `json:"key"`
	RawKey string       `json:"raw_key"`
}

// PolicyResponse is the API representation of a policy.
type PolicyResponse struct {
	ID              string         `json:"id"`
	TenantID        string         `json:"tenant_id"`
	AppID           string         `json:"app_id"`
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	RateLimit       int            `json:"rate_limit"`
	RateLimitWindow string         `json:"rate_limit_window"`
	BurstLimit      int            `json:"burst_limit"`
	AllowedScopes   []string       `json:"allowed_scopes,omitempty"`
	AllowedIPs      []string       `json:"allowed_ips,omitempty"`
	AllowedOrigins  []string       `json:"allowed_origins,omitempty"`
	AllowedMethods  []string       `json:"allowed_methods,omitempty"`
	AllowedPaths    []string       `json:"allowed_paths,omitempty"`
	MaxKeyLifetime  string         `json:"max_key_lifetime,omitempty"`
	RotationPeriod  string         `json:"rotation_period,omitempty"`
	GracePeriod     string         `json:"grace_period"`
	DailyQuota      int64          `json:"daily_quota"`
	MonthlyQuota    int64          `json:"monthly_quota"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// ScopeResponse is the API representation of a scope.
type ScopeResponse struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id"`
	AppID       string         `json:"app_id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parent      string         `json:"parent,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// UsageResponse is the API representation of a usage record.
type UsageResponse struct {
	ID         string         `json:"id"`
	KeyID      string         `json:"key_id"`
	TenantID   string         `json:"tenant_id"`
	Endpoint   string         `json:"endpoint"`
	Method     string         `json:"method"`
	StatusCode int            `json:"status_code"`
	IPAddress  string         `json:"ip_address,omitempty"`
	UserAgent  string         `json:"user_agent,omitempty"`
	LatencyMs  int64          `json:"latency_ms"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

// AggregationResponse is the API representation of aggregated usage.
type AggregationResponse struct {
	KeyID        string    `json:"key_id"`
	TenantID     string    `json:"tenant_id"`
	Period       string    `json:"period"`
	PeriodStart  time.Time `json:"period_start"`
	RequestCount int64     `json:"request_count"`
	ErrorCount   int64     `json:"error_count"`
	TotalLatency int64     `json:"total_latency_ms"`
	P50Latency   int64     `json:"p50_latency_ms"`
	P99Latency   int64     `json:"p99_latency_ms"`
}

// RotationResponse is the API representation of a rotation record.
type RotationResponse struct {
	ID        string    `json:"id"`
	KeyID     string    `json:"key_id"`
	TenantID  string    `json:"tenant_id"`
	Reason    string    `json:"reason"`
	GraceTTL  string    `json:"grace_ttl"`
	GraceEnds time.Time `json:"grace_ends"`
	RotatedBy string    `json:"rotated_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ValidationResponse is the API representation of a key validation result.
type ValidationResponse struct {
	Valid  bool         `json:"valid"`
	Key    *KeyResponse `json:"key,omitempty"`
	Scopes []string     `json:"scopes,omitempty"`
}

// ── Mapper functions ─────────────────────────────────

func toKeyResponse(k *key.Key) *KeyResponse {
	r := &KeyResponse{
		ID:          k.ID.String(),
		TenantID:    k.TenantID,
		AppID:       k.AppID,
		Name:        k.Name,
		Description: k.Description,
		Prefix:      k.Prefix,
		Hint:        k.Hint,
		Environment: string(k.Environment),
		State:       string(k.State),
		Scopes:      k.Scopes,
		Metadata:    k.Metadata,
		CreatedBy:   k.CreatedBy,
		ExpiresAt:   k.ExpiresAt,
		LastUsedAt:  k.LastUsedAt,
		RotatedAt:   k.RotatedAt,
		RevokedAt:   k.RevokedAt,
		CreatedAt:   k.CreatedAt,
		UpdatedAt:   k.UpdatedAt,
	}
	if k.PolicyID != nil {
		r.PolicyID = k.PolicyID.String()
	}
	return r
}

func toPolicyResponse(p *policy.Policy) *PolicyResponse {
	return &PolicyResponse{
		ID:              p.ID.String(),
		TenantID:        p.TenantID,
		AppID:           p.AppID,
		Name:            p.Name,
		Description:     p.Description,
		RateLimit:       p.RateLimit,
		RateLimitWindow: p.RateLimitWindow.String(),
		BurstLimit:      p.BurstLimit,
		AllowedScopes:   p.AllowedScopes,
		AllowedIPs:      p.AllowedIPs,
		AllowedOrigins:  p.AllowedOrigins,
		AllowedMethods:  p.AllowedMethods,
		AllowedPaths:    p.AllowedPaths,
		MaxKeyLifetime:  p.MaxKeyLifetime.String(),
		RotationPeriod:  p.RotationPeriod.String(),
		GracePeriod:     p.GracePeriod.String(),
		DailyQuota:      p.DailyQuota,
		MonthlyQuota:    p.MonthlyQuota,
		Metadata:        p.Metadata,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

func toScopeResponse(s *scope.Scope) *ScopeResponse {
	return &ScopeResponse{
		ID:          s.ID.String(),
		TenantID:    s.TenantID,
		AppID:       s.AppID,
		Name:        s.Name,
		Description: s.Description,
		Parent:      s.Parent,
		Metadata:    s.Metadata,
		CreatedAt:   s.CreatedAt,
	}
}

func toUsageResponse(r *usage.Record) *UsageResponse {
	return &UsageResponse{
		ID:         r.ID.String(),
		KeyID:      r.KeyID.String(),
		TenantID:   r.TenantID,
		Endpoint:   r.Endpoint,
		Method:     r.Method,
		StatusCode: r.StatusCode,
		IPAddress:  r.IPAddress,
		UserAgent:  r.UserAgent,
		LatencyMs:  r.Latency.Milliseconds(),
		Metadata:   r.Metadata,
		CreatedAt:  r.CreatedAt,
	}
}

func toAggregationResponse(a *usage.Aggregation) *AggregationResponse {
	return &AggregationResponse{
		KeyID:        a.KeyID.String(),
		TenantID:     a.TenantID,
		Period:       a.Period,
		PeriodStart:  a.PeriodStart,
		RequestCount: a.RequestCount,
		ErrorCount:   a.ErrorCount,
		TotalLatency: a.TotalLatency,
		P50Latency:   a.P50Latency,
		P99Latency:   a.P99Latency,
	}
}

func toRotationResponse(r *rotation.Record) *RotationResponse {
	return &RotationResponse{
		ID:        r.ID.String(),
		KeyID:     r.KeyID.String(),
		TenantID:  r.TenantID,
		Reason:    string(r.Reason),
		GraceTTL:  r.GraceTTL.String(),
		GraceEnds: r.GraceEnds,
		RotatedBy: r.RotatedBy,
		CreatedAt: r.CreatedAt,
	}
}

func toValidationResponse(v *keysmith.ValidationResult) *ValidationResponse {
	resp := &ValidationResponse{
		Valid: v.Key != nil,
	}
	if v.Key != nil {
		resp.Key = toKeyResponse(v.Key)
	}
	resp.Scopes = v.Scopes
	return resp
}
