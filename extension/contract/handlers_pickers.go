package contract

import (
	"context"
	"sort"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/scope"
)

// The pickers feed select boxes on the key forms, which want every row in one
// go, so their page defaults are larger than the key list's.
const (
	defaultPickerLimit = 100
	maxPickerLimit     = 200
)

type pickerRequest struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type policiesListResponse struct {
	Policies []PolicySummary `json:"policies"`
	HasMore  bool            `json:"hasMore"`
	// RateLimiterConfigured tells the create form whether a rate limit it
	// sets is enforced in this deployment or only stored.
	RateLimiterConfigured bool `json:"rateLimiterConfigured"`
}

type scopesListResponse struct {
	Scopes  []ScopeSummary `json:"scopes"`
	HasMore bool           `json:"hasMore"`
}

// clampPickerPage applies the picker paging defaults and caps.
func clampPickerPage(limit, offset int) (lim, off int) {
	if limit <= 0 {
		limit = defaultPickerLimit
	}
	if limit > maxPickerLimit {
		limit = maxPickerLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func policiesListHandler(deps Deps) func(context.Context, pickerRequest, dashcontract.Principal) (policiesListResponse, error) {
	return func(ctx context.Context, in pickerRequest, p dashcontract.Principal) (policiesListResponse, error) {
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return policiesListResponse{}, err
		}
		limit, offset := clampPickerPage(in.Limit, in.Offset)
		// One extra row tells us whether another page exists.
		rows, err := deps.Engine.ListPolicies(ctx, &policy.ListFilter{
			TenantID: tenant,
			Limit:    limit + 1,
			Offset:   offset,
		})
		if err != nil {
			return policiesListResponse{}, deps.mapError("policies.list", err)
		}
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		// Sorted here so every backend agrees on the order within a page.
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		out := make([]PolicySummary, 0, len(rows))
		for _, r := range rows {
			out = append(out, projectPolicySummary(r))
		}
		return policiesListResponse{
			Policies:              out,
			HasMore:               hasMore,
			RateLimiterConfigured: deps.Engine.RateLimiterConfigured(),
		}, nil
	}
}

func scopesListHandler(deps Deps) func(context.Context, pickerRequest, dashcontract.Principal) (scopesListResponse, error) {
	return func(ctx context.Context, in pickerRequest, p dashcontract.Principal) (scopesListResponse, error) {
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return scopesListResponse{}, err
		}
		limit, offset := clampPickerPage(in.Limit, in.Offset)
		rows, err := deps.Engine.ListScopes(ctx, &scope.ListFilter{
			TenantID: tenant,
			Limit:    limit + 1,
			Offset:   offset,
		})
		if err != nil {
			return scopesListResponse{}, deps.mapError("scopes.list", err)
		}
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		out := make([]ScopeSummary, 0, len(rows))
		for _, r := range rows {
			out = append(out, projectScopeSummary(r))
		}
		return scopesListResponse{Scopes: out, HasMore: hasMore}, nil
	}
}
