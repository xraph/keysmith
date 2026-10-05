package contract

import (
	"context"
	"time"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/usage"
)

// overviewRecent is how many keys and rotations the overview lists.
const overviewRecent = 5

type overviewRequest struct{}

type overviewResponse struct {
	Counts              overviewCounts `json:"counts"`
	OpenGraceWindows    int            `json:"openGraceWindows"`
	ExpiringWithin7Days int            `json:"expiringWithin7Days"`
	// RequestsLast24h is null when the tenant has never recorded usage, so
	// the page can say "not recorded" rather than show 0.
	RequestsLast24h *int64         `json:"requestsLast24h"`
	RecentKeys      []KeySummary   `json:"recentKeys"`      // newest first, at most 5
	RecentRotations []RotationItem `json:"recentRotations"` // newest first, at most 5
	EnforcedFields  int            `json:"enforcedFields"`  // of PolicyFields, on this deployment
	PolicyFields    int            `json:"policyFields"`
}

// overviewCounts are the tenant's keys by stored state. An active key past
// its expiry that nothing has marked yet is still stored active, so it
// counts as active here.
type overviewCounts struct {
	Active    int64 `json:"active"`
	Suspended int64 `json:"suspended"`
	Revoked   int64 `json:"revoked"`
	Expired   int64 `json:"expired"`
}

// overviewHandler answers the tenant's summary: keys by state, open grace
// windows, keys expiring within 7 days, requests in the last 24 hours, the
// newest keys and rotations, and how many policy fields this deployment
// enforces.
func overviewHandler(deps Deps) func(context.Context, overviewRequest, dashcontract.Principal) (overviewResponse, error) {
	return func(ctx context.Context, _ overviewRequest, p dashcontract.Principal) (overviewResponse, error) {
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return overviewResponse{}, err
		}
		now := time.Now()
		out := overviewResponse{
			EnforcedFields: enforcedPolicyFieldCount(deps.Engine.RateLimiterConfigured()),
			PolicyFields:   policyFieldCount,
		}

		if out.Counts, err = overviewKeyCounts(ctx, deps, tenant); err != nil {
			return overviewResponse{}, err
		}
		if out.ExpiringWithin7Days, err = overviewExpiring(ctx, deps, tenant, now); err != nil {
			return overviewResponse{}, err
		}
		if out.OpenGraceWindows, err = overviewOpenWindows(ctx, deps, tenant, now); err != nil {
			return overviewResponse{}, err
		}
		if out.RequestsLast24h, err = overviewRequests(ctx, deps, tenant, now); err != nil {
			return overviewResponse{}, err
		}
		if out.RecentKeys, err = overviewKeys(ctx, deps, tenant, now); err != nil {
			return overviewResponse{}, err
		}
		if out.RecentRotations, err = overviewRotations(ctx, deps, tenant, now); err != nil {
			return overviewResponse{}, err
		}
		return out, nil
	}
}

func overviewKeyCounts(ctx context.Context, deps Deps, tenant string) (overviewCounts, error) {
	var c overviewCounts
	for _, row := range []struct {
		state key.State
		into  *int64
	}{
		{key.StateActive, &c.Active},
		{key.StateSuspended, &c.Suspended},
		{key.StateRevoked, &c.Revoked},
		{key.StateExpired, &c.Expired},
	} {
		n, err := deps.Engine.Store().Keys().Count(ctx, &key.ListFilter{TenantID: tenant, State: row.state})
		if err != nil {
			return overviewCounts{}, deps.mapError("overview", err)
		}
		*row.into = n
	}
	return c, nil
}

// overviewExpiring counts the tenant's keys that keys.list flags as
// expiring soon: active, expiry ahead of now and within the window.
//
// ListExpired has no tenant filter, so this reads every tenant's keys that
// expire before now plus the window and keeps this tenant's. The list is not
// paged, so the count is complete. It is the one post-filter in the
// contract and is safe for that reason alone. A key already past its expiry
// is expired, not expiring, even before anything marks it.
func overviewExpiring(ctx context.Context, deps Deps, tenant string, now time.Time) (int, error) {
	keys, err := deps.Engine.Store().Keys().ListExpired(ctx, now.Add(expiresSoonWindow))
	if err != nil {
		return 0, deps.mapError("overview", err)
	}
	n := 0
	for _, k := range keys {
		// effectiveState reads a key past its expiry as expired, and a key
		// with RevokedAt set as revoked whatever its stored state, so only
		// keys whose expiry is still ahead are left.
		if state, _ := effectiveState(k, now); k.TenantID == tenant && state == string(key.StateActive) {
			n++
		}
	}
	return n, nil
}

// overviewOpenWindows counts the tenant's rotations that rotations.list
// shows as open, by the same rule (projectRotationItem's WindowOpen), so the
// two pages always agree.
//
// ListPendingGrace has no tenant filter either and is not paged. A record
// without an old hint is never open, so its key is not loaded.
func overviewOpenWindows(ctx context.Context, deps Deps, tenant string, now time.Time) (int, error) {
	recs, err := deps.Engine.Store().Rotations().ListPendingGrace(ctx, now)
	if err != nil {
		return 0, deps.mapError("overview", err)
	}
	mine := make([]*rotation.Record, 0, len(recs))
	for _, r := range recs {
		if r.TenantID == tenant && r.OldHint != "" {
			mine = append(mine, r)
		}
	}
	keys, err := rotationKeys(ctx, deps, tenant, mine, "overview")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range mine {
		if projectRotationItem(r, keys[r.KeyID.String()], now).WindowOpen {
			n++
		}
	}
	return n, nil
}

// overviewRequests is nil when the tenant has no usage row at all, and the
// tenant's request count over the last 24 hours otherwise. Whether anything
// was recorded is a one-row read, so the count runs only when it matters.
func overviewRequests(ctx context.Context, deps Deps, tenant string, now time.Time) (*int64, error) {
	recorded, err := usageRecorded(ctx, deps, tenant)
	if err != nil {
		return nil, deps.mapError("overview", err)
	}
	if !recorded {
		return nil, nil
	}
	after := now.Add(-24 * time.Hour)
	n, err := deps.Engine.Store().Usages().Count(ctx, &usage.QueryFilter{TenantID: tenant, After: &after})
	if err != nil {
		return nil, deps.mapError("overview", err)
	}
	return &n, nil
}

// overviewKeys is the tenant's newest keys, projected as keys.list projects
// them, scopes included.
func overviewKeys(ctx context.Context, deps Deps, tenant string, now time.Time) ([]KeySummary, error) {
	keys, err := deps.Engine.ListKeys(ctx, &key.ListFilter{TenantID: tenant, Limit: overviewRecent})
	if err != nil {
		return nil, deps.mapError("overview", err)
	}
	out := make([]KeySummary, 0, len(keys))
	for _, k := range keys {
		names, err := scopeNames(ctx, deps.Engine, k.ID)
		if err != nil {
			return nil, deps.mapError("overview", err)
		}
		out = append(out, projectKey(k, names, now))
	}
	return out, nil
}

// overviewRotations is the tenant's newest rotations, projected as
// rotations.list projects them.
func overviewRotations(ctx context.Context, deps Deps, tenant string, now time.Time) ([]RotationItem, error) {
	recs, err := deps.Engine.ListRotations(ctx, &rotation.ListFilter{TenantID: tenant, Limit: overviewRecent})
	if err != nil {
		return nil, deps.mapError("overview", err)
	}
	keys, err := rotationKeys(ctx, deps, tenant, recs, "overview")
	if err != nil {
		return nil, err
	}
	out := make([]RotationItem, 0, len(recs))
	for _, r := range recs {
		out = append(out, projectRotationItem(r, keys[r.KeyID.String()], now))
	}
	return out, nil
}
