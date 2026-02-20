package scope

import (
	"context"

	"github.com/xraph/keysmith/id"
)

// Store is the persistence interface for permission scopes.
type Store interface {
	Create(ctx context.Context, s *Scope) error
	Get(ctx context.Context, scopeID id.ScopeID) (*Scope, error)
	GetByName(ctx context.Context, tenantID, name string) (*Scope, error)
	Update(ctx context.Context, s *Scope) error
	Delete(ctx context.Context, scopeID id.ScopeID) error
	List(ctx context.Context, filter *ListFilter) ([]*Scope, error)
	ListByKey(ctx context.Context, keyID id.KeyID) ([]*Scope, error)
	AssignToKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error
	RemoveFromKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error
}
