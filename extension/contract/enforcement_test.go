package contract

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The table in the policy editor's order: its three groups top to bottom,
// and the fields in the order the editor lays them out within each group.
// Labels are the editor's own, so the settings page and the editor read
// the same.
func TestEnforcementTableFollowsTheEditor(t *testing.T) {
	want := []EnforcementRow{
		{Field: "maxKeyLifetimeSeconds", Label: "Max key lifetime", Group: "keysmith", Enforced: true, When: "when a key is created"},
		{Field: "graceSeconds", Label: "Grace on rotation", Group: "keysmith", Enforced: true, When: "when a key is rotated"},
		{Field: "allowedScopes", Label: "Allowed scopes", Group: "keysmith", Enforced: true, When: "when a key is created or its scopes are assigned"},
		{Field: "rateLimit", Label: "Rate limit", Group: "rateLimiter", Enforced: true, When: "when a key is validated"},
		{Field: "rateLimitWindowSeconds", Label: "Window", Group: "rateLimiter", Enforced: true, When: "when a key is validated"},
		{Field: "burstLimit", Label: "Burst limit", Group: "application"},
		{Field: "rotationPeriodSeconds", Label: "Rotation period", Group: "application"},
		{Field: "dailyQuota", Label: "Daily quota", Group: "application"},
		{Field: "monthlyQuota", Label: "Monthly quota", Group: "application"},
		{Field: "allowedIps", Label: "Allowed IPs", Group: "application"},
		{Field: "allowedOrigins", Label: "Allowed origins", Group: "application"},
		{Field: "allowedPaths", Label: "Allowed paths", Group: "application"},
		{Field: "allowedMethods", Label: "Allowed methods", Group: "application"},
	}
	assert.Equal(t, want, enforcementTable(true))

	// Without a limiter the rate limit rows are stored and never checked,
	// so they say so, and nothing claims a moment it is checked.
	want[3].Enforced, want[3].When = false, ""
	want[4].Enforced, want[4].When = false, ""
	assert.Equal(t, want, enforcementTable(false))
}

func TestEnforcedFieldCountMatchesTheTable(t *testing.T) {
	assert.Equal(t, 3, enforcedPolicyFieldCount(false))
	assert.Equal(t, 5, enforcedPolicyFieldCount(true))
	for _, limiter := range []bool{false, true} {
		n := 0
		for _, r := range enforcementTable(limiter) {
			if r.Enforced {
				n++
			}
			// When is set exactly when the field is enforced.
			assert.Equal(t, r.Enforced, r.When != "", r.Field)
		}
		assert.Equal(t, n, enforcedPolicyFieldCount(limiter))
	}
	assert.Equal(t, 13, policyFieldCount)
}

// Every policy field the write path takes is in the table once, by the same
// wire name, and nothing else is: name and description are not policy
// rules.
func TestEnforcementTableNamesEveryPolicyField(t *testing.T) {
	var wire []string
	typ := reflect.TypeOf(policyFields{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name == "name" || name == "description" {
			continue
		}
		wire = append(wire, name)
	}
	require.Len(t, wire, policyFieldCount)

	rows := enforcementTable(true)
	fields := make([]string, 0, len(rows))
	for _, r := range rows {
		fields = append(fields, r.Field)
	}
	assert.ElementsMatch(t, wire, fields)
}

// Callers get their own copy: changing one answer never changes the next.
func TestEnforcementTableIsACopy(t *testing.T) {
	a := enforcementTable(true)
	a[0].Label = "changed"
	a[3].Enforced = false
	b := enforcementTable(true)
	assert.Equal(t, "Max key lifetime", b[0].Label)
	assert.True(t, b[3].Enforced)
}
