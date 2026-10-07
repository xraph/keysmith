package contract

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/policy"
)

const (
	maxPolicyNameLength        = 200
	maxPolicyDescriptionLength = 1000
	maxPolicyListEntries       = 100

	// maxPolicyLifetimeSeconds caps maxKeyLifetimeSeconds and
	// rotationPeriodSeconds: 10 years of 365 days.
	maxPolicyLifetimeSeconds = 10 * 365 * 24 * 3600
	// maxPolicyGraceSeconds is 90 days.
	maxPolicyGraceSeconds = 90 * 24 * 3600
	// maxPolicyWindowSeconds is 31 days.
	maxPolicyWindowSeconds = 31 * 24 * 3600
	// maxPolicyRate caps rateLimit and burstLimit.
	maxPolicyRate = 1_000_000_000
)

// policyMethods are the methods allowedMethods accepts, upper case.
var policyMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

// policyFields: every field optional. On create a nil field is unset; on
// update a nil field is left alone, and 0 or an empty list clears it. JSON
// null is the same as leaving the field out.
type policyFields struct {
	Name                   *string   `json:"name"`
	Description            *string   `json:"description"`
	MaxKeyLifetimeSeconds  *int64    `json:"maxKeyLifetimeSeconds"`
	GraceSeconds           *int64    `json:"graceSeconds"`
	AllowedScopes          *[]string `json:"allowedScopes"`
	RateLimit              *int64    `json:"rateLimit"`
	RateLimitWindowSeconds *int64    `json:"rateLimitWindowSeconds"`
	BurstLimit             *int64    `json:"burstLimit"`
	AllowedIPs             *[]string `json:"allowedIps"`
	AllowedOrigins         *[]string `json:"allowedOrigins"`
	AllowedMethods         *[]string `json:"allowedMethods"`
	AllowedPaths           *[]string `json:"allowedPaths"`
	RotationPeriodSeconds  *int64    `json:"rotationPeriodSeconds"`
	DailyQuota             *int64    `json:"dailyQuota"`
	MonthlyQuota           *int64    `json:"monthlyQuota"`
}

type policiesUpdateRequest struct {
	ID string `json:"id"`
	policyFields
}

type policyResponse struct {
	Policy PolicyDetail `json:"policy"`
}

// applyPolicyFields copies the fields in sets onto p. Lists are copied, so
// nothing later sorts the request's slices or a store's shared ones.
//
// Out-of-range numbers are clamped to a value just past the limit that
// validatePolicy enforces, so the conversion to a duration or an int can
// never wrap around into a value that passes.
func applyPolicyFields(p *policy.Policy, in policyFields) {
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	setSeconds := func(dst *time.Duration, src *int64) {
		if src != nil {
			*dst = secondsToDuration(*src)
		}
	}
	setRate := func(dst *int, src *int64) {
		if src != nil {
			*dst = int(min(max(*src, -1), maxPolicyRate+1))
		}
	}
	setCount := func(dst *int64, src *int64) {
		if src != nil {
			*dst = *src
		}
	}
	setList := func(dst *[]string, src *[]string) {
		if src != nil {
			*dst = slices.Clone(*src)
		}
	}
	setSeconds(&p.MaxKeyLifetime, in.MaxKeyLifetimeSeconds)
	setSeconds(&p.GracePeriod, in.GraceSeconds)
	setList(&p.AllowedScopes, in.AllowedScopes)
	setRate(&p.RateLimit, in.RateLimit)
	setSeconds(&p.RateLimitWindow, in.RateLimitWindowSeconds)
	setRate(&p.BurstLimit, in.BurstLimit)
	setList(&p.AllowedIPs, in.AllowedIPs)
	setList(&p.AllowedOrigins, in.AllowedOrigins)
	setList(&p.AllowedMethods, in.AllowedMethods)
	setList(&p.AllowedPaths, in.AllowedPaths)
	setSeconds(&p.RotationPeriod, in.RotationPeriodSeconds)
	setCount(&p.DailyQuota, in.DailyQuota)
	setCount(&p.MonthlyQuota, in.MonthlyQuota)
}

// secondsToDuration converts whole seconds to a duration, saturating at the
// largest magnitude a duration can hold instead of overflowing.
func secondsToDuration(s int64) time.Duration {
	const limit = math.MaxInt64 / int64(time.Second)
	return time.Duration(min(max(s, -limit), limit)) * time.Second
}

// policyNumber is one numeric field as validatePolicy checks it. value and
// limit share a unit (nanoseconds for durations). limit 0 means no cap.
type policyNumber struct {
	field  string
	value  int64
	limit  int64
	capMsg string
}

// policyList is one list field as validatePolicy normalises and checks it.
type policyList struct {
	field string
	list  *[]string
	upper bool
	check func(entry string) error
}

// validatePolicy normalises p in place (trims the name and description;
// trims, drops blanks from, sorts and de-duplicates every list, upper-casing
// methods) and then checks it. The first failure wins, in this order: name,
// description, negative numbers, caps, rate-limit pairings, lists. Every
// failure is BAD_REQUEST, except a scope lookup that fails for a reason
// other than not found: that comes back unmapped, for the handler to map.
//
// stored is the allowedScopes list the row already had (nil on create).
// Only names not in it are looked up, so a policy that already names a scope
// that has since gone stays editable.
func validatePolicy(ctx context.Context, deps Deps, tenant string, p *policy.Policy, stored []string) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return badRequest("name is required")
	}
	if utf8.RuneCountInString(p.Name) > maxPolicyNameLength {
		return badRequest("name is too long")
	}
	p.Description = strings.TrimSpace(p.Description)
	if utf8.RuneCountInString(p.Description) > maxPolicyDescriptionLength {
		return badRequest("description is too long")
	}

	kept := normaliseList(stored, false)

	const sec = int64(time.Second)
	numbers := []policyNumber{
		{"maxKeyLifetimeSeconds", int64(p.MaxKeyLifetime), maxPolicyLifetimeSeconds * sec, "is at most 10 years"},
		{"graceSeconds", int64(p.GracePeriod), maxPolicyGraceSeconds * sec, "is at most 90 days"},
		{"rateLimit", int64(p.RateLimit), maxPolicyRate, "is too large"},
		{"rateLimitWindowSeconds", int64(p.RateLimitWindow), maxPolicyWindowSeconds * sec, "is at most 31 days"},
		{"burstLimit", int64(p.BurstLimit), maxPolicyRate, "is too large"},
		{"rotationPeriodSeconds", int64(p.RotationPeriod), maxPolicyLifetimeSeconds * sec, "is at most 10 years"},
		{"dailyQuota", p.DailyQuota, 0, ""},
		{"monthlyQuota", p.MonthlyQuota, 0, ""},
	}
	for _, n := range numbers {
		if n.value < 0 {
			return badRequest(n.field + " cannot be negative")
		}
	}
	for _, n := range numbers {
		if n.limit > 0 && n.value > n.limit {
			return badRequest(n.field + " " + n.capMsg)
		}
	}

	if p.RateLimit > 0 && p.RateLimitWindow == 0 {
		return badRequest("a rate limit needs a window")
	}
	if p.BurstLimit > 0 && p.RateLimit == 0 {
		return badRequest("a burst limit needs a rate limit")
	}

	lists := []policyList{
		{"allowedScopes", &p.AllowedScopes, false, func(name string) error {
			if _, found := slices.BinarySearch(kept, name); found {
				return nil
			}
			if _, err := deps.Engine.Store().Scopes().GetByName(ctx, tenant, name); err != nil {
				// The same wrapping the engine uses, so mapScopeError
				// answers it exactly as it does for keys.create.
				return fmt.Errorf("scope %q: %w", name, err)
			}
			return nil
		}},
		{"allowedIps", &p.AllowedIPs, false, func(s string) error {
			if net.ParseIP(s) != nil {
				return nil
			}
			if _, _, err := net.ParseCIDR(s); err == nil {
				return nil
			}
			return badRequest("allowedIps: " + strconv.Quote(s) + " is not an IP address or CIDR range")
		}},
		{"allowedOrigins", &p.AllowedOrigins, false, func(s string) error {
			if s == "*" || isOrigin(s) {
				return nil
			}
			return badRequest("allowedOrigins: " + strconv.Quote(s) + " is not an origin like https://example.com")
		}},
		{"allowedMethods", &p.AllowedMethods, true, func(s string) error {
			if slices.Contains(policyMethods, s) {
				return nil
			}
			return badRequest("allowedMethods: " + strconv.Quote(s) + " is not an HTTP method")
		}},
		{"allowedPaths", &p.AllowedPaths, false, func(s string) error {
			if strings.HasPrefix(s, "/") {
				return nil
			}
			return badRequest("allowedPaths: " + strconv.Quote(s) + " must start with /")
		}},
	}
	for _, l := range lists {
		*l.list = normaliseList(*l.list, l.upper)
		if len(*l.list) > maxPolicyListEntries {
			return badRequest(l.field + " has more than 100 entries")
		}
		for _, entry := range *l.list {
			if err := l.check(entry); err != nil {
				return err
			}
		}
	}
	return nil
}

// normaliseList trims every entry, drops blanks, upper-cases when asked, and
// returns a new sorted slice without duplicates. It never changes in.
func normaliseList(in []string, upper bool) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if upper {
			s = strings.ToUpper(s)
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// isOrigin reports whether s is an http or https origin: a scheme and a host,
// with nothing after the host. A trailing slash counts as a path, because a
// browser's Origin header never carries one.
func isOrigin(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") &&
		u.Host != "" && u.Opaque == "" && u.User == nil &&
		u.Path == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}

// mapPolicyWriteError answers an error from validatePolicy or a policy write.
// A contract error is already the answer. A scope lookup goes through
// mapScopeError, as on keys.create, and everything else through mapError,
// which knows ErrPolicyNameTaken and keeps anything unexpected generic.
func (d Deps) mapPolicyWriteError(intent string, err error) error {
	var ce *dashcontract.Error
	if errors.As(err, &ce) {
		return err
	}
	return d.mapScopeError(intent, err)
}

func policiesCreateHandler(deps Deps) func(context.Context, policyFields, dashcontract.Principal) (policyResponse, error) {
	return func(ctx context.Context, in policyFields, p dashcontract.Principal) (policyResponse, error) {
		const intent = "policies.create"
		if _, err := requireUser(p); err != nil {
			return policyResponse{}, err
		}
		tenant, err := tenantFrom(ctx, p, deps)
		if err != nil {
			return policyResponse{}, err
		}
		app, err := appFrom(p, deps)
		if err != nil {
			return policyResponse{}, err
		}

		pol := &policy.Policy{}
		applyPolicyFields(pol, in)
		if err := validatePolicy(ctx, deps, tenant, pol, nil); err != nil {
			return policyResponse{}, deps.mapPolicyWriteError(intent, err)
		}
		// engineCtx: CreatePolicy stamps the tenant and app from the
		// context, and it prefers a forge Scope the host may have set.
		if err := deps.Engine.CreatePolicy(engineCtx(ctx, app, tenant), pol); err != nil {
			return policyResponse{}, deps.mapPolicyWriteError(intent, err)
		}
		return policyResponse{Policy: projectPolicyDetail(pol)}, nil
	}
}

func policiesUpdateHandler(deps Deps) func(context.Context, policiesUpdateRequest, dashcontract.Principal) (policyResponse, error) {
	return func(ctx context.Context, in policiesUpdateRequest, p dashcontract.Principal) (policyResponse, error) {
		const intent = "policies.update"
		if _, err := requireUser(p); err != nil {
			return policyResponse{}, err
		}
		tenant, err := tenantFrom(ctx, p, deps)
		if err != nil {
			return policyResponse{}, err
		}
		// Edit the loaded row: UpdatePolicy checks the name against
		// pol.TenantID and writes the whole row back.
		pol, err := loadPolicyForTenant(ctx, deps, tenant, in.ID, intent)
		if err != nil {
			return policyResponse{}, err
		}
		// applyPolicyFields replaces the list, never edits it in place.
		stored := pol.AllowedScopes
		applyPolicyFields(pol, in.policyFields)
		if err := validatePolicy(ctx, deps, tenant, pol, stored); err != nil {
			return policyResponse{}, deps.mapPolicyWriteError(intent, err)
		}
		if err := deps.Engine.UpdatePolicy(ctx, pol); err != nil {
			return policyResponse{}, deps.mapPolicyWriteError(intent, err)
		}
		return policyResponse{Policy: projectPolicyDetail(pol)}, nil
	}
}

type policiesDeleteResponse struct {
	ID string `json:"id"`
}

// policyInUse answers a refused delete. blocking is this tenant's count of
// keys that are not revoked; 0 means the count is unknown or every blocking
// key belongs to another tenant, so the message names no number.
func policyInUse(blocking int) error {
	msg := "keys that are not revoked use this policy"
	switch {
	case blocking == 1:
		msg = "1 key that is not revoked uses this policy"
	case blocking > 1:
		msg = strconv.Itoa(blocking) + " keys that are not revoked use this policy"
	}
	return &dashcontract.Error{Code: dashcontract.CodeConflict, Message: msg}
}

func policiesDeleteHandler(deps Deps) func(context.Context, policyIDRequest, dashcontract.Principal) (policiesDeleteResponse, error) {
	return func(ctx context.Context, in policyIDRequest, p dashcontract.Principal) (policiesDeleteResponse, error) {
		const intent = "policies.delete"
		if _, err := requireUser(p); err != nil {
			return policiesDeleteResponse{}, err
		}
		tenant, err := tenantFrom(ctx, p, deps)
		if err != nil {
			return policiesDeleteResponse{}, err
		}
		pol, err := loadPolicyForTenant(ctx, deps, tenant, in.ID, intent)
		if err != nil {
			return policiesDeleteResponse{}, err
		}
		err = deps.Engine.DeletePolicy(ctx, pol.ID)
		switch {
		case errors.Is(err, keysmith.ErrPolicyInUse):
			// The engine counts keys in every tenant; the message counts
			// only this one's, so it never names another tenant's keys.
			_, blocking, cerr := policyKeyCounts(ctx, deps, tenant, pol.ID)
			if cerr != nil {
				blocking = 0
				if deps.Logger != nil {
					deps.Logger.Error("keysmith/contract: count keys for a refused policy delete",
						forge.F("intent", intent),
						forge.F("error", cerr),
					)
				}
			}
			return policiesDeleteResponse{}, policyInUse(blocking)
		case err != nil:
			return policiesDeleteResponse{}, deps.mapError(intent, err)
		}
		return policiesDeleteResponse{ID: pol.ID.String()}, nil
	}
}
