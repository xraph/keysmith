package rotation

import (
	"context"
	"time"

	"github.com/xraph/keysmith/id"
)

// Store is the persistence interface for key rotation records.
type Store interface {
	Create(ctx context.Context, rec *Record) error
	Get(ctx context.Context, rotID id.RotationID) (*Record, error)
	List(ctx context.Context, filter *ListFilter) ([]*Record, error)
	ListPendingGrace(ctx context.Context, now time.Time) ([]*Record, error)
	LatestForKey(ctx context.Context, keyID id.KeyID) (*Record, error)
}
