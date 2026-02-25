// Package id defines TypeID-based identity types for all Keysmith entities.
//
// Every entity in Keysmith uses a single ID struct with a prefix that identifies
// the entity type. IDs are K-sortable (UUIDv7-based), globally unique,
// and URL-safe in the format "prefix_suffix".
package id

import (
	"database/sql/driver"
	"fmt"

	"go.jetify.com/typeid/v2"
)

// Prefix identifies the entity type encoded in a TypeID.
type Prefix string

// Prefix constants for all Keysmith entity types.
const (
	PrefixKey      Prefix = "akey"
	PrefixPolicy   Prefix = "kpol"
	PrefixUsage    Prefix = "kusg"
	PrefixRotation Prefix = "krot"
	PrefixScope    Prefix = "kscp"
)

// ID is the primary identifier type for all Keysmith entities.
// It wraps a TypeID providing a prefix-qualified, globally unique,
// sortable, URL-safe identifier in the format "prefix_suffix".
//
//nolint:recvcheck // Value receivers for read-only methods, pointer receivers for UnmarshalText/Scan.
type ID struct {
	inner typeid.TypeID
	valid bool
}

// Nil is the zero-value ID.
var Nil ID

// New generates a new globally unique ID with the given prefix.
// It panics if prefix is not a valid TypeID prefix (programming error).
func New(prefix Prefix) ID {
	tid, err := typeid.Generate(string(prefix))
	if err != nil {
		panic(fmt.Sprintf("id: invalid prefix %q: %v", prefix, err))
	}

	return ID{inner: tid, valid: true}
}

// Parse parses a TypeID string (e.g., "akey_01h2xcejqtf2nbrexx3vqjhp41")
// into an ID. Returns an error if the string is not valid.
func Parse(s string) (ID, error) {
	if s == "" {
		return Nil, fmt.Errorf("id: parse %q: empty string", s)
	}

	tid, err := typeid.Parse(s)
	if err != nil {
		return Nil, fmt.Errorf("id: parse %q: %w", s, err)
	}

	return ID{inner: tid, valid: true}, nil
}

// ParseWithPrefix parses a TypeID string and validates that its prefix
// matches the expected value.
func ParseWithPrefix(s string, expected Prefix) (ID, error) {
	parsed, err := Parse(s)
	if err != nil {
		return Nil, err
	}

	if parsed.Prefix() != expected {
		return Nil, fmt.Errorf("id: expected prefix %q, got %q", expected, parsed.Prefix())
	}

	return parsed, nil
}

// MustParse is like Parse but panics on error. Use for hardcoded ID values.
func MustParse(s string) ID {
	parsed, err := Parse(s)
	if err != nil {
		panic(fmt.Sprintf("id: must parse %q: %v", s, err))
	}

	return parsed
}

// MustParseWithPrefix is like ParseWithPrefix but panics on error.
func MustParseWithPrefix(s string, expected Prefix) ID {
	parsed, err := ParseWithPrefix(s, expected)
	if err != nil {
		panic(fmt.Sprintf("id: must parse with prefix %q: %v", expected, err))
	}

	return parsed
}

// ──────────────────────────────────────────────────
// Type aliases for backward compatibility
// ──────────────────────────────────────────────────

// KeyID is a type-safe identifier for API keys (prefix: "akey").
type KeyID = ID

// PolicyID is a type-safe identifier for key policies (prefix: "kpol").
type PolicyID = ID

// UsageID is a type-safe identifier for usage records (prefix: "kusg").
type UsageID = ID

// RotationID is a type-safe identifier for rotation records (prefix: "krot").
type RotationID = ID

// ScopeID is a type-safe identifier for key scopes (prefix: "kscp").
type ScopeID = ID

// AnyID is a type alias that accepts any valid prefix.
type AnyID = ID

// ──────────────────────────────────────────────────
// Convenience constructors
// ──────────────────────────────────────────────────

// NewKeyID generates a new unique API key ID.
func NewKeyID() ID { return New(PrefixKey) }

// NewPolicyID generates a new unique policy ID.
func NewPolicyID() ID { return New(PrefixPolicy) }

// NewUsageID generates a new unique usage ID.
func NewUsageID() ID { return New(PrefixUsage) }

// NewRotationID generates a new unique rotation ID.
func NewRotationID() ID { return New(PrefixRotation) }

// NewScopeID generates a new unique scope ID.
func NewScopeID() ID { return New(PrefixScope) }

// ──────────────────────────────────────────────────
// Convenience parsers
// ──────────────────────────────────────────────────

// ParseKeyID parses a string and validates the "akey" prefix.
func ParseKeyID(s string) (ID, error) { return ParseWithPrefix(s, PrefixKey) }

// ParsePolicyID parses a string and validates the "kpol" prefix.
func ParsePolicyID(s string) (ID, error) { return ParseWithPrefix(s, PrefixPolicy) }

// ParseUsageID parses a string and validates the "kusg" prefix.
func ParseUsageID(s string) (ID, error) { return ParseWithPrefix(s, PrefixUsage) }

// ParseRotationID parses a string and validates the "krot" prefix.
func ParseRotationID(s string) (ID, error) { return ParseWithPrefix(s, PrefixRotation) }

// ParseScopeID parses a string and validates the "kscp" prefix.
func ParseScopeID(s string) (ID, error) { return ParseWithPrefix(s, PrefixScope) }

// ParseAny parses a string into an ID without type checking the prefix.
func ParseAny(s string) (ID, error) { return Parse(s) }

// ──────────────────────────────────────────────────
// ID methods
// ──────────────────────────────────────────────────

// String returns the full TypeID string representation (prefix_suffix).
// Returns an empty string for the Nil ID.
func (i ID) String() string {
	if !i.valid {
		return ""
	}

	return i.inner.String()
}

// Prefix returns the prefix component of this ID.
func (i ID) Prefix() Prefix {
	if !i.valid {
		return ""
	}

	return Prefix(i.inner.Prefix())
}

// IsNil reports whether this ID is the zero value.
func (i ID) IsNil() bool {
	return !i.valid
}

// MarshalText implements encoding.TextMarshaler.
func (i ID) MarshalText() ([]byte, error) {
	if !i.valid {
		return []byte{}, nil
	}

	return []byte(i.inner.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (i *ID) UnmarshalText(data []byte) error {
	if len(data) == 0 {
		*i = Nil

		return nil
	}

	parsed, err := Parse(string(data))
	if err != nil {
		return err
	}

	*i = parsed

	return nil
}

// Value implements driver.Valuer for database storage.
// Returns nil for the Nil ID so that optional foreign key columns store NULL.
func (i ID) Value() (driver.Value, error) {
	if !i.valid {
		return nil, nil //nolint:nilnil // nil is the canonical NULL for driver.Valuer
	}

	return i.inner.String(), nil
}

// Scan implements sql.Scanner for database retrieval.
func (i *ID) Scan(src any) error {
	if src == nil {
		*i = Nil

		return nil
	}

	switch v := src.(type) {
	case string:
		if v == "" {
			*i = Nil

			return nil
		}

		return i.UnmarshalText([]byte(v))
	case []byte:
		if len(v) == 0 {
			*i = Nil

			return nil
		}

		return i.UnmarshalText(v)
	default:
		return fmt.Errorf("id: cannot scan %T into ID", src)
	}
}
