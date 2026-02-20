package keysmith

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/xraph/keysmith/key"
)

// KeyGenerator generates raw API key strings.
type KeyGenerator interface {
	// Generate produces a raw API key string with the given prefix and environment.
	Generate(prefix string, env key.Environment) (string, error)
}

// DefaultKeyGenerator returns a generator producing keys in the format:
// {prefix}_{env}_{64 random hex chars} (e.g., "sk_live_a3f8b2c9...").
func DefaultKeyGenerator() KeyGenerator { return &defaultGenerator{byteLen: 32} }

type defaultGenerator struct {
	byteLen int
}

func (g *defaultGenerator) Generate(prefix string, env key.Environment) (string, error) {
	b := make([]byte, g.byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return fmt.Sprintf("%s_%s_%s", prefix, env, hex.EncodeToString(b)), nil
}
