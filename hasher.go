package keysmith

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// Hasher hashes raw API keys for secure storage.
type Hasher interface {
	// Hash produces a deterministic hash of the raw key.
	Hash(rawKey string) (string, error)

	// Verify checks whether a raw key matches a stored hash.
	Verify(rawKey, hash string) (bool, error)
}

// DefaultHasher returns a SHA-256 hasher.
func DefaultHasher() Hasher { return &sha256Hasher{} }

type sha256Hasher struct{}

func (h *sha256Hasher) Hash(rawKey string) (string, error) {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:]), nil
}

func (h *sha256Hasher) Verify(rawKey, hash string) (bool, error) {
	computed, err := h.Hash(rawKey)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1, nil
}
