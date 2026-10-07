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

// overviewCounts are the tenant's keys by effective state, as the badges
// show it. An active key past its expiry that nothing has marked yet counts
// as expired, and a key with RevokedAt set counts as revoked whatever its
// stored state says. effectiveState holds the rule.
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
		tenant, err := tenantFrom(ctx, p, deps)
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
		soon, err := overviewSoon(ctx, deps, tenant, now)
		if err != nil {
			return overviewResponse{}, err
		}
		out.ExpiringWithin7Days = soon.expiring
		out.Counts.Active -= soon.pendingExpired + soon.staleRevoked
		out.Counts.Expired += soon.pendingExpired
		out.Counts.Revoked += soon.staleRevoked
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

// overviewSoonCounts are the tenant's stored-active keys that read as
// something else, or soon will.
type overviewSoonCounts struct {
	expiring       int   // effective active, expiry ahead and within the window
	pendingExpired int64 // effective expired: expiry passed, nothing has marked it
	staleRevoked   int64 // RevokedAt set, though the stored state says active
}

// overviewSoon reads the tenant's stored-active keys that expire before now
// plus the window, once, and sorts them by effective state. The expiring
// count is the keys keys.list flags as expiring soon. The other two move
// keys out of the stored active count into the bucket their badge shows.
//
// Every key whose expiry has passed expires before now plus the window, so
// this read finds every pending expiry. A stored-active key with RevokedAt
// set and no expiry inside the window is the one case it misses. Only a race
// leaves a key that way (a stale write putting the state back to active
// after a revoke), and that key still counts as active here.
//
// ListExpired has no tenant filter, so this reads every tenant's keys and
// keeps this tenant's. The list is not paged, so the counts are complete.
// It is the one post-filter in the contract and is safe for that reason
// alone.
func overviewSoon(ctx context.Context, deps Deps, tenant string, now time.Time) (overviewSoonCounts, error) {
	keys, err := deps.Engine.Store().Keys().ListExpired(ctx, now.Add(expiresSoonWindow))
	if err != nil {
		return overviewSoonCounts{}, deps.mapError("overview", err)
	}
	var c overviewSoonCounts
	for _, k := range keys {
		if k.TenantID != tenant {
			continue
		}
		// effectiveState reads a key with RevokedAt set as revoked before
		// it looks at the expiry, so a raced key is never counted twice.
		switch state, _ := effectiveState(k, now); state {
		case string(key.StateActive):
			c.expiring++
		case string(key.StateExpired):
			c.pendingExpired++
		case string(key.StateRevoked):
			c.staleRevoked++
		}
	}
	return c, nil
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
