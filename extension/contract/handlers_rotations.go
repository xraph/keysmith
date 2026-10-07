package contract

import (
	"context"
	"errors"
	"strings"
	"time"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
)

type rotationsListRequest struct {
	KeyID  string `json:"keyId"`
	Reason string `json:"reason"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type rotationsListResponse struct {
	Items   []RotationItem `json:"items"`
	HasMore bool           `json:"hasMore"`
}

// rotationsListHandler answers the tenant's rotations, newest first, the
// order every store's List already gives. keyId is a filter, not a lookup:
// another tenant's key or a missing one answers an empty list, because the
// tenant filter already excludes their rotations.
func rotationsListHandler(deps Deps) func(context.Context, rotationsListRequest, dashcontract.Principal) (rotationsListResponse, error) {
	return func(ctx context.Context, in rotationsListRequest, p dashcontract.Principal) (rotationsListResponse, error) {
		tenant, err := tenantFrom(ctx, p, deps)
		if err != nil {
			return rotationsListResponse{}, err
		}
		filter := &rotation.ListFilter{TenantID: tenant}
		if raw := strings.TrimSpace(in.KeyID); raw != "" {
			kid, kerr := id.ParseKeyID(raw)
			if kerr != nil {
				return rotationsListResponse{}, badRequest("keyId is not a key id")
			}
			filter.KeyID = &kid
		}
		switch rotation.Reason(in.Reason) {
		case "", rotation.ReasonManual, rotation.ReasonCompromise, rotation.ReasonPolicy, rotation.ReasonScheduled:
			filter.Reason = rotation.Reason(in.Reason)
		default:
			return rotationsListResponse{}, badRequest("reason must be one of manual, compromise, policy, scheduled")
		}
		limit, offset := clampPage(in.Limit, in.Offset)
		// One extra row tells us whether another page exists.
		filter.Limit, filter.Offset = limit+1, offset

		recs, err := deps.Engine.ListRotations(ctx, filter)
		if err != nil {
			return rotationsListResponse{}, deps.mapError("rotations.list", err)
		}
		hasMore := len(recs) > limit
		if hasMore {
			recs = recs[:limit]
		}

		keys, err := rotationKeys(ctx, deps, tenant, recs, "rotations.list")
		if err != nil {
			return rotationsListResponse{}, err
		}
		now := time.Now()
		out := make([]RotationItem, 0, len(recs))
		for _, r := range recs {
			out = append(out, projectRotationItem(r, keys[r.KeyID.String()], now))
		}
		return rotationsListResponse{Items: out, HasMore: hasMore}, nil
	}
}

// rotationKeys loads each distinct key the records name, once, and keeps it
// only when it belongs to tenant. A key that is gone, or that belongs to
// another tenant, maps to nil, which projects as a null name. At most one
// read per distinct key. intent labels a failure in the server log.
func rotationKeys(ctx context.Context, deps Deps, tenant string, recs []*rotation.Record, intent string) (map[string]*key.Key, error) {
	keys := make(map[string]*key.Key, len(recs))
	for _, r := range recs {
		kid := r.KeyID.String()
		if _, done := keys[kid]; done {
			continue
		}
		k, err := deps.Engine.GetKey(ctx, r.KeyID)
		switch {
		case errors.Is(err, keysmith.ErrKeyNotFound):
			k = nil
		case err != nil:
			return nil, deps.mapError(intent, err)
		case k.TenantID != tenant:
			k = nil
		}
		keys[kid] = k
	}
	return keys, nil
}
