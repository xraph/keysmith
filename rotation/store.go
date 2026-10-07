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

	// GetInGraceByOldHash returns the record whose OldKeyHash equals hash and
	// whose GraceEnds is after now. It returns store.ErrRotationNotFound when
	// none matches. If several match, the one with the latest GraceEnds wins.
	GetInGraceByOldHash(ctx context.Context, hash string, now time.Time) (*Record, error)

	// EndGrace sets GraceEnds to at on every record for the key whose
	// GraceEnds is after at, and returns how many records changed.
	EndGrace(ctx context.Context, keyID id.KeyID, at time.Time) (int64, error)

	// EndGraceByID sets one record's GraceEnds to at when it is after at,
	// the way EndGrace does for every record on a key. Other records on the
	// same key keep their windows. A missing record, or one whose window
	// already ended at or before at, changes nothing and returns nil, just
	// as EndGrace counts it as zero.
	EndGraceByID(ctx context.Context, rotID id.RotationID, at time.Time) error
}
