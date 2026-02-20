package keysmith_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
)

func TestHasher_Deterministic(t *testing.T) {
	h := keysmith.DefaultHasher()
	rawKey := "sk_live_abc123def456"

	hash1, err := h.Hash(rawKey)
	require.NoError(t, err)

	hash2, err := h.Hash(rawKey)
	require.NoError(t, err)

	assert.Equal(t, hash1, hash2)
}

func TestHasher_DifferentKeys(t *testing.T) {
	h := keysmith.DefaultHasher()

	hash1, err := h.Hash("sk_live_key1")
	require.NoError(t, err)

	hash2, err := h.Hash("sk_live_key2")
	require.NoError(t, err)

	assert.NotEqual(t, hash1, hash2)
}

func TestHasher_VerifyRoundTrip(t *testing.T) {
	h := keysmith.DefaultHasher()
	rawKey := "sk_live_abc123def456"

	hash, err := h.Hash(rawKey)
	require.NoError(t, err)

	ok, err := h.Verify(rawKey, hash)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestHasher_VerifyWrongKey(t *testing.T) {
	h := keysmith.DefaultHasher()

	hash, err := h.Hash("sk_live_correct")
	require.NoError(t, err)

	ok, err := h.Verify("sk_live_wrong", hash)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHasher_HashFormat(t *testing.T) {
	h := keysmith.DefaultHasher()
	hash, err := h.Hash("test")
	require.NoError(t, err)
	// SHA-256 produces a 64-character hex string.
	assert.Len(t, hash, 64)
}
