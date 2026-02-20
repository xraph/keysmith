package id_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith/id"
)

func TestNewKeyID_RoundTrip(t *testing.T) {
	kid := id.NewKeyID()
	assert.Contains(t, kid.String(), "akey_")

	parsed, err := id.ParseKeyID(kid.String())
	require.NoError(t, err)
	assert.Equal(t, kid.String(), parsed.String())
}

func TestNewPolicyID_RoundTrip(t *testing.T) {
	pid := id.NewPolicyID()
	assert.Contains(t, pid.String(), "kpol_")

	parsed, err := id.ParsePolicyID(pid.String())
	require.NoError(t, err)
	assert.Equal(t, pid.String(), parsed.String())
}

func TestNewUsageID_RoundTrip(t *testing.T) {
	uid := id.NewUsageID()
	assert.Contains(t, uid.String(), "kusg_")

	parsed, err := id.ParseUsageID(uid.String())
	require.NoError(t, err)
	assert.Equal(t, uid.String(), parsed.String())
}

func TestNewRotationID_RoundTrip(t *testing.T) {
	rid := id.NewRotationID()
	assert.Contains(t, rid.String(), "krot_")

	parsed, err := id.ParseRotationID(rid.String())
	require.NoError(t, err)
	assert.Equal(t, rid.String(), parsed.String())
}

func TestNewScopeID_RoundTrip(t *testing.T) {
	sid := id.NewScopeID()
	assert.Contains(t, sid.String(), "kscp_")

	parsed, err := id.ParseScopeID(sid.String())
	require.NoError(t, err)
	assert.Equal(t, sid.String(), parsed.String())
}

func TestParseKeyID_WrongPrefix(t *testing.T) {
	pid := id.NewPolicyID()
	_, err := id.ParseKeyID(pid.String())
	assert.Error(t, err)
}

func TestParseAny(t *testing.T) {
	kid := id.NewKeyID()
	anyID, err := id.ParseAny(kid.String())
	require.NoError(t, err)
	assert.Equal(t, kid.String(), anyID.String())
}

func TestParseKeyID_Invalid(t *testing.T) {
	_, err := id.ParseKeyID("invalid")
	assert.Error(t, err)
}

func TestUniqueIDs(t *testing.T) {
	a := id.NewKeyID()
	b := id.NewKeyID()
	assert.NotEqual(t, a.String(), b.String())
}
