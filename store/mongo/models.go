package mongo

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
	ID              string         `grove:"id,pk"          bson:"_id"`
	TenantID        string         `grove:"tenant_id"      bson:"tenant_id"`
	AppID           string         `grove:"app_id"         bson:"app_id"`
	Name            string         `grove:"name"           bson:"name"`
	Description     string         `grove:"description"    bson:"description"`
	Prefix          string         `grove:"prefix"         bson:"prefix"`
	Hint            string         `grove:"hint"           bson:"hint"`
	KeyHash         string         `grove:"key_hash"       bson:"key_hash"`
	Environment     string         `grove:"environment"    bson:"environment"`
	State           string         `grove:"state"          bson:"state"`
	PolicyID        *string        `grove:"policy_id"      bson:"policy_id,omitempty"`
	Metadata        map[string]any `grove:"metadata"       bson:"metadata,omitempty"`
	CreatedBy       string         `grove:"created_by"     bson:"created_by"`
	ExpiresAt       *time.Time     `grove:"expires_at"     bson:"expires_at,omitempty"`
	LastUsedAt      *time.Time     `grove:"last_used_at"   bson:"last_used_at,omitempty"`
	RotatedAt       *time.Time     `grove:"rotated_at"     bson:"rotated_at,omitempty"`
	RevokedAt       *time.Time     `grove:"revoked_at"     bson:"revoked_at,omitempty"`
	CreatedAt       time.Time      `grove:"created_at"     bson:"created_at"`
	UpdatedAt       time.Time      `grove:"updated_at"     bson:"updated_at"`
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
	ID              string         `grove:"id,pk"               bson:"_id"`
	TenantID        string         `grove:"tenant_id"           bson:"tenant_id"`
	AppID           string         `grove:"app_id"              bson:"app_id"`
	Name            string         `grove:"name"                bson:"name"`
	Description     string         `grove:"description"         bson:"description"`
	RateLimit       int            `grove:"rate_limit"          bson:"rate_limit"`
	RateLimitWindow int64          `grove:"rate_limit_window"   bson:"rate_limit_window_ms"`
	BurstLimit      int            `grove:"burst_limit"         bson:"burst_limit"`
	AllowedScopes   []string       `grove:"allowed_scopes"      bson:"allowed_scopes"`
	AllowedIPs      []string       `grove:"allowed_ips"         bson:"allowed_ips"`
	AllowedOrigins  []string       `grove:"allowed_origins"     bson:"allowed_origins"`
	AllowedMethods  []string       `grove:"allowed_methods"     bson:"allowed_methods"`
	AllowedPaths    []string       `grove:"allowed_paths"       bson:"allowed_paths"`
	MaxKeyLifetime  int64          `grove:"max_key_lifetime"    bson:"max_key_lifetime_ms"`
	RotationPeriod  int64          `grove:"rotation_period"     bson:"rotation_period_ms"`
	GracePeriod     int64          `grove:"grace_period"        bson:"grace_period_ms"`
	DailyQuota      int64          `grove:"daily_quota"         bson:"daily_quota"`
	MonthlyQuota    int64          `grove:"monthly_quota"       bson:"monthly_quota"`
	Metadata        map[string]any `grove:"metadata"            bson:"metadata,omitempty"`
	CreatedAt       time.Time      `grove:"created_at"          bson:"created_at"`
	UpdatedAt       time.Time      `grove:"updated_at"          bson:"updated_at"`
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
	ID              string         `grove:"id,pk"       bson:"_id"`
	TenantID        string         `grove:"tenant_id"   bson:"tenant_id"`
	AppID           string         `grove:"app_id"      bson:"app_id"`
	Name            string         `grove:"name"        bson:"name"`
	Description     string         `grove:"description" bson:"description"`
	Parent          *string        `grove:"parent"      bson:"parent,omitempty"`
	Metadata        map[string]any `grove:"metadata"    bson:"metadata,omitempty"`
	CreatedAt       time.Time      `grove:"created_at"  bson:"created_at"`
}

// keyScopeModel represents the join collection for key-scope assignments.
type keyScopeModel struct {
	grove.BaseModel `grove:"table:keysmith_key_scopes"`
	KeyID           string `grove:"key_id,pk"   bson:"key_id"`
	ScopeID         string `grove:"scope_id,pk" bson:"scope_id"`
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
	ID              string         `grove:"id,pk"        bson:"_id"`
	KeyID           string         `grove:"key_id"       bson:"key_id"`
	TenantID        string         `grove:"tenant_id"    bson:"tenant_id"`
	Endpoint        string         `grove:"endpoint"     bson:"endpoint"`
	Method          string         `grove:"method"       bson:"method"`
	StatusCode      int            `grove:"status_code"  bson:"status_code"`
	IPAddress       string         `grove:"ip_address"   bson:"ip_address"`
	UserAgent       string         `grove:"user_agent"   bson:"user_agent"`
	LatencyMs       int64          `grove:"latency_ms"   bson:"latency_ms"`
	Metadata        map[string]any `grove:"metadata"     bson:"metadata,omitempty"`
	CreatedAt       time.Time      `grove:"created_at"   bson:"created_at"`
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
	KeyID           string    `grove:"key_id,pk"       bson:"key_id"`
	TenantID        string    `grove:"tenant_id"       bson:"tenant_id"`
	Period          string    `grove:"period,pk"       bson:"period"`
	PeriodStart     time.Time `grove:"period_start,pk" bson:"period_start"`
	RequestCount    int64     `grove:"request_count"   bson:"request_count"`
	ErrorCount      int64     `grove:"error_count"     bson:"error_count"`
	TotalLatency    int64     `grove:"total_latency"   bson:"total_latency"`
	P50Latency      int64     `grove:"p50_latency"     bson:"p50_latency"`
	P99Latency      int64     `grove:"p99_latency"     bson:"p99_latency"`
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
	ID              string    `grove:"id,pk"         bson:"_id"`
	KeyID           string    `grove:"key_id"        bson:"key_id"`
	TenantID        string    `grove:"tenant_id"     bson:"tenant_id"`
	OldKeyHash      string    `grove:"old_key_hash"  bson:"old_key_hash"`
	NewKeyHash      string    `grove:"new_key_hash"  bson:"new_key_hash"`
	Reason          string    `grove:"reason"        bson:"reason"`
	GraceTTLMs      int64     `grove:"grace_ttl_ms"  bson:"grace_ttl_ms"`
	GraceEnds       time.Time `grove:"grace_ends"    bson:"grace_ends"`
	RotatedBy       string    `grove:"rotated_by"    bson:"rotated_by"`
	CreatedAt       time.Time `grove:"created_at"    bson:"created_at"`
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
