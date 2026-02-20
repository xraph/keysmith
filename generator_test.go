package keysmith_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/key"
)

func TestGenerator_Format(t *testing.T) {
	g := keysmith.DefaultKeyGenerator()

	tests := []struct {
		prefix string
		env    key.Environment
		want   string // expected prefix pattern
	}{
		{"sk", key.EnvLive, "sk_live_"},
		{"sk", key.EnvTest, "sk_test_"},
		{"pk", key.EnvStaging, "pk_staging_"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix+"_"+string(tt.env), func(t *testing.T) {
			rawKey, err := g.Generate(tt.prefix, tt.env)
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(rawKey, tt.want), "key %q should start with %q", rawKey, tt.want)

			// 64 hex chars after the prefix_env_ part.
			suffix := rawKey[len(tt.want):]
			assert.Len(t, suffix, 64)
		})
	}
}

func TestGenerator_Uniqueness(t *testing.T) {
	g := keysmith.DefaultKeyGenerator()

	key1, err := g.Generate("sk", key.EnvLive)
	require.NoError(t, err)

	key2, err := g.Generate("sk", key.EnvLive)
	require.NoError(t, err)

	assert.NotEqual(t, key1, key2)
}
