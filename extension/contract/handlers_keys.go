package contract

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
)

const (
	defaultListLimit = 25
	maxListLimit     = 100

	// maxRotationsRead bounds how many rotation records the detail page reads
	// when looking for open windows.
	maxRotationsRead = 100
)

type keysListRequest struct {
	Environment string `json:"environment"`
	State       string `json:"state"`
	PolicyID    string `json:"policyId"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
}

type keysListResponse struct {
	Keys  []KeySummary `json:"keys"`
	Total int64        `json:"total"`
}

type keysDetailRequest struct {
	ID string `json:"id"`
}

type keysDetailResponse struct {
	Key          KeySummary     `json:"key"`
	Policy       *PolicyRef     `json:"policy"` // explicit null when none
	Metadata     map[string]any `json:"metadata"`
	PreviousKeys []PreviousKey  `json:"previousKeys"`
}

func badRequest(message string) error {
	return &dashcontract.Error{Code: dashcontract.CodeBadRequest, Message: message}
}

// keyNotFound is the one error the detail handler returns for a key that does
// not exist and for a key that belongs to another tenant, so the two cannot
// be told apart.
func keyNotFound() error {
	return &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "key not found"}
}

// clampPage applies the list paging defaults and caps.
func clampPage(limit, offset int) (lim, off int) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// scopeNames reads a key's scope names, sorted and without duplicates. Key
// reads never populate key.Key.Scopes on any backend, so the scope store is
// the only source. The memory store's ListByKey matches by name across
// tenants in map order, so it can return one name twice in any order.
func scopeNames(ctx context.Context, eng *keysmith.Engine, keyID id.KeyID) ([]string, error) {
	scopes, err := eng.Store().Scopes().ListByKey(ctx, keyID)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(scopes))
	for _, s := range scopes {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return slices.Compact(names), nil
}

func keysListHandler(deps Deps) func(context.Context, keysListRequest, dashcontract.Principal) (keysListResponse, error) {
	return func(ctx context.Context, in keysListRequest, p dashcontract.Principal) (keysListResponse, error) {
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return keysListResponse{}, err
		}
		switch in.Environment {
		case "", string(key.EnvLive), string(key.EnvTest), string(key.EnvStaging):
		default:
			return keysListResponse{}, badRequest("environment must be one of live, test, staging")
		}
		// The engine never assigns "rotated", so it is not a filter.
		switch in.State {
		case "", string(key.StateActive), string(key.StateSuspended), string(key.StateRevoked), string(key.StateExpired):
		default:
			return keysListResponse{}, badRequest("state must be one of active, suspended, revoked, expired")
		}
		limit, offset := clampPage(in.Limit, in.Offset)
		filter := &key.ListFilter{
			TenantID:    tenant,
			Environment: key.Environment(in.Environment),
			State:       key.State(in.State),
			Limit:       limit,
			Offset:      offset,
		}
		if in.PolicyID != "" {
			pid, perr := id.ParsePolicyID(in.PolicyID)
			if perr != nil {
				return keysListResponse{}, badRequest("policyId is not a policy id")
			}
			filter.PolicyID = &pid
		}

		keys, err := deps.Engine.ListKeys(ctx, filter)
		if err != nil {
			return keysListResponse{}, deps.mapError("keys.list", err)
		}
		// Count ignores Limit and Offset, so Total is the size of the filter.
		total, err := deps.Engine.Store().Keys().Count(ctx, filter)
		if err != nil {
			return keysListResponse{}, deps.mapError("keys.list", err)
		}

		now := time.Now()
		out := make([]KeySummary, 0, len(keys))
		for _, k := range keys {
			// One scope read per row, bounded by the page cap (maxListLimit).
			names, err := scopeNames(ctx, deps.Engine, k.ID)
			if err != nil {
				return keysListResponse{}, deps.mapError("keys.list", err)
			}
			out = append(out, projectKey(k, names, now))
		}
		return keysListResponse{Keys: out, Total: total}, nil
	}
}

func keysDetailHandler(deps Deps) func(context.Context, keysDetailRequest, dashcontract.Principal) (keysDetailResponse, error) {
	return func(ctx context.Context, in keysDetailRequest, p dashcontract.Principal) (keysDetailResponse, error) {
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return keysDetailResponse{}, err
		}
		if in.ID == "" {
			return keysDetailResponse{}, badRequest("id is required")
		}
		kid, err := id.ParseKeyID(in.ID)
		if err != nil {
			return keysDetailResponse{}, badRequest("id is not a key id")
		}
		k, err := deps.Engine.GetKey(ctx, kid)
		if err != nil {
			if errors.Is(err, keysmith.ErrKeyNotFound) {
				return keysDetailResponse{}, keyNotFound()
			}
			return keysDetailResponse{}, deps.mapError("keys.detail", err)
		}
		if k.TenantID != tenant {
			return keysDetailResponse{}, keyNotFound()
		}

		names, err := scopeNames(ctx, deps.Engine, k.ID)
		if err != nil {
			return keysDetailResponse{}, deps.mapError("keys.detail", err)
		}
		now := time.Now()
		out := keysDetailResponse{
			Key:          projectKey(k, names, now),
			Metadata:     k.Metadata,
			PreviousKeys: []PreviousKey{},
		}
		if out.Metadata == nil {
			out.Metadata = map[string]any{}
		}

		if k.PolicyID != nil {
			pol, perr := deps.Engine.GetPolicy(ctx, *k.PolicyID)
			switch {
			case errors.Is(perr, keysmith.ErrPolicyNotFound):
				// A dangling policy reads as none.
			case perr != nil:
				return keysDetailResponse{}, deps.mapError("keys.detail", perr)
			case pol.TenantID == k.TenantID:
				ref := projectPolicyRef(pol)
				out.Policy = &ref
			}
			// A policy from another tenant is never shown.
		}

		recs, err := deps.Engine.ListRotations(ctx, &rotation.ListFilter{KeyID: &k.ID, Limit: maxRotationsRead})
		if err != nil {
			return keysDetailResponse{}, deps.mapError("keys.detail", err)
		}
		open := make([]*rotation.Record, 0, len(recs))
		for _, r := range recs {
			// Records written before the grace fix have no old hint and must
			// never read as an open window.
			if r.OldHint != "" && r.GraceEnds.After(now) {
				open = append(open, r)
			}
		}
		sort.Slice(open, func(i, j int) bool { return open[i].GraceEnds.Before(open[j].GraceEnds) })
		for _, r := range open {
			out.PreviousKeys = append(out.PreviousKeys, PreviousKey{
				RotationID: r.ID.String(),
				Hint:       r.OldHint,
				Reason:     string(r.Reason),
				RotatedAt:  rfc3339(r.CreatedAt),
				GraceEnds:  rfc3339(r.GraceEnds),
			})
		}
		return out, nil
	}
}
