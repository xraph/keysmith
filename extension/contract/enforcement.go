package contract

// EnforcementRow says whether this deployment enforces one policy field, and
// when. Field is the wire name the policy commands take. Label is the
// editor's label for it. Group is the editor's group: "keysmith" (enforced
// by Keysmith), "rateLimiter" (enforced only with a rate limiter) or
// "application" (stored for the host application, never checked here).
// When is set exactly when Enforced is true.
type EnforcementRow struct {
	Field    string `json:"field"`
	Label    string `json:"label"`
	Group    string `json:"group"`
	Enforced bool   `json:"enforced"`
	When     string `json:"when"`
}

const (
	groupKeysmith    = "keysmith"
	groupRateLimiter = "rateLimiter"
	groupApplication = "application"
)

// policyFieldCount is how many policy fields the write path takes, name and
// description aside.
const policyFieldCount = 13

// enforcementRows is the table in the order the React policy editor lays the
// fields out, groups top to bottom. Each When names the engine path that
// checks the field:
//
//   - maxKeyLifetimeSeconds: CreateKey refuses an expiry past the cap and
//     defaults a missing one to it.
//   - graceSeconds: RotateKey uses it when the rotation names no grace.
//   - allowedScopes: checkScopes, from CreateKey and AssignScopes.
//   - rateLimit, rateLimitWindowSeconds: ValidateKey, only when a
//     RateLimiter is injected and rateLimit is above 0.
//
// Nothing in the engine reads the application group. The host reads those
// from ValidationResult.Policy. Change this table when the engine starts or
// stops checking a field.
var enforcementRows = [policyFieldCount]EnforcementRow{
	{Field: "maxKeyLifetimeSeconds", Label: "Max key lifetime", Group: groupKeysmith, When: "when a key is created"},
	{Field: "graceSeconds", Label: "Grace on rotation", Group: groupKeysmith, When: "when a key is rotated"},
	{Field: "allowedScopes", Label: "Allowed scopes", Group: groupKeysmith, When: "when a key is created or its scopes are assigned"},
	{Field: "rateLimit", Label: "Rate limit", Group: groupRateLimiter, When: "when a key is validated"},
	{Field: "rateLimitWindowSeconds", Label: "Window", Group: groupRateLimiter, When: "when a key is validated"},
	{Field: "burstLimit", Label: "Burst limit", Group: groupApplication},
	{Field: "rotationPeriodSeconds", Label: "Rotation period", Group: groupApplication},
	{Field: "dailyQuota", Label: "Daily quota", Group: groupApplication},
	{Field: "monthlyQuota", Label: "Monthly quota", Group: groupApplication},
	{Field: "allowedIps", Label: "Allowed IPs", Group: groupApplication},
	{Field: "allowedOrigins", Label: "Allowed origins", Group: groupApplication},
	{Field: "allowedPaths", Label: "Allowed paths", Group: groupApplication},
	{Field: "allowedMethods", Label: "Allowed methods", Group: groupApplication},
}

// enforcementTable is a fresh copy of the table for a deployment that does
// or does not have a rate limiter. The keysmith group is always enforced,
// the application group never is, and the rate limiter group only with a
// limiter. Without one, those rows carry no When, since nothing checks them.
func enforcementTable(rateLimiter bool) []EnforcementRow {
	out := make([]EnforcementRow, 0, len(enforcementRows))
	for _, r := range &enforcementRows {
		switch r.Group {
		case groupKeysmith:
			r.Enforced = true
		case groupRateLimiter:
			r.Enforced = rateLimiter
		default:
			r.Enforced = false
		}
		if !r.Enforced {
			r.When = ""
		}
		out = append(out, r)
	}
	return out
}

// enforcedPolicyFieldCount is how many of the policyFieldCount fields this
// deployment enforces: 3, or 5 with a rate limiter.
func enforcedPolicyFieldCount(rateLimiter bool) int {
	n := 0
	for _, r := range enforcementTable(rateLimiter) {
		if r.Enforced {
			n++
		}
	}
	return n
}
