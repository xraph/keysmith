package contract

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
)

func TestEffectiveState(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	revokedAt := now.Add(-time.Hour)
	cases := []struct {
		name    string
		k       key.Key
		state   string
		pending bool
	}{
		{"active", key.Key{State: key.StateActive, ExpiresAt: &future}, "active", false},
		{"active past expiry is expired, pending", key.Key{State: key.StateActive, ExpiresAt: &past}, "expired", true},
		{"expiry exactly now is expired", key.Key{State: key.StateActive, ExpiresAt: &now}, "expired", true},
		{"stored expired", key.Key{State: key.StateExpired, ExpiresAt: &past}, "expired", false},
		{"suspended past expiry stays suspended", key.Key{State: key.StateSuspended, ExpiresAt: &past}, "suspended", false},
		{"revoked_at wins over a stale active state", key.Key{State: key.StateActive, RevokedAt: &revokedAt}, "revoked", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, p := effectiveState(&tc.k, now)
			assert.Equal(t, tc.state, s)
			assert.Equal(t, tc.pending, p)
		})
	}
}

func TestExpiresSoonOnlyForActiveWithinSevenDays(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	in3d, in8d := now.Add(72*time.Hour), now.Add(8*24*time.Hour)
	assert.True(t, projectKey(&key.Key{State: key.StateActive, ExpiresAt: &in3d}, nil, now).ExpiresSoon)
	assert.False(t, projectKey(&key.Key{State: key.StateActive, ExpiresAt: &in8d}, nil, now).ExpiresSoon)
	assert.False(t, projectKey(&key.Key{State: key.StateSuspended, ExpiresAt: &in3d}, nil, now).ExpiresSoon)
	assert.NotNil(t, projectKey(&key.Key{State: key.StateActive}, nil, now).Scopes)
}

func TestPolicyRefSendsUnsetGraceAndLifetimeAsNull(t *testing.T) {
	ref := projectPolicyRef(&policy.Policy{ID: id.NewPolicyID(), Name: "open"})
	assert.Nil(t, ref.GraceSeconds, "zero grace means not set, and RotateKey then uses 24h")
	assert.Nil(t, ref.MaxKeyLifetimeSeconds, "zero lifetime means no maximum")
	b, err := json.Marshal(ref)
	require.NoError(t, err)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(b, &wire))
	for _, field := range []string{"graceSeconds", "maxKeyLifetimeSeconds"} {
		v, present := wire[field]
		assert.True(t, present, "%s must be sent as an explicit null", field)
		assert.Nil(t, v, field)
	}

	set := projectPolicyRef(&policy.Policy{ID: id.NewPolicyID(), Name: "p", GracePeriod: 2 * time.Hour, MaxKeyLifetime: 720 * time.Hour})
	require.NotNil(t, set.GraceSeconds)
	require.NotNil(t, set.MaxKeyLifetimeSeconds)
	assert.EqualValues(t, 7200, *set.GraceSeconds)
	assert.EqualValues(t, 720*3600, *set.MaxKeyLifetimeSeconds)
}
