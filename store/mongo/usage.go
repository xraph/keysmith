package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/grove/drivers/mongodriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/usage"
)

type usageStore struct {
	mdb *mongodriver.MongoDB
}

func (s *usageStore) Record(ctx context.Context, rec *usage.Record) error {
	m := usageToModel(rec)
	_, err := s.mdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: record usage: %w", err)
	}
	return nil
}

func (s *usageStore) RecordBatch(ctx context.Context, recs []*usage.Record) error {
	if len(recs) == 0 {
		return nil
	}

	models := make([]usageModel, len(recs))
	for i, rec := range recs {
		models[i] = *usageToModel(rec)
	}

	_, err := s.mdb.NewInsert(&models).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: record batch usage: %w", err)
	}
	return nil
}

func (s *usageStore) Query(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Record, error) {
	var models []usageModel

	f := bson.M{}
	if filter != nil {
		if filter.KeyID != nil {
			f["key_id"] = filter.KeyID.String()
		}
		if filter.TenantID != "" {
			f["tenant_id"] = filter.TenantID
		}
		if filter.After != nil || filter.Before != nil {
			dateFilter := bson.M{}
			if filter.After != nil {
				dateFilter["$gte"] = *filter.After
			}
			if filter.Before != nil {
				dateFilter["$lt"] = *filter.Before
			}
			f["created_at"] = dateFilter
		}
	}

	q := s.mdb.NewFind(&models).
		Filter(f).
		Sort(bson.D{{Key: "created_at", Value: -1}})

	if filter != nil {
		if filter.Limit > 0 {
			q = q.Limit(int64(filter.Limit))
		}
		if filter.Offset > 0 {
			q = q.Skip(int64(filter.Offset))
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/mongo: query usage: %w", err)
	}

	result := make([]*usage.Record, 0, len(models))
	for i := range models {
		rec, err := usageFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert usage: %w", err)
		}
		result = append(result, rec)
	}
	return result, nil
}

func (s *usageStore) Aggregate(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Aggregation, error) {
	var models []usageAggModel

	f := bson.M{}
	if filter != nil {
		if filter.KeyID != nil {
			f["key_id"] = filter.KeyID.String()
		}
		if filter.TenantID != "" {
			f["tenant_id"] = filter.TenantID
		}
		if filter.Period != "" {
			f["period"] = filter.Period
		}
		if filter.After != nil || filter.Before != nil {
			dateFilter := bson.M{}
			if filter.After != nil {
				dateFilter["$gte"] = *filter.After
			}
			if filter.Before != nil {
				dateFilter["$lt"] = *filter.Before
			}
			f["period_start"] = dateFilter
		}
	}

	q := s.mdb.NewFind(&models).
		Filter(f).
		Sort(bson.D{{Key: "period_start", Value: -1}})

	if filter != nil {
		if filter.Limit > 0 {
			q = q.Limit(int64(filter.Limit))
		}
		if filter.Offset > 0 {
			q = q.Skip(int64(filter.Offset))
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/mongo: aggregate usage: %w", err)
	}

	result := make([]*usage.Aggregation, 0, len(models))
	for i := range models {
		agg, err := aggFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert aggregation: %w", err)
		}
		result = append(result, agg)
	}
	return result, nil
}

func (s *usageStore) Count(ctx context.Context, filter *usage.QueryFilter) (int64, error) {
	f := bson.M{}
	if filter != nil {
		if filter.KeyID != nil {
			f["key_id"] = filter.KeyID.String()
		}
		if filter.TenantID != "" {
			f["tenant_id"] = filter.TenantID
		}
		if filter.After != nil || filter.Before != nil {
			dateFilter := bson.M{}
			if filter.After != nil {
				dateFilter["$gte"] = *filter.After
			}
			if filter.Before != nil {
				dateFilter["$lt"] = *filter.Before
			}
			f["created_at"] = dateFilter
		}
	}

	count, err := s.mdb.NewFind((*usageModel)(nil)).
		Filter(f).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/mongo: count usage: %w", err)
	}
	return count, nil
}

func (s *usageStore) Purge(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.mdb.NewDelete((*usageModel)(nil)).
		Many().
		Filter(bson.M{"created_at": bson.M{"$lt": before}}).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/mongo: purge usage: %w", err)
	}
	return res.DeletedCount(), nil
}

func (s *usageStore) DailyCount(ctx context.Context, keyID id.KeyID, date time.Time) (int64, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	count, err := s.mdb.NewFind((*usageModel)(nil)).
		Filter(bson.M{
			"key_id":     keyID.String(),
			"created_at": bson.M{"$gte": dayStart, "$lt": dayEnd},
		}).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/mongo: daily count: %w", err)
	}
	return count, nil
}

func (s *usageStore) MonthlyCount(ctx context.Context, keyID id.KeyID, month time.Time) (int64, error) {
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)

	count, err := s.mdb.NewFind((*usageModel)(nil)).
		Filter(bson.M{
			"key_id":     keyID.String(),
			"created_at": bson.M{"$gte": monthStart, "$lt": monthEnd},
		}).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/mongo: monthly count: %w", err)
	}
	return count, nil
}
