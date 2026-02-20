package policy

import (
	"context"

	"github.com/xraph/keysmith/id"
)

// Store is the persistence interface for key policies.
type Store interface {
	Create(ctx context.Context, pol *Policy) error
	Get(ctx context.Context, polID id.PolicyID) (*Policy, error)
	GetByName(ctx context.Context, tenantID, name string) (*Policy, error)
	Update(ctx context.Context, pol *Policy) error
	Delete(ctx context.Context, polID id.PolicyID) error
	List(ctx context.Context, filter *ListFilter) ([]*Policy, error)
	Count(ctx context.Context, filter *ListFilter) (int64, error)
}
