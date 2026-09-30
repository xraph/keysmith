package contract

import (
	"time"

	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
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

// PolicyRef is the slice of a policy a key's page needs.
type PolicyRef struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	MaxKeyLifetimeSeconds int64  `json:"maxKeyLifetimeSeconds"`
	GraceSeconds          int64  `json:"graceSeconds"`
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
		MaxKeyLifetimeSeconds: int64(p.MaxKeyLifetime / time.Second),
		GraceSeconds:          int64(p.GracePeriod / time.Second),
	}
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// rfc3339Ptr is empty for nil so omitempty drops the field.
func rfc3339Ptr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return rfc3339(*t)
}
