// Package id provides TypeID-based identity types for all Keysmith entities.
//
// Every entity in Keysmith gets a type-prefixed, K-sortable, UUIDv7-based
// identifier. IDs are validated at parse time to ensure the prefix matches
// the expected type.
//
// Examples:
//
//	akey_01h2xcejqtf2nbrexx3vqjhp41
//	kpol_01h2xcejqtf2nbrexx3vqjhp41
//	kscp_01h455vb4pex5vsknk084sn02q
package id

import (
	"fmt"

	"go.jetify.com/typeid/v2"
)

// ──────────────────────────────────────────────────
// Prefix constants
// ──────────────────────────────────────────────────

const (
	// PrefixKey is the TypeID prefix for API keys.
	PrefixKey = "akey"

	// PrefixPolicy is the TypeID prefix for key policies.
	PrefixPolicy = "kpol"

	// PrefixUsage is the TypeID prefix for usage records.
	PrefixUsage = "kusg"

	// PrefixRotation is the TypeID prefix for rotation records.
	PrefixRotation = "krot"

	// PrefixScope is the TypeID prefix for key scopes.
	PrefixScope = "kscp"
)

// ──────────────────────────────────────────────────
// Type aliases for readability
// ──────────────────────────────────────────────────

// KeyID is a type-safe identifier for API keys (prefix: "akey").
type KeyID = typeid.TypeID

// PolicyID is a type-safe identifier for key policies (prefix: "kpol").
type PolicyID = typeid.TypeID

// UsageID is a type-safe identifier for usage records (prefix: "kusg").
type UsageID = typeid.TypeID

// RotationID is a type-safe identifier for rotation records (prefix: "krot").
type RotationID = typeid.TypeID

// ScopeID is a type-safe identifier for key scopes (prefix: "kscp").
type ScopeID = typeid.TypeID

// AnyID is a TypeID that accepts any valid prefix.
type AnyID = typeid.TypeID

// ──────────────────────────────────────────────────
// Constructors
// ──────────────────────────────────────────────────

// NewKeyID returns a new random KeyID.
func NewKeyID() KeyID { return must(typeid.Generate(PrefixKey)) }

// NewPolicyID returns a new random PolicyID.
func NewPolicyID() PolicyID { return must(typeid.Generate(PrefixPolicy)) }

// NewUsageID returns a new random UsageID.
func NewUsageID() UsageID { return must(typeid.Generate(PrefixUsage)) }

// NewRotationID returns a new random RotationID.
func NewRotationID() RotationID { return must(typeid.Generate(PrefixRotation)) }

// NewScopeID returns a new random ScopeID.
func NewScopeID() ScopeID { return must(typeid.Generate(PrefixScope)) }

// ──────────────────────────────────────────────────
// Parsing (validates prefix at parse time)
// ──────────────────────────────────────────────────

// ParseKeyID parses a string into a KeyID. Returns an error if the
// prefix is not "akey" or the suffix is invalid.
func ParseKeyID(s string) (KeyID, error) { return parseWithPrefix(PrefixKey, s) }

// ParsePolicyID parses a string into a PolicyID.
func ParsePolicyID(s string) (PolicyID, error) { return parseWithPrefix(PrefixPolicy, s) }

// ParseUsageID parses a string into a UsageID.
func ParseUsageID(s string) (UsageID, error) { return parseWithPrefix(PrefixUsage, s) }

// ParseRotationID parses a string into a RotationID.
func ParseRotationID(s string) (RotationID, error) { return parseWithPrefix(PrefixRotation, s) }

// ParseScopeID parses a string into a ScopeID.
func ParseScopeID(s string) (ScopeID, error) { return parseWithPrefix(PrefixScope, s) }

// ParseAny parses a string into an AnyID, accepting any valid prefix.
func ParseAny(s string) (AnyID, error) { return typeid.Parse(s) }

// ──────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────

// parseWithPrefix parses a TypeID and validates that its prefix matches expected.
func parseWithPrefix(expected, s string) (typeid.TypeID, error) {
	tid, err := typeid.Parse(s)
	if err != nil {
		return tid, err
	}
	if tid.Prefix() != expected {
		return tid, fmt.Errorf("id: expected prefix %q, got %q", expected, tid.Prefix())
	}
	return tid, nil
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
