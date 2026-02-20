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
	Update(ctx context.Context, key *Key) error
	UpdateState(ctx context.Context, keyID id.KeyID, state State) error
	UpdateLastUsed(ctx context.Context, keyID id.KeyID, at time.Time) error
	Delete(ctx context.Context, keyID id.KeyID) error
	List(ctx context.Context, filter *ListFilter) ([]*Key, error)
	Count(ctx context.Context, filter *ListFilter) (int64, error)
	ListExpired(ctx context.Context, before time.Time) ([]*Key, error)
	ListByPolicy(ctx context.Context, policyID id.PolicyID) ([]*Key, error)
	DeleteByTenant(ctx context.Context, tenantID string) error
}
