package keysmith

import "github.com/xraph/keysmith/key"

// Config holds configuration for the Keysmith engine.
type Config struct {
	// DefaultPrefix is the default key prefix (e.g., "sk").
	DefaultPrefix string

	// DefaultEnvironment is the default key environment.
	DefaultEnvironment key.Environment

	// DefaultKeyLength is the byte count for the random portion of keys.
	DefaultKeyLength int
}
