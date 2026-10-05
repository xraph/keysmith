package contract

import (
	"slices"
	"sort"
	"time"

	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
)

// expiresSoonWindow is how far ahead an active key counts as expiring soon.
const expiresSoonWindow = 7 * 24 * time.Hour

// KeySummary is the public shape of a key. It carries the hint (the last
// four characters of the raw key) and never the raw key or its hash.
type KeySummary struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	Prefix         string   `json:"prefix"`
	Hint           string   `json:"hint"`
	Environment    string   `json:"environment"`
	State          string   `json:"state"`          // as stored
	EffectiveState string   `json:"effectiveState"` // what the page shows
	ExpiryPending  bool     `json:"expiryPending"`  // expired, not yet marked
	ExpiresSoon    bool     `json:"expiresSoon"`    // active, expires within 7 days
	PolicyID       string   `json:"policyId,omitempty"`
	Scopes         []string `json:"scopes"`
	CreatedBy      string   `json:"createdBy,omitempty"`
	ExpiresAt      string   `json:"expiresAt,omitempty"`
	LastUsedAt     string   `json:"lastUsedAt,omitempty"`
	RotatedAt      string   `json:"rotatedAt,omitempty"`
	RevokedAt      string   `json:"revokedAt,omitempty"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
}

// PolicyRef is the slice of a policy a key's page needs. The engine reads a
// zero grace as "not set" (RotateKey then uses 24h) and a zero lifetime as
// "no maximum", so both go out as an explicit null, never as 0.
type PolicyRef struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	MaxKeyLifetimeSeconds *int64 `json:"maxKeyLifetimeSeconds"`
	GraceSeconds          *int64 `json:"graceSeconds"`
}

// PolicySummary is a policy as the key forms' picker shows it. Grace and
// lifetime follow PolicyRef: an unset value goes out as an explicit null.
type PolicySummary struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	Description           string   `json:"description,omitempty"`
	MaxKeyLifetimeSeconds *int64   `json:"maxKeyLifetimeSeconds"`
	GraceSeconds          *int64   `json:"graceSeconds"`
	AllowedScopes         []string `json:"allowedScopes"` // never nil
}

// PolicyDetail is every policy field the editor and the detail page show.
// Counts and durations follow PolicyRef: zero means unset to the engine, so
// it goes out as an explicit null. Lists go out sorted, without duplicates,
// and as [] when empty, never null. Durations are whole seconds.
type PolicyDetail struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	Description            string   `json:"description,omitempty"`
	MaxKeyLifetimeSeconds  *int64   `json:"maxKeyLifetimeSeconds"`
	GraceSeconds           *int64   `json:"graceSeconds"`
	AllowedScopes          []string `json:"allowedScopes"`
	RateLimit              *int64   `json:"rateLimit"`
	RateLimitWindowSeconds *int64   `json:"rateLimitWindowSeconds"`
	BurstLimit             *int64   `json:"burstLimit"`
	AllowedIPs             []string `json:"allowedIps"`
	AllowedOrigins         []string `json:"allowedOrigins"`
	AllowedMethods         []string `json:"allowedMethods"`
	AllowedPaths           []string `json:"allowedPaths"`
	RotationPeriodSeconds  *int64   `json:"rotationPeriodSeconds"`
	DailyQuota             *int64   `json:"dailyQuota"`
	MonthlyQuota           *int64   `json:"monthlyQuota"`
	CreatedAt              string   `json:"createdAt"`
	UpdatedAt              string   `json:"updatedAt"`
}

// ScopeSummary is a scope as the key forms' picker shows it.
type ScopeSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Parent      string `json:"parent,omitempty"`
	Description string `json:"description,omitempty"`
}

// PreviousKey is an open rotation window: the old key still validates until
// GraceEnds. Only the old key's hint is shown.
type PreviousKey struct {
	RotationID string `json:"rotationId"`
	Hint       string `json:"hint"`
	Reason     string `json:"reason"`
	RotatedAt  string `json:"rotatedAt"`
	GraceEnds  string `json:"graceEnds"`
}

// RotationItem is one rotation as the Rotations page and the key page's
// history show it. Masked forms are built on the client from prefix,
// environment and the hints. Neither key's hash is ever carried.
type RotationItem struct {
	ID    string `json:"id"`
	KeyID string `json:"keyId"`
	// KeyName, Prefix and Environment are null together when the key no
	// longer exists in this tenant.
	KeyName     *string `json:"keyName"`
	Prefix      *string `json:"prefix"`
	Environment *string `json:"environment"`
	// OldHint and NewHint are "" on records written before hints existed.
	OldHint string `json:"oldHint"`
	NewHint string `json:"newHint"`
	Reason  string `json:"reason"`
	// GraceSeconds is the window the rotation recorded. 0 is a real
	// zero-grace rotation, not unset, so it goes out as 0, never null.
	GraceSeconds int64  `json:"graceSeconds"`
	GraceEnds    string `json:"graceEnds"`
	// WindowOpen is true while the previous key still validates: the key
	// exists in this tenant, the record has an old hint, and GraceEnds is
	// ahead of now.
	WindowOpen bool   `json:"windowOpen"`
	RotatedBy  string `json:"rotatedBy,omitempty"`
	RotatedAt  string `json:"rotatedAt"` // the record's CreatedAt
}

// projectRotationItem builds the wire shape of r. k is the rotated key, or
// nil when it no longer exists in the caller's tenant; the caller decides
// that, so k is used as given.
//
// A window is open only while GraceEnds is ahead of now, the record has an
// old hint, and k is not nil. A hintless record was written before the grace
// fix, when RotateKey recorded a window but killed the old key at once, and
// the engine's ValidateKey never honours it; keys.detail's previousKeys
// draws the same line, so the two pages agree. ValidateKey also loads the
// rotated key after finding the grace record, so once the key is gone the
// old key stops validating too, and a key outside the tenant is never this
// tenant's window to show.
func projectRotationItem(r *rotation.Record, k *key.Key, now time.Time) RotationItem {
	it := RotationItem{
		ID:           r.ID.String(),
		KeyID:        r.KeyID.String(),
		OldHint:      r.OldHint,
		NewHint:      r.NewHint,
		Reason:       string(r.Reason),
		GraceSeconds: int64(r.GraceTTL / time.Second),
		GraceEnds:    rfc3339(r.GraceEnds),
		WindowOpen:   k != nil && r.OldHint != "" && r.GraceEnds.After(now),
		RotatedBy:    r.RotatedBy,
		RotatedAt:    rfc3339(r.CreatedAt),
	}
	if k != nil {
		name, prefix, env := k.Name, k.Prefix, string(k.Environment)
		it.KeyName, it.Prefix, it.Environment = &name, &prefix, &env
	}
	return it
}

// effectiveState is the state the page shows. A revoked_at timestamp wins
// over whatever the state column says. A key stored as active whose expiry
// has passed reads as expired, and pending is true because nothing has
// written that state yet (the cleanup job has not run).
func effectiveState(k *key.Key, now time.Time) (state string, pending bool) {
	if k.RevokedAt != nil {
		return string(key.StateRevoked), false
	}
	if k.State == key.StateActive && k.ExpiresAt != nil && !k.ExpiresAt.After(now) {
		return string(key.StateExpired), true
	}
	return string(k.State), false
}

// projectKey builds the wire shape of k. scopes come from the scope store,
// because key reads never populate key.Key.Scopes.
func projectKey(k *key.Key, scopes []string, now time.Time) KeySummary {
	state, pending := effectiveState(k, now)
	if scopes == nil {
		scopes = []string{}
	}
	s := KeySummary{
		ID:             k.ID.String(),
		Name:           k.Name,
		Description:    k.Description,
		Prefix:         k.Prefix,
		Hint:           k.Hint,
		Environment:    string(k.Environment),
		State:          string(k.State),
		EffectiveState: state,
		ExpiryPending:  pending,
		Scopes:         scopes,
		CreatedBy:      k.CreatedBy,
		ExpiresAt:      rfc3339Ptr(k.ExpiresAt),
		LastUsedAt:     rfc3339Ptr(k.LastUsedAt),
		RotatedAt:      rfc3339Ptr(k.RotatedAt),
		RevokedAt:      rfc3339Ptr(k.RevokedAt),
		CreatedAt:      rfc3339(k.CreatedAt),
		UpdatedAt:      rfc3339(k.UpdatedAt),
	}
	if k.PolicyID != nil {
		s.PolicyID = k.PolicyID.String()
	}
	if state == string(key.StateActive) && k.ExpiresAt != nil &&
		k.ExpiresAt.After(now) && k.ExpiresAt.Sub(now) <= expiresSoonWindow {
		s.ExpiresSoon = true
	}
	return s
}

func projectPolicyRef(p *policy.Policy) PolicyRef {
	return PolicyRef{
		ID:                    p.ID.String(),
		Name:                  p.Name,
		MaxKeyLifetimeSeconds: secondsOrNil(p.MaxKeyLifetime),
		GraceSeconds:          secondsOrNil(p.GracePeriod),
	}
}

func projectPolicySummary(p *policy.Policy) PolicySummary {
	allowed := p.AllowedScopes
	if allowed == nil {
		allowed = []string{}
	}
	return PolicySummary{
		ID:                    p.ID.String(),
		Name:                  p.Name,
		Description:           p.Description,
		MaxKeyLifetimeSeconds: secondsOrNil(p.MaxKeyLifetime),
		GraceSeconds:          secondsOrNil(p.GracePeriod),
		AllowedScopes:         allowed,
	}
}

func projectPolicyDetail(p *policy.Policy) PolicyDetail {
	return PolicyDetail{
		ID:                     p.ID.String(),
		Name:                   p.Name,
		Description:            p.Description,
		MaxKeyLifetimeSeconds:  secondsOrNil(p.MaxKeyLifetime),
		GraceSeconds:           secondsOrNil(p.GracePeriod),
		AllowedScopes:          sortedUnique(p.AllowedScopes),
		RateLimit:              countOrNil(int64(p.RateLimit)),
		RateLimitWindowSeconds: secondsOrNil(p.RateLimitWindow),
		BurstLimit:             countOrNil(int64(p.BurstLimit)),
		AllowedIPs:             sortedUnique(p.AllowedIPs),
		AllowedOrigins:         sortedUnique(p.AllowedOrigins),
		AllowedMethods:         sortedUnique(p.AllowedMethods),
		AllowedPaths:           sortedUnique(p.AllowedPaths),
		RotationPeriodSeconds:  secondsOrNil(p.RotationPeriod),
		DailyQuota:             countOrNil(p.DailyQuota),
		MonthlyQuota:           countOrNil(p.MonthlyQuota),
		CreatedAt:              rfc3339(p.CreatedAt),
		UpdatedAt:              rfc3339(p.UpdatedAt),
	}
}

// sortedUnique is a sorted copy of in without duplicates, and [] for nil.
// It never sorts in in place: in may be a store's shared state.
func sortedUnique(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return slices.Compact(out)
}

// countOrNil is nil for zero, which the engine treats as unset, the way
// secondsOrNil treats a zero duration.
func countOrNil(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}

// secondsOrNil is nil for a zero duration, which the engine treats as unset.
func secondsOrNil(d time.Duration) *int64 {
	if d == 0 {
		return nil
	}
	s := int64(d / time.Second)
	return &s
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// rfc3339Ptr is empty for nil so omitempty drops the field.
func rfc3339Ptr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return rfc3339(*t)
}
