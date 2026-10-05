package storetest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/usage"
)

func usageAt(keyID id.KeyID, tenant string, at time.Time, status int, latency time.Duration) *usage.Record {
	return &usage.Record{ID: id.NewUsageID(), KeyID: keyID, TenantID: tenant, Endpoint: "/x", Method: "GET",
		StatusCode: status, Latency: latency, CreatedAt: at}
}

func TestAggregateDailyBuckets(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		day1 := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
		day2 := time.Date(2026, 9, 29, 23, 30, 0, 0, time.UTC)
		require.NoError(t, s.Usages().RecordBatch(ctx, []*usage.Record{
			usageAt(k.ID, "t1", day1, 200, 10*time.Millisecond),
			usageAt(k.ID, "t1", day1.Add(time.Hour), 404, 20*time.Millisecond),
			usageAt(k.ID, "t1", day2, 503, 30*time.Millisecond),
		}))
		after := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
		before := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
		aggs, err := s.Usages().Aggregate(ctx, &usage.QueryFilter{TenantID: "t1", Period: usage.PeriodDaily, After: &after, Before: &before})
		require.NoError(t, err)
		require.Len(t, aggs, 2)
		assert.True(t, aggs[0].PeriodStart.Equal(after))
		assert.EqualValues(t, 2, aggs[0].RequestCount)
		assert.EqualValues(t, 1, aggs[0].ErrorCount)
		assert.EqualValues(t, 0, aggs[0].ServerErrorCount)
		assert.EqualValues(t, 30, aggs[0].TotalLatency)
		assert.EqualValues(t, 1, aggs[1].ServerErrorCount)
		assert.Nil(t, aggs[0].P50Latency)
	})
}

func TestAggregateNeverCrossesTenants(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		a, b := newKey("t1"), newKey("t2")
		require.NoError(t, s.Keys().Create(ctx, a))
		require.NoError(t, s.Keys().Create(ctx, b))
		at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
		require.NoError(t, s.Usages().RecordBatch(ctx, []*usage.Record{
			usageAt(a.ID, "t1", at, 200, 0), usageAt(b.ID, "t2", at, 200, 0), usageAt(b.ID, "t2", at, 200, 0),
		}))
		aggs, err := s.Usages().Aggregate(ctx, &usage.QueryFilter{TenantID: "t1", Period: usage.PeriodDaily})
		require.NoError(t, err)
		require.Len(t, aggs, 1)
		assert.Equal(t, "t1", aggs[0].TenantID)
		assert.EqualValues(t, 1, aggs[0].RequestCount)
	})
}

func TestAggregateHourlyAndMonthlyAndPerKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k1, k2 := newKey("t1"), newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k1))
		require.NoError(t, s.Keys().Create(ctx, k2))
		at := time.Date(2026, 9, 28, 10, 15, 0, 0, time.UTC)
		require.NoError(t, s.Usages().RecordBatch(ctx, []*usage.Record{
			usageAt(k1.ID, "t1", at, 200, 0), usageAt(k2.ID, "t1", at.Add(10*time.Minute), 200, 0),
		}))
		hourly, err := s.Usages().Aggregate(ctx, &usage.QueryFilter{TenantID: "t1", Period: usage.PeriodHourly})
		require.NoError(t, err)
		require.Len(t, hourly, 1)
		assert.True(t, hourly[0].PeriodStart.Equal(time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)))
		assert.EqualValues(t, 2, hourly[0].RequestCount)

		monthly, err := s.Usages().Aggregate(ctx, &usage.QueryFilter{TenantID: "t1", Period: usage.PeriodMonthly})
		require.NoError(t, err)
		require.Len(t, monthly, 1)
		assert.True(t, monthly[0].PeriodStart.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)))

		perKey, err := s.Usages().Aggregate(ctx, &usage.QueryFilter{TenantID: "t1", KeyID: &k1.ID, Period: usage.PeriodDaily})
		require.NoError(t, err)
		require.Len(t, perKey, 1)
		assert.EqualValues(t, 1, perKey[0].RequestCount)
		assert.Equal(t, k1.ID.String(), perKey[0].KeyID.String())

		_, err = s.Usages().Aggregate(ctx, &usage.QueryFilter{TenantID: "t1", Period: "weekly"})
		assert.ErrorIs(t, err, usage.ErrInvalidPeriod)
	})
}

func TestUsageBeforeIsExclusive(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
		require.NoError(t, s.Usages().Record(ctx, usageAt(k.ID, "t1", at, 200, 0)))
		got, err := s.Usages().Query(ctx, &usage.QueryFilter{TenantID: "t1", Before: &at})
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestAggregateRangeIsAfterInclusiveBeforeExclusive(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		k := newKey("t1")
		require.NoError(t, s.Keys().Create(ctx, k))
		edge := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
		require.NoError(t, s.Usages().RecordBatch(ctx, []*usage.Record{
			usageAt(k.ID, "t1", edge.Add(-time.Millisecond), 200, 0),
			usageAt(k.ID, "t1", edge, 200, 0),
		}))

		from, err := s.Usages().Aggregate(ctx, &usage.QueryFilter{TenantID: "t1", Period: usage.PeriodDaily, After: &edge})
		require.NoError(t, err)
		require.Len(t, from, 1)
		assert.True(t, from[0].PeriodStart.Equal(edge))
		assert.EqualValues(t, 1, from[0].RequestCount)

		until, err := s.Usages().Aggregate(ctx, &usage.QueryFilter{TenantID: "t1", Period: usage.PeriodDaily, Before: &edge})
		require.NoError(t, err)
		require.Len(t, until, 1)
		assert.True(t, until[0].PeriodStart.Equal(edge.AddDate(0, 0, -1)))
		assert.EqualValues(t, 1, until[0].RequestCount)
	})
}

// Usage rows often share a created_at (a burst of requests, or mongo's
// millisecond precision), so paging by offset needs a total order or a row
// can come back twice or never.
func TestPagingUsageReturnsEveryRowOnce(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)
		// The SQL stores need the key rows the records reference.
		mine, theirs := newKey("t1"), newKey("t2")
		require.NoError(t, s.Keys().Create(ctx, mine))
		require.NoError(t, s.Keys().Create(ctx, theirs))
		want := map[string]bool{}
		for range 250 {
			r := usageAt(mine.ID, "t1", now, 200, 0)
			require.NoError(t, s.Usages().Record(ctx, r))
			want[r.ID.String()] = true
		}
		require.NoError(t, s.Usages().Record(ctx, usageAt(theirs.ID, "t2", now, 200, 0)))

		for range 5 {
			seen := map[string]int{}
			var order []string
			for offset := 0; ; offset += 33 {
				page, err := s.Usages().Query(ctx, &usage.QueryFilter{TenantID: "t1", Limit: 33, Offset: offset})
				require.NoError(t, err)
				for _, r := range page {
					seen[r.ID.String()]++
					order = append(order, r.ID.String())
				}
				if len(page) < 33 {
					break
				}
			}
			require.Len(t, seen, len(want))
			for uid, n := range seen {
				require.True(t, want[uid], "only t1's usage")
				require.Equal(t, 1, n, "usage row %s came back %d times", uid, n)
			}
			// Every row shares one created_at, so the id alone orders them.
			for i := 1; i < len(order); i++ {
				require.Greater(t, order[i-1], order[i], "row %d is not below row %d in id order", i, i-1)
			}
		}
	})
}
