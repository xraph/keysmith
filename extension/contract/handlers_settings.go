package contract

import (
	"context"
	"slices"
	"time"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
)

const (
	// settingsHealthTimeout bounds the store ping, so a store that hangs
	// cannot hang the Settings page.
	settingsHealthTimeout = 2 * time.Second

	// defaultGraceSeconds is the grace RotateKey gives a rotation that names
	// none, on a key with no policy grace: the engine's fallback in
	// engine.go RotateKey, graceTTL := 24 * time.Hour. Change both together.
	defaultGraceSeconds int64 = 24 * 60 * 60

	storeAnswered    = "The store answered."
	storeDidNotReply = "The store did not answer. The error is in the server log."
)

type settingsRequest struct{}

// settingsResponse is what this deployment runs with: its hook plugins,
// whether the store answers, whether a rate limiter is configured, where
// the tenant came from, what of a policy is enforced, and the grace a
// rotation gets when nothing names one.
type settingsResponse struct {
	Plugins      []string `json:"plugins"` // sorted, [] when none
	StoreHealthy bool     `json:"storeHealthy"`
	// StoreMessage is one of two fixed sentences. The driver's error never
	// goes out; it is in the server log.
	StoreMessage          string           `json:"storeMessage"`
	RateLimiterConfigured bool             `json:"rateLimiterConfigured"`
	TenantSource          string           `json:"tenantSource"` // "claim" or "config"
	Tenant                string           `json:"tenant"`
	Enforcement           []EnforcementRow `json:"enforcement"`
	EnforcedFields        int              `json:"enforcedFields"`
	DefaultGraceSeconds   int64            `json:"defaultGraceSeconds"`
}

// settingsHandler answers the deployment's settings for the caller's
// tenant. A store that does not answer is reported, not refused: the page
// says so, and the error goes to the server log with the intent.
func settingsHandler(deps Deps) func(context.Context, settingsRequest, dashcontract.Principal) (settingsResponse, error) {
	return func(ctx context.Context, _ settingsRequest, p dashcontract.Principal) (settingsResponse, error) {
		tenant, err := tenantFrom(p, deps)
		if err != nil {
			return settingsResponse{}, err
		}
		limiter := deps.Engine.RateLimiterConfigured()
		out := settingsResponse{
			Plugins:               sortedPlugins(deps.Plugins),
			StoreHealthy:          true,
			StoreMessage:          storeAnswered,
			RateLimiterConfigured: limiter,
			TenantSource:          tenantSourceOf(p, deps),
			Tenant:                tenant,
			Enforcement:           enforcementTable(limiter),
			EnforcedFields:        enforcedPolicyFieldCount(limiter),
			DefaultGraceSeconds:   defaultGraceSeconds,
		}

		pingCtx, cancel := context.WithTimeout(ctx, settingsHealthTimeout)
		defer cancel()
		if err := deps.Engine.Health(pingCtx); err != nil {
			out.StoreHealthy, out.StoreMessage = false, storeDidNotReply
			if deps.Logger != nil {
				deps.Logger.Error("keysmith/contract: the store did not answer the health check",
					forge.F("intent", "settings"),
					forge.F("error", err),
				)
			}
		}
		return out, nil
	}
}

// sortedPlugins is a sorted copy of names, and [] for none. Deps.Plugins is
// shared by every request, so it is never sorted in place.
func sortedPlugins(names []string) []string {
	out := slices.Clone(names)
	if out == nil {
		out = []string{}
	}
	slices.Sort(out)
	return out
}
