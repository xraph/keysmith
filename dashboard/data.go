package dashboard

import (
	"context"
	"fmt"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/usage"
)

// KeyStats holds aggregated key counts by state.
type KeyStats struct {
	Total     int64
	Active    int64
	Revoked   int64
	Suspended int64
	Expired   int64
}

// fetchKeyStats returns aggregated key counts for the overview.
func fetchKeyStats(ctx context.Context, engine *keysmith.Engine) KeyStats {
	var stats KeyStats
	total, err := engine.Store().Keys().Count(ctx, &key.ListFilter{})
	if err == nil {
		stats.Total = total
	}
	active, err := engine.Store().Keys().Count(ctx, &key.ListFilter{State: key.StateActive})
	if err == nil {
		stats.Active = active
	}
	revoked, err := engine.Store().Keys().Count(ctx, &key.ListFilter{State: key.StateRevoked})
	if err == nil {
		stats.Revoked = revoked
	}
	suspended, err := engine.Store().Keys().Count(ctx, &key.ListFilter{State: key.StateSuspended})
	if err == nil {
		stats.Suspended = suspended
	}
	expired, err := engine.Store().Keys().Count(ctx, &key.ListFilter{State: key.StateExpired})
	if err == nil {
		stats.Expired = expired
	}
	return stats
}

// fetchRecentKeys returns the most recently created keys.
func fetchRecentKeys(ctx context.Context, engine *keysmith.Engine, limit int) ([]*key.Key, error) {
	if limit <= 0 {
		limit = 10
	}
	keys, err := engine.ListKeys(ctx, &key.ListFilter{Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("dashboard: fetch recent keys: %w", err)
	}
	return keys, nil
}

// fetchKeys returns keys matching filters.
func fetchKeys(ctx context.Context, engine *keysmith.Engine, filter *key.ListFilter) ([]*key.Key, int64, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	keys, err := engine.ListKeys(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("dashboard: fetch keys: %w", err)
	}
	count, _ := engine.Store().Keys().Count(ctx, filter)
	return keys, count, nil
}

// fetchPolicies returns all policies.
func fetchPolicies(ctx context.Context, engine *keysmith.Engine) ([]*policy.Policy, int64, error) {
	policies, err := engine.ListPolicies(ctx, &policy.ListFilter{Limit: 100})
	if err != nil {
		return nil, 0, fmt.Errorf("dashboard: fetch policies: %w", err)
	}
	count, _ := engine.Store().Policies().Count(ctx, &policy.ListFilter{})
	return policies, count, nil
}

// fetchScopes returns all scopes.
func fetchScopes(ctx context.Context, engine *keysmith.Engine) ([]*scope.Scope, error) {
	scopes, err := engine.ListScopes(ctx, &scope.ListFilter{Limit: 200})
	if err != nil {
		return nil, fmt.Errorf("dashboard: fetch scopes: %w", err)
	}
	return scopes, nil
}

// fetchKeyScopes returns scopes assigned to a key.
func fetchKeyScopes(ctx context.Context, engine *keysmith.Engine, keyID id.KeyID) ([]*scope.Scope, error) {
	scopes, err := engine.Store().Scopes().ListByKey(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: fetch key scopes: %w", err)
	}
	return scopes, nil
}

// fetchKeyUsage returns usage records for a key.
func fetchKeyUsage(ctx context.Context, engine *keysmith.Engine, keyID id.KeyID, limit int) ([]*usage.Record, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	filter := &usage.QueryFilter{KeyID: &keyID, Limit: limit}
	records, err := engine.QueryUsage(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("dashboard: fetch key usage: %w", err)
	}
	count, _ := engine.Store().Usages().Count(ctx, filter)
	return records, count, nil
}

// fetchKeyAggregates returns aggregated usage for a key.
func fetchKeyAggregates(ctx context.Context, engine *keysmith.Engine, keyID id.KeyID) ([]*usage.Aggregation, error) {
	filter := &usage.QueryFilter{KeyID: &keyID, Period: "daily", Limit: 30}
	aggs, err := engine.AggregateUsage(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("dashboard: fetch key aggregates: %w", err)
	}
	return aggs, nil
}

// fetchRecentUsage returns recent usage records (tenant-wide).
func fetchRecentUsage(ctx context.Context, engine *keysmith.Engine, limit int) ([]*usage.Record, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	filter := &usage.QueryFilter{Limit: limit}
	records, err := engine.QueryUsage(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("dashboard: fetch recent usage: %w", err)
	}
	count, _ := engine.Store().Usages().Count(ctx, filter)
	return records, count, nil
}

// fetchUsageAggregates returns aggregated usage (tenant-wide).
func fetchUsageAggregates(ctx context.Context, engine *keysmith.Engine) ([]*usage.Aggregation, error) {
	aggs, err := engine.AggregateUsage(ctx, &usage.QueryFilter{Period: "daily", Limit: 30})
	if err != nil {
		return nil, fmt.Errorf("dashboard: fetch usage aggregates: %w", err)
	}
	return aggs, nil
}

// fetchRotations returns rotation records.
func fetchRotations(ctx context.Context, engine *keysmith.Engine, filter *rotation.ListFilter) ([]*rotation.Record, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	records, err := engine.ListRotations(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("dashboard: fetch rotations: %w", err)
	}
	return records, nil
}

// fetchKeyRotations returns rotation records for a specific key.
func fetchKeyRotations(ctx context.Context, engine *keysmith.Engine, keyID id.KeyID) ([]*rotation.Record, error) {
	records, err := engine.ListRotations(ctx, &rotation.ListFilter{KeyID: &keyID, Limit: 50})
	if err != nil {
		return nil, fmt.Errorf("dashboard: fetch key rotations: %w", err)
	}
	return records, nil
}

// fetchPolicyCount returns the total number of policies.
func fetchPolicyCount(ctx context.Context, engine *keysmith.Engine) int64 {
	count, _ := engine.Store().Policies().Count(ctx, &policy.ListFilter{})
	return count
}

// fetchScopeCount returns the total number of scopes.
func fetchScopeCount(ctx context.Context, engine *keysmith.Engine) int {
	scopes, err := engine.ListScopes(ctx, &scope.ListFilter{Limit: 1000})
	if err != nil {
		return 0
	}
	return len(scopes)
}
