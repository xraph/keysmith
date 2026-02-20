package usage

import (
	"context"
	"time"

	"github.com/xraph/keysmith/id"
)

// Store is the persistence interface for key usage tracking.
type Store interface {
	Record(ctx context.Context, rec *Record) error
	RecordBatch(ctx context.Context, recs []*Record) error
	Query(ctx context.Context, filter *QueryFilter) ([]*Record, error)
	Aggregate(ctx context.Context, filter *QueryFilter) ([]*Aggregation, error)
	Count(ctx context.Context, filter *QueryFilter) (int64, error)
	Purge(ctx context.Context, before time.Time) (int64, error)
	DailyCount(ctx context.Context, keyID id.KeyID, date time.Time) (int64, error)
	MonthlyCount(ctx context.Context, keyID id.KeyID, month time.Time) (int64, error)
}
