package contract

import (
	"context"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
)

type policyIDRequest struct {
	ID string `json:"id"`
}

type policiesDetailResponse struct {
	Policy PolicyDetail `json:"policy"`
	// KeysUsing is every key in the tenant with this policy, revoked or not.
	KeysUsing int `json:"keysUsing"`
	// KeysBlockingDelete is the ones that are not revoked. While it is above
	// zero, policies.delete refuses.
	KeysBlockingDelete int `json:"keysBlockingDelete"`
	// RateLimiterConfigured is false when the engine has no rate limiter, so
	// the page can say a policy's rate limit is stored but not enforced.
	RateLimiterConfigured bool `json:"rateLimiterConfigured"`
}

// policyKeyCounts counts the tenant's keys that use the policy. A key blocks
// a delete under the same rule engine.DeletePolicy applies: it is not
// revoked by state and has no RevokedAt. ListByPolicy matches across
// tenants, so a key from another tenant naming this policy is skipped.
func policyKeyCounts(ctx context.Context, deps Deps, tenant string, polID id.PolicyID) (using, blocking int, err error) {
	keys, err := deps.Engine.Store().Keys().ListByPolicy(ctx, polID)
	if err != nil {
		return 0, 0, err
	}
	for _, k := range keys {
		if k.TenantID != tenant {
			continue
		}
		using++
		if k.State != key.StateRevoked && k.RevokedAt == nil {
			blocking++
		}
	}
	return using, blocking, nil
}

func policiesDetailHandler(deps Deps) func(context.Context, policyIDRequest, dashcontract.Principal) (policiesDetailResponse, error) {
	return func(ctx context.Context, in policyIDRequest, p dashcontract.Principal) (policiesDetailResponse, error) {
		const intent = "policies.detail"
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return policiesDetailResponse{}, err
		}
		pol, err := loadPolicyForTenant(ctx, deps, tenant, in.ID, intent)
		if err != nil {
			return policiesDetailResponse{}, err
		}
		using, blocking, err := policyKeyCounts(ctx, deps, tenant, pol.ID)
		if err != nil {
			return policiesDetailResponse{}, deps.mapError(intent, err)
		}
		return policiesDetailResponse{
			Policy:                projectPolicyDetail(pol),
			KeysUsing:             using,
			KeysBlockingDelete:    blocking,
			RateLimiterConfigured: deps.Engine.RateLimiterConfigured(),
		}, nil
	}
}
