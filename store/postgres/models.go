package postgres

import (
	"time"

	"github.com/xraph/grove"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/usage"
)

// ──────────────────────────────────────────────────
// Key model
// ──────────────────────────────────────────────────

type keyModel struct {
	grove.BaseModel `grove:"table:keysmith_keys"`
	ID              string         `grove:"id,pk"`
	TenantID        string         `grove:"tenant_id,notnull"`
	AppID           string         `grove:"app_id,notnull"`
	Name            string         `grove:"name,notnull"`
	Description     string         `grove:"description"`
	Prefix          string         `grove:"prefix,notnull"`
	Hint            string         `grove:"hint,notnull"`
	KeyHash         string         `grove:"key_hash,notnull"`
	Environment     string         `grove:"environment,notnull"`
	State           string         `grove:"state,notnull"`
	PolicyID        *string        `grove:"policy_id"`
	Metadata        map[string]any `grove:"metadata,type:jsonb"`
	CreatedBy       string         `grove:"created_by"`
	ExpiresAt       *time.Time     `grove:"expires_at"`
	LastUsedAt      *time.Time     `grove:"last_used_at"`
	RotatedAt       *time.Time     `grove:"rotated_at"`
	RevokedAt       *time.Time     `grove:"revoked_at"`
	CreatedAt       time.Time      `grove:"created_at,notnull"`
	UpdatedAt       time.Time      `grove:"updated_at,notnull"`
}

func keyToModel(k *key.Key) *keyModel {
	m := &keyModel{
		ID:          k.ID.String(),
		TenantID:    k.TenantID,
		AppID:       k.AppID,
		Name:        k.Name,
		Description: k.Description,
		Prefix:      k.Prefix,
		Hint:        k.Hint,
		KeyHash:     k.KeyHash,
		Environment: string(k.Environment),
		State:       string(k.State),
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
		s := k.PolicyID.String()
		m.PolicyID = &s
	}
	return m
}

func keyFromModel(m *keyModel) (*key.Key, error) {
	kid, err := id.ParseKeyID(m.ID)
	if err != nil {
		return nil, err
	}
	k := &key.Key{
		ID:          kid,
		TenantID:    m.TenantID,
		AppID:       m.AppID,
		Name:        m.Name,
		Description: m.Description,
		Prefix:      m.Prefix,
		Hint:        m.Hint,
		KeyHash:     m.KeyHash,
		Environment: key.Environment(m.Environment),
		State:       key.State(m.State),
		Metadata:    m.Metadata,
		CreatedBy:   m.CreatedBy,
		ExpiresAt:   m.ExpiresAt,
		LastUsedAt:  m.LastUsedAt,
		RotatedAt:   m.RotatedAt,
		RevokedAt:   m.RevokedAt,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
	if m.PolicyID != nil {
		pid, err := id.ParsePolicyID(*m.PolicyID)
		if err != nil {
			return nil, err
		}
		k.PolicyID = &pid
	}
	return k, nil
}

// ──────────────────────────────────────────────────
// Policy model
// ──────────────────────────────────────────────────

type policyModel struct {
	grove.BaseModel `grove:"table:keysmith_policies"`
	ID              string         `grove:"id,pk"`
	TenantID        string         `grove:"tenant_id,notnull"`
	AppID           string         `grove:"app_id,notnull"`
	Name            string         `grove:"name,notnull"`
	Description     string         `grove:"description"`
	RateLimit       int            `grove:"rate_limit,notnull"`
	RateLimitWindow int64          `grove:"rate_limit_window,notnull"`
	BurstLimit      int            `grove:"burst_limit,notnull"`
	AllowedScopes   []string       `grove:"allowed_scopes,type:jsonb"`
	AllowedIPs      []string       `grove:"allowed_ips,type:jsonb"`
	AllowedOrigins  []string       `grove:"allowed_origins,type:jsonb"`
	AllowedMethods  []string       `grove:"allowed_methods,type:jsonb"`
	AllowedPaths    []string       `grove:"allowed_paths,type:jsonb"`
	MaxKeyLifetime  int64          `grove:"max_key_lifetime,notnull"`
	RotationPeriod  int64          `grove:"rotation_period,notnull"`
	GracePeriod     int64          `grove:"grace_period,notnull"`
	DailyQuota      int64          `grove:"daily_quota,notnull"`
	MonthlyQuota    int64          `grove:"monthly_quota,notnull"`
	Metadata        map[string]any `grove:"metadata,type:jsonb"`
	CreatedAt       time.Time      `grove:"created_at,notnull"`
	UpdatedAt       time.Time      `grove:"updated_at,notnull"`
}

func policyToModel(pol *policy.Policy) *policyModel {
	return &policyModel{
		ID:              pol.ID.String(),
		TenantID:        pol.TenantID,
		AppID:           pol.AppID,
		Name:            pol.Name,
		Description:     pol.Description,
		RateLimit:       pol.RateLimit,
		RateLimitWindow: pol.RateLimitWindow.Milliseconds(),
		BurstLimit:      pol.BurstLimit,
		AllowedScopes:   pol.AllowedScopes,
		AllowedIPs:      pol.AllowedIPs,
		AllowedOrigins:  pol.AllowedOrigins,
		AllowedMethods:  pol.AllowedMethods,
		AllowedPaths:    pol.AllowedPaths,
		MaxKeyLifetime:  pol.MaxKeyLifetime.Milliseconds(),
		RotationPeriod:  pol.RotationPeriod.Milliseconds(),
		GracePeriod:     pol.GracePeriod.Milliseconds(),
		DailyQuota:      pol.DailyQuota,
		MonthlyQuota:    pol.MonthlyQuota,
		Metadata:        pol.Metadata,
		CreatedAt:       pol.CreatedAt,
		UpdatedAt:       pol.UpdatedAt,
	}
}

func policyFromModel(m *policyModel) (*policy.Policy, error) {
	pid, err := id.ParsePolicyID(m.ID)
	if err != nil {
		return nil, err
	}
	return &policy.Policy{
		ID:              pid,
		TenantID:        m.TenantID,
		AppID:           m.AppID,
		Name:            m.Name,
		Description:     m.Description,
		RateLimit:       m.RateLimit,
		RateLimitWindow: time.Duration(m.RateLimitWindow) * time.Millisecond,
		BurstLimit:      m.BurstLimit,
		AllowedScopes:   m.AllowedScopes,
		AllowedIPs:      m.AllowedIPs,
		AllowedOrigins:  m.AllowedOrigins,
		AllowedMethods:  m.AllowedMethods,
		AllowedPaths:    m.AllowedPaths,
		MaxKeyLifetime:  time.Duration(m.MaxKeyLifetime) * time.Millisecond,
		RotationPeriod:  time.Duration(m.RotationPeriod) * time.Millisecond,
		GracePeriod:     time.Duration(m.GracePeriod) * time.Millisecond,
		DailyQuota:      m.DailyQuota,
		MonthlyQuota:    m.MonthlyQuota,
		Metadata:        m.Metadata,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}, nil
}

// ──────────────────────────────────────────────────
// Scope model
// ──────────────────────────────────────────────────

type scopeModel struct {
	grove.BaseModel `grove:"table:keysmith_scopes"`
	ID              string         `grove:"id,pk"`
	TenantID        string         `grove:"tenant_id,notnull"`
	AppID           string         `grove:"app_id,notnull"`
	Name            string         `grove:"name,notnull"`
	Description     string         `grove:"description"`
	Parent          *string        `grove:"parent"`
	Metadata        map[string]any `grove:"metadata,type:jsonb"`
	CreatedAt       time.Time      `grove:"created_at,notnull"`
}

// keyScopeModel represents the join table for key-scope assignments.
type keyScopeModel struct {
	grove.BaseModel `grove:"table:keysmith_key_scopes"`
	KeyID           string `grove:"key_id,pk"`
	ScopeID         string `grove:"scope_id,pk"`
}

func scopeToModel(sc *scope.Scope) *scopeModel {
	m := &scopeModel{
		ID:          sc.ID.String(),
		TenantID:    sc.TenantID,
		AppID:       sc.AppID,
		Name:        sc.Name,
		Description: sc.Description,
		Metadata:    sc.Metadata,
		CreatedAt:   sc.CreatedAt,
	}
	if sc.Parent != "" {
		m.Parent = &sc.Parent
	}
	return m
}

func scopeFromModel(m *scopeModel) (*scope.Scope, error) {
	sid, err := id.ParseScopeID(m.ID)
	if err != nil {
		return nil, err
	}
	sc := &scope.Scope{
		ID:          sid,
		TenantID:    m.TenantID,
		AppID:       m.AppID,
		Name:        m.Name,
		Description: m.Description,
		Metadata:    m.Metadata,
		CreatedAt:   m.CreatedAt,
	}
	if m.Parent != nil {
		sc.Parent = *m.Parent
	}
	return sc, nil
}

// ──────────────────────────────────────────────────
// Usage model
// ──────────────────────────────────────────────────

type usageModel struct {
	grove.BaseModel `grove:"table:keysmith_usage"`
	ID              string         `grove:"id,pk"`
	KeyID           string         `grove:"key_id,notnull"`
	TenantID        string         `grove:"tenant_id,notnull"`
	Endpoint        string         `grove:"endpoint,notnull"`
	Method          string         `grove:"method,notnull"`
	StatusCode      int            `grove:"status_code,notnull"`
	IPAddress       string         `grove:"ip_address"`
	UserAgent       string         `grove:"user_agent"`
	LatencyMs       int64          `grove:"latency_ms,notnull"`
	Metadata        map[string]any `grove:"metadata,type:jsonb"`
	CreatedAt       time.Time      `grove:"created_at,notnull"`
}

func usageToModel(rec *usage.Record) *usageModel {
	return &usageModel{
		ID:         rec.ID.String(),
		KeyID:      rec.KeyID.String(),
		TenantID:   rec.TenantID,
		Endpoint:   rec.Endpoint,
		Method:     rec.Method,
		StatusCode: rec.StatusCode,
		IPAddress:  rec.IPAddress,
		UserAgent:  rec.UserAgent,
		LatencyMs:  rec.Latency.Milliseconds(),
		Metadata:   rec.Metadata,
		CreatedAt:  rec.CreatedAt,
	}
}

func usageFromModel(m *usageModel) (*usage.Record, error) {
	uid, err := id.ParseUsageID(m.ID)
	if err != nil {
		return nil, err
	}
	kid, err := id.ParseKeyID(m.KeyID)
	if err != nil {
		return nil, err
	}
	return &usage.Record{
		ID:         uid,
		KeyID:      kid,
		TenantID:   m.TenantID,
		Endpoint:   m.Endpoint,
		Method:     m.Method,
		StatusCode: m.StatusCode,
		IPAddress:  m.IPAddress,
		UserAgent:  m.UserAgent,
		Latency:    time.Duration(m.LatencyMs) * time.Millisecond,
		Metadata:   m.Metadata,
		CreatedAt:  m.CreatedAt,
	}, nil
}

// usageAggModel represents aggregated usage statistics.
type usageAggModel struct {
	grove.BaseModel `grove:"table:keysmith_usage_agg"`
	KeyID           string    `grove:"key_id,pk"`
	TenantID        string    `grove:"tenant_id,notnull"`
	Period          string    `grove:"period,pk"`
	PeriodStart     time.Time `grove:"period_start,pk"`
	RequestCount    int64     `grove:"request_count,notnull"`
	ErrorCount      int64     `grove:"error_count,notnull"`
	TotalLatency    int64     `grove:"total_latency,notnull"`
	P50Latency      int64     `grove:"p50_latency,notnull"`
	P99Latency      int64     `grove:"p99_latency,notnull"`
}

func aggFromModel(m *usageAggModel) (*usage.Aggregation, error) {
	kid, err := id.ParseKeyID(m.KeyID)
	if err != nil {
		return nil, err
	}
	return &usage.Aggregation{
		KeyID:        kid,
		TenantID:     m.TenantID,
		Period:       m.Period,
		PeriodStart:  m.PeriodStart,
		RequestCount: m.RequestCount,
		ErrorCount:   m.ErrorCount,
		TotalLatency: m.TotalLatency,
		P50Latency:   m.P50Latency,
		P99Latency:   m.P99Latency,
	}, nil
}

// ──────────────────────────────────────────────────
// Rotation model
// ──────────────────────────────────────────────────

type rotationModel struct {
	grove.BaseModel `grove:"table:keysmith_rotations"`
	ID              string    `grove:"id,pk"`
	KeyID           string    `grove:"key_id,notnull"`
	TenantID        string    `grove:"tenant_id,notnull"`
	OldKeyHash      string    `grove:"old_key_hash,notnull"`
	NewKeyHash      string    `grove:"new_key_hash,notnull"`
	Reason          string    `grove:"reason,notnull"`
	GraceTTLMs      int64     `grove:"grace_ttl_ms,notnull"`
	GraceEnds       time.Time `grove:"grace_ends,notnull"`
	RotatedBy       string    `grove:"rotated_by"`
	CreatedAt       time.Time `grove:"created_at,notnull"`
}

func rotationToModel(rec *rotation.Record) *rotationModel {
	return &rotationModel{
		ID:         rec.ID.String(),
		KeyID:      rec.KeyID.String(),
		TenantID:   rec.TenantID,
		OldKeyHash: rec.OldKeyHash,
		NewKeyHash: rec.NewKeyHash,
		Reason:     string(rec.Reason),
		GraceTTLMs: rec.GraceTTL.Milliseconds(),
		GraceEnds:  rec.GraceEnds,
		RotatedBy:  rec.RotatedBy,
		CreatedAt:  rec.CreatedAt,
	}
}

func rotationFromModel(m *rotationModel) (*rotation.Record, error) {
	rid, err := id.ParseRotationID(m.ID)
	if err != nil {
		return nil, err
	}
	kid, err := id.ParseKeyID(m.KeyID)
	if err != nil {
		return nil, err
	}
	return &rotation.Record{
		ID:         rid,
		KeyID:      kid,
		TenantID:   m.TenantID,
		OldKeyHash: m.OldKeyHash,
		NewKeyHash: m.NewKeyHash,
		Reason:     rotation.Reason(m.Reason),
		GraceTTL:   time.Duration(m.GraceTTLMs) * time.Millisecond,
		GraceEnds:  m.GraceEnds,
		RotatedBy:  m.RotatedBy,
		CreatedAt:  m.CreatedAt,
	}, nil
}
