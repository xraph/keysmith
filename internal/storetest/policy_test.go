package storetest_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/store"
)

func fullPolicy(tenant string) *policy.Policy {
	now := time.Now()
	return &policy.Policy{
		ID: id.NewPolicyID(), TenantID: tenant, AppID: "app", Name: "full-" + id.NewPolicyID().String(),
		Description: "every field set", RateLimit: 120, RateLimitWindow: 90 * time.Second, BurstLimit: 7,
		AllowedScopes: []string{"read", "write"}, AllowedIPs: []string{"10.0.0.0/8"},
		AllowedOrigins: []string{"https://example.com"}, AllowedMethods: []string{"GET", "POST"},
		AllowedPaths: []string{"/v1/*"}, MaxKeyLifetime: 30 * 24 * time.Hour, RotationPeriod: 7 * 24 * time.Hour,
		GracePeriod: 2 * time.Hour, DailyQuota: 1000, MonthlyQuota: 20000,
		Metadata: map[string]any{"team": "billing"}, CreatedAt: now, UpdatedAt: now,
	}
}

func assertPolicyEqual(t *testing.T, want, got *policy.Policy) {
	t.Helper()
	assert.Equal(t, want.Name, got.Name)
	assert.Equal(t, want.Description, got.Description)
	assert.Equal(t, want.RateLimit, got.RateLimit)
	assert.Equal(t, want.RateLimitWindow, got.RateLimitWindow)
	assert.Equal(t, want.BurstLimit, got.BurstLimit)
	assert.Equal(t, want.AllowedScopes, got.AllowedScopes)
	assert.Equal(t, want.AllowedIPs, got.AllowedIPs)
	assert.Equal(t, want.AllowedOrigins, got.AllowedOrigins)
	assert.Equal(t, want.AllowedMethods, got.AllowedMethods)
	assert.Equal(t, want.AllowedPaths, got.AllowedPaths)
	assert.Equal(t, want.MaxKeyLifetime, got.MaxKeyLifetime)
	assert.Equal(t, want.RotationPeriod, got.RotationPeriod)
	assert.Equal(t, want.GracePeriod, got.GracePeriod)
	assert.Equal(t, want.DailyQuota, got.DailyQuota)
	assert.Equal(t, want.MonthlyQuota, got.MonthlyQuota)
	assert.Equal(t, "billing", got.Metadata["team"])
}

func TestPolicyEveryFieldRoundTrips(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		p := fullPolicy("t1")
		require.NoError(t, s.Policies().Create(ctx, p))
		got, err := s.Policies().Get(ctx, p.ID)
		require.NoError(t, err)
		assertPolicyEqual(t, p, got)

		p.RateLimitWindow, p.MaxKeyLifetime, p.RotationPeriod, p.GracePeriod = time.Minute, time.Hour, 48*time.Hour, 10*time.Minute
		p.AllowedScopes, p.AllowedPaths = []string{"admin"}, []string{"/v2/*"}
		p.DailyQuota, p.MonthlyQuota, p.RateLimit, p.BurstLimit = 5, 50, 3, 1
		p.Description = "changed"
		require.NoError(t, s.Policies().Update(ctx, p))
		got, err = s.Policies().Get(ctx, p.ID)
		require.NoError(t, err)
		assertPolicyEqual(t, p, got)

		byName, err := s.Policies().GetByName(ctx, "t1", p.Name)
		require.NoError(t, err)
		assertPolicyEqual(t, p, byName)
		list, err := s.Policies().List(ctx, &policy.ListFilter{TenantID: "t1"})
		require.NoError(t, err)
		require.Len(t, list, 1)
		assertPolicyEqual(t, p, list[0])
	})
}

// Paging a tenant's policies by offset returns every row exactly once, even
// when they all share one created_at, so the order has to be total.
func TestPagingPoliciesReturnsEveryRowOnce(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)
		want := map[string]bool{}
		for i := range 250 {
			p := &policy.Policy{
				ID: id.NewPolicyID(), TenantID: "t1", AppID: "app",
				Name: "p" + strconv.Itoa(i), CreatedAt: now, UpdatedAt: now,
			}
			require.NoError(t, s.Policies().Create(ctx, p))
			want[p.ID.String()] = true
		}
		require.NoError(t, s.Policies().Create(ctx, &policy.Policy{
			ID: id.NewPolicyID(), TenantID: "t2", AppID: "app", Name: "theirs", CreatedAt: now, UpdatedAt: now,
		}))

		for range 5 {
			seen := map[string]int{}
			for offset := 0; ; offset += 33 {
				page, err := s.Policies().List(ctx, &policy.ListFilter{TenantID: "t1", Limit: 33, Offset: offset})
				require.NoError(t, err)
				for _, p := range page {
					seen[p.ID.String()]++
				}
				if len(page) < 33 {
					break
				}
			}
			require.Len(t, seen, len(want))
			for pid, n := range seen {
				require.True(t, want[pid], "only t1's policies")
				require.Equal(t, 1, n, "policy %s came back %d times", pid, n)
			}
		}
	})
}
