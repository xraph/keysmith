package store

import "errors"

// Not-found sentinels. Every backend returns these (possibly wrapped), so
// callers can tell a missing row from a failed query with errors.Is. The
// root keysmith package re-exports them under the same names.
var (
	ErrKeyNotFound      = errors.New("keysmith: key not found")
	ErrPolicyNotFound   = errors.New("keysmith: policy not found")
	ErrScopeNotFound    = errors.New("keysmith: scope not found")
	ErrRotationNotFound = errors.New("keysmith: rotation record not found")
)

// ErrKeyConflict is returned by key.Store.UpdateIfVersion when the key exists
// but its stored version is not the one the caller read: another write
// landed in between. Every backend returns it (possibly wrapped), and the
// root keysmith package re-exports it under the same name.
var ErrKeyConflict = errors.New("keysmith: key changed since it was read")

// NotFound maps an entity name to its sentinel. Backends call it from their
// errNotFound helper.
func NotFound(entity string) error {
	switch entity {
	case "key":
		return ErrKeyNotFound
	case "policy":
		return ErrPolicyNotFound
	case "scope":
		return ErrScopeNotFound
	case "rotation":
		return ErrRotationNotFound
	default:
		return errors.New("keysmith: " + entity + " not found")
	}
}
