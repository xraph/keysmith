// Package usage defines API key usage records and aggregation types.
package usage

import (
	"time"

	"github.com/xraph/keysmith/id"
)

// Record is a single usage event for a key.
type Record struct {
	ID         id.UsageID     `json:"id" db:"id"`
	KeyID      id.KeyID       `json:"key_id" db:"key_id"`
	TenantID   string         `json:"tenant_id" db:"tenant_id"`
	Endpoint   string         `json:"endpoint" db:"endpoint"`
	Method     string         `json:"method" db:"method"`
	StatusCode int            `json:"status_code" db:"status_code"`
	IPAddress  string         `json:"ip_address" db:"ip_address"`
	UserAgent  string         `json:"user_agent,omitempty" db:"user_agent"`
	Latency    time.Duration  `json:"latency" db:"latency_ms"`
	Metadata   map[string]any `json:"metadata,omitempty" db:"metadata"`
	CreatedAt  time.Time      `json:"created_at" db:"created_at"`
}

// Aggregation represents aggregated usage statistics.
type Aggregation struct {
	KeyID        id.KeyID  `json:"key_id"`
	TenantID     string    `json:"tenant_id"`
	Period       string    `json:"period"`
	PeriodStart  time.Time `json:"period_start"`
	RequestCount int64     `json:"request_count"`
	ErrorCount   int64     `json:"error_count"`
	TotalLatency int64     `json:"total_latency_ms"`
	P50Latency   int64     `json:"p50_latency_ms"`
	P99Latency   int64     `json:"p99_latency_ms"`
}

// QueryFilter contains filters for querying usage.
type QueryFilter struct {
	KeyID    *id.KeyID  `json:"key_id,omitempty"`
	TenantID string     `json:"tenant_id,omitempty"`
	After    *time.Time `json:"after,omitempty"`
	Before   *time.Time `json:"before,omitempty"`
	Period   string     `json:"period,omitempty"`
	Limit    int        `json:"limit,omitempty"`
	Offset   int        `json:"offset,omitempty"`
}
