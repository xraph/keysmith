// Package usage defines API key usage records and aggregation types.
package usage

import (
	"errors"
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

// Aggregation periods accepted by Store.Aggregate. Buckets are UTC.
const (
	PeriodHourly  = "hourly"
	PeriodDaily   = "daily"
	PeriodMonthly = "monthly"
)

// ErrInvalidPeriod is returned when an aggregation period is not one of the
// Period constants.
var ErrInvalidPeriod = errors.New("keysmith: usage period must be hourly, daily or monthly")

// Truncate returns the UTC start of the bucket t falls in.
func Truncate(t time.Time, period string) (time.Time, error) {
	u := t.UTC()
	switch period {
	case PeriodHourly:
		return time.Date(u.Year(), u.Month(), u.Day(), u.Hour(), 0, 0, 0, time.UTC), nil
	case PeriodDaily:
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC), nil
	case PeriodMonthly:
		return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	default:
		return time.Time{}, ErrInvalidPeriod
	}
}

// Aggregation represents aggregated usage statistics for one bucket. It is
// computed from the raw usage rows on every call. KeyID is the zero value when
// the filter did not name a key and the bucket sums across keys.
type Aggregation struct {
	KeyID            id.KeyID  `json:"key_id"`
	TenantID         string    `json:"tenant_id"`
	Period           string    `json:"period"`
	PeriodStart      time.Time `json:"period_start"`
	RequestCount     int64     `json:"request_count"`
	ErrorCount       int64     `json:"error_count"`        // status >= 400
	ServerErrorCount int64     `json:"server_error_count"` // status >= 500
	TotalLatency     int64     `json:"total_latency_ms"`
	// P50Latency and P99Latency are never set. The four backends cannot
	// compute the same percentile, so none of them try. The fields stay so
	// the JSON shape does not break.
	P50Latency *int64 `json:"p50_latency_ms,omitempty"`
	P99Latency *int64 `json:"p99_latency_ms,omitempty"`
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
