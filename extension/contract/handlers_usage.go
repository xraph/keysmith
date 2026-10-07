package contract

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/usage"
)

// maxUsageBuckets caps a usage.series answer. 400 covers the widest range
// the Usage page offers (30 days daily, 12 months monthly) many times over,
// and stops a stray request asking for decades of hourly buckets.
const maxUsageBuckets = 400

type usageSeriesRequest struct {
	KeyID  string `json:"keyId"`
	Period string `json:"period"` // hourly | daily | monthly
	After  string `json:"after"`  // RFC3339, inclusive
	Before string `json:"before"` // RFC3339, exclusive
}

// UsageBucket is one period of a usage series. Every bucket in the range is
// present, empty ones included, so a chart never has to fill gaps.
type UsageBucket struct {
	Start        string `json:"start"`        // RFC3339 UTC bucket start
	Requests     int64  `json:"requests"`     // every recorded request
	ClientErrors int64  `json:"clientErrors"` // status 400 to 499
	ServerErrors int64  `json:"serverErrors"` // status 500 and up
	Succeeded    int64  `json:"succeeded"`    // status below 400
	// AvgLatencyMs is the mean latency rounded down, and null for an empty
	// bucket, where there is nothing to average.
	AvgLatencyMs *int64 `json:"avgLatencyMs"`
}

type usageSeriesResponse struct {
	Period  string        `json:"period"`
	Buckets []UsageBucket `json:"buckets"` // ascending, never null
	// Recorded is false when the tenant has no usage rows at all, at any
	// time, for any key. Usage exists only once the application calls
	// RecordUsage, and the page says so instead of drawing a flat chart.
	Recorded bool `json:"recorded"`
}

type usageRecordsRequest struct {
	KeyID  string `json:"keyId"`
	After  string `json:"after"`  // RFC3339, inclusive, optional
	Before string `json:"before"` // RFC3339, exclusive, optional
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// UsageRecordItem is one recorded request. The user agent and metadata stay
// out: the page does not show them, and metadata is whatever the
// application chose to attach.
type UsageRecordItem struct {
	ID         string `json:"id"`
	KeyID      string `json:"keyId"`
	Method     string `json:"method"`
	Endpoint   string `json:"endpoint"`
	StatusCode int    `json:"statusCode"`
	LatencyMs  int64  `json:"latencyMs"`
	IPAddress  string `json:"ipAddress,omitempty"`
	At         string `json:"at"`
}

type usageRecordsResponse struct {
	Items []UsageRecordItem `json:"items"` // never null
	Total int64             `json:"total"`
}

// usageSeriesHandler answers the tenant's usage as one bucket per period
// from the bucket after falls in up to before, with empty buckets filled in.
// Buckets are UTC whatever offset the request's times carry. keyId is a
// filter: another tenant's key or a missing one answers empty buckets.
func usageSeriesHandler(deps Deps) func(context.Context, usageSeriesRequest, dashcontract.Principal) (usageSeriesResponse, error) {
	return func(ctx context.Context, in usageSeriesRequest, p dashcontract.Principal) (usageSeriesResponse, error) {
		tenant, err := tenantFrom(ctx, p, deps)
		if err != nil {
			return usageSeriesResponse{}, err
		}
		switch in.Period {
		case usage.PeriodHourly, usage.PeriodDaily, usage.PeriodMonthly:
		default:
			return usageSeriesResponse{}, badRequest("period must be one of hourly, daily, monthly")
		}
		after, err := parseUsageTime("after", in.After, true)
		if err != nil {
			return usageSeriesResponse{}, err
		}
		before, err := parseUsageTime("before", in.Before, true)
		if err != nil {
			return usageSeriesResponse{}, err
		}
		if !before.After(*after) {
			return usageSeriesResponse{}, badRequest("before must be after after")
		}
		// The period was checked above, so Truncate cannot fail.
		first, _ := usage.Truncate(*after, in.Period)
		starts, ok := usageBucketStarts(first, *before, in.Period)
		if !ok {
			// Monthly is already the longest period, so only a shorter range
			// helps there.
			advice := "choose a longer period or a shorter range"
			if in.Period == usage.PeriodMonthly {
				advice = "choose a shorter range"
			}
			return usageSeriesResponse{}, badRequest(fmt.Sprintf(
				"this range has more than %d %s buckets; %s", maxUsageBuckets, in.Period, advice))
		}
		kid, err := parseUsageKeyID(in.KeyID)
		if err != nil {
			return usageSeriesResponse{}, err
		}

		// The store gets the whole first bucket, so a range that starts
		// mid-hour still shows that hour's full count.
		aggs, err := deps.Engine.AggregateUsage(ctx, &usage.QueryFilter{
			TenantID: tenant, KeyID: kid, After: &first, Before: before, Period: in.Period,
		})
		if err != nil {
			return usageSeriesResponse{}, deps.mapError("usage.series", err)
		}
		recorded, err := usageRecorded(ctx, deps, tenant)
		if err != nil {
			return usageSeriesResponse{}, deps.mapError("usage.series", err)
		}

		type sums struct{ requests, errs, serverErrs, latency int64 }
		byStart := make(map[int64]*sums, len(starts))
		for _, s := range starts {
			byStart[s.Unix()] = &sums{}
		}
		// Every row in [first, before) truncates to one of the starts. A
		// start that is not one of them means the store bucketed in another
		// zone or unit. The page still answers without it, and the operator
		// hears about it once per request, not once per bucket.
		var misaligned int
		var firstMisaligned time.Time
		for _, a := range aggs {
			b, ok := byStart[a.PeriodStart.Unix()]
			if !ok {
				if misaligned == 0 {
					firstMisaligned = a.PeriodStart
				}
				misaligned++
				continue
			}
			b.requests += a.RequestCount
			b.errs += a.ErrorCount
			b.serverErrs += a.ServerErrorCount
			b.latency += a.TotalLatency
		}
		if misaligned > 0 && deps.Logger != nil {
			deps.Logger.Warn("keysmith/contract: usage store answered buckets outside the requested ones",
				forge.F("intent", "usage.series"),
				forge.F("period", in.Period),
				forge.F("count", misaligned),
				forge.F("first_start", firstMisaligned.UTC().Format(time.RFC3339)),
			)
		}
		buckets := make([]UsageBucket, 0, len(starts))
		for _, s := range starts {
			b := byStart[s.Unix()]
			out := UsageBucket{
				Start:        rfc3339(s),
				Requests:     b.requests,
				ClientErrors: b.errs - b.serverErrs,
				ServerErrors: b.serverErrs,
				Succeeded:    b.requests - b.errs,
			}
			if b.requests > 0 {
				avg := b.latency / b.requests
				out.AvgLatencyMs = &avg
			}
			buckets = append(buckets, out)
		}
		return usageSeriesResponse{Period: in.Period, Buckets: buckets, Recorded: recorded}, nil
	}
}

// usageRecordsHandler answers the tenant's recorded requests, newest first
// in the store's order, with the total that match. Both times are optional.
func usageRecordsHandler(deps Deps) func(context.Context, usageRecordsRequest, dashcontract.Principal) (usageRecordsResponse, error) {
	return func(ctx context.Context, in usageRecordsRequest, p dashcontract.Principal) (usageRecordsResponse, error) {
		tenant, err := tenantFrom(ctx, p, deps)
		if err != nil {
			return usageRecordsResponse{}, err
		}
		after, err := parseUsageTime("after", in.After, false)
		if err != nil {
			return usageRecordsResponse{}, err
		}
		before, err := parseUsageTime("before", in.Before, false)
		if err != nil {
			return usageRecordsResponse{}, err
		}
		if after != nil && before != nil && !before.After(*after) {
			return usageRecordsResponse{}, badRequest("before must be after after")
		}
		kid, err := parseUsageKeyID(in.KeyID)
		if err != nil {
			return usageRecordsResponse{}, err
		}
		limit, offset := clampPage(in.Limit, in.Offset)

		recs, err := deps.Engine.QueryUsage(ctx, &usage.QueryFilter{
			TenantID: tenant, KeyID: kid, After: after, Before: before, Limit: limit, Offset: offset,
		})
		if err != nil {
			return usageRecordsResponse{}, deps.mapError("usage.records", err)
		}
		total, err := deps.Engine.Store().Usages().Count(ctx, &usage.QueryFilter{
			TenantID: tenant, KeyID: kid, After: after, Before: before,
		})
		if err != nil {
			return usageRecordsResponse{}, deps.mapError("usage.records", err)
		}
		items := make([]UsageRecordItem, 0, len(recs))
		for _, r := range recs {
			items = append(items, UsageRecordItem{
				ID:         r.ID.String(),
				KeyID:      r.KeyID.String(),
				Method:     r.Method,
				Endpoint:   r.Endpoint,
				StatusCode: r.StatusCode,
				LatencyMs:  r.Latency.Milliseconds(),
				IPAddress:  r.IPAddress,
				At:         rfc3339(r.CreatedAt),
			})
		}
		return usageRecordsResponse{Items: items, Total: total}, nil
	}
}

// usageRecorded reports whether tenant has any usage row at all, at any
// time and for any key. It asks for one row rather than counting them, since
// the usage table is the largest keysmith has. The error is the store's; the
// caller maps it.
func usageRecorded(ctx context.Context, deps Deps, tenant string) (bool, error) {
	recs, err := deps.Engine.QueryUsage(ctx, &usage.QueryFilter{TenantID: tenant, Limit: 1})
	if err != nil {
		return false, err
	}
	return len(recs) > 0, nil
}

// parseUsageTime reads an RFC3339 time from the request field name, in UTC.
// A blank value is nil when the field is optional.
func parseUsageTime(name, raw string, required bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return nil, badRequest(name + " is required")
		}
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, badRequest(name + " is not an RFC3339 time")
	}
	t = t.UTC()
	return &t, nil
}

// parseUsageKeyID reads the optional keyId filter. Blank means every key.
func parseUsageKeyID(raw string) (*id.KeyID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	kid, err := id.ParseKeyID(raw)
	if err != nil {
		return nil, badRequest("keyId is not a key id")
	}
	return &kid, nil
}

// usageBucketStarts lists every bucket start from first, a bucket start in
// UTC, while it is before before. It gives up, false, past maxUsageBuckets,
// so a huge range costs at most that many steps.
func usageBucketStarts(first, before time.Time, period string) ([]time.Time, bool) {
	var starts []time.Time
	for s := first; s.Before(before); s = nextUsageBucket(s, period) {
		if len(starts) == maxUsageBuckets {
			return nil, false
		}
		starts = append(starts, s)
	}
	return starts, true
}

// nextUsageBucket steps one period. s is a UTC bucket start, so a day is a
// calendar day and a month steps from the 1st and never skips February.
func nextUsageBucket(s time.Time, period string) time.Time {
	switch period {
	case usage.PeriodHourly:
		return s.Add(time.Hour)
	case usage.PeriodDaily:
		return s.AddDate(0, 0, 1)
	default:
		return s.AddDate(0, 1, 0)
	}
}
