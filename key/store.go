package key

import (
	"context"
	"time"

	"github.com/xraph/keysmith/id"
)

// Store is the persistence interface for API keys.
type Store interface {
	Create(ctx context.Context, key *Key) error
	Get(ctx context.Context, keyID id.KeyID) (*Key, error)
	GetByHash(ctx context.Context, hash string) (*Key, error)
	GetByPrefix(ctx context.Context, prefix, hint string) (*Key, error)

	// Update writes the whole row whatever its stored version, and adds one
	// to that version. It ignores key.Version and leaves it as it was. It is
	// last-writer-wins on purpose: callers that write keys directly and never
	// read a version keep working.
	Update(ctx context.Context, key *Key) error

	// UpdateIfVersion writes the whole row only when the stored version
	// equals version, sets the stored version to version+1, and sets
	// key.Version to it. A key stored at any other version is left alone and
	// the call returns store.ErrKeyConflict. A missing key returns
	// store.ErrKeyNotFound.
	UpdateIfVersion(ctx context.Context, key *Key, version int64) error

	// UpdateState sets the state and updated_at, and adds one to the stored
	// version.
	UpdateState(ctx context.Context, keyID id.KeyID, state State) error

	// UpdateLastUsed sets last_used_at and leaves the version alone. It runs
	// on every validation, so counting it would turn every version-checked
	// write on a busy key into a conflict.
	UpdateLastUsed(ctx context.Context, keyID id.KeyID, at time.Time) error

	Delete(ctx context.Context, keyID id.KeyID) error
	List(ctx context.Context, filter *ListFilter) ([]*Key, error)
	Count(ctx context.Context, filter *ListFilter) (int64, error)
	ListExpired(ctx context.Context, before time.Time) ([]*Key, error)
	ListByPolicy(ctx context.Context, policyID id.PolicyID) ([]*Key, error)
	DeleteByTenant(ctx context.Context, tenantID string) error
}
