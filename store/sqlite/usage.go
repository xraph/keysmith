package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/grove/driver"
	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/usage"
)

type usageStore struct {
	sdb *sqlitedriver.SqliteDB
}

func (s *usageStore) Record(ctx context.Context, rec *usage.Record) error {
	m := usageToModel(rec)
	_, err := s.sdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: record usage: %w", err)
	}
	return nil
}

func (s *usageStore) RecordBatch(ctx context.Context, recs []*usage.Record) error {
	if len(recs) == 0 {
		return nil
	}

	tx, err := s.sdb.BeginTxQuery(ctx, &driver.TxOptions{})
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, rec := range recs {
		m := usageToModel(rec)
		_, err := tx.NewInsert(m).Exec(ctx)
		if err != nil {
			return fmt.Errorf("keysmith/sqlite: record batch usage: %w", err)
		}
	}

	return tx.Commit()
}

func (s *usageStore) Query(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Record, error) {
	var models []usageModel
	q := s.sdb.NewSelect(&models).OrderExpr("created_at DESC")

	if filter != nil {
		if filter.KeyID != nil {
			q = q.Where("key_id = ?", filter.KeyID.String())
		}
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
		if filter.After != nil {
			q = q.Where("created_at >= ?", *filter.After)
		}
		if filter.Before != nil {
			q = q.Where("created_at < ?", *filter.Before)
		}
		if filter.Limit > 0 {
			q = q.Limit(filter.Limit)
		}
		if filter.Offset > 0 {
			q = q.Offset(filter.Offset)
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: query usage: %w", err)
	}

	result := make([]*usage.Record, 0, len(models))
	for i := range models {
		rec, err := usageFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: convert usage: %w", err)
		}
		result = append(result, rec)
	}
	return result, nil
}

func (s *usageStore) Aggregate(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Aggregation, error) {
	var models []usageAggModel
	q := s.sdb.NewSelect(&models).OrderExpr("period_start DESC")

	if filter != nil {
		if filter.KeyID != nil {
			q = q.Where("key_id = ?", filter.KeyID.String())
		}
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
		if filter.Period != "" {
			q = q.Where("period = ?", filter.Period)
		}
		if filter.After != nil {
			q = q.Where("period_start >= ?", *filter.After)
		}
		if filter.Before != nil {
			q = q.Where("period_start < ?", *filter.Before)
		}
		if filter.Limit > 0 {
			q = q.Limit(filter.Limit)
		}
		if filter.Offset > 0 {
			q = q.Offset(filter.Offset)
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: aggregate usage: %w", err)
	}

	result := make([]*usage.Aggregation, 0, len(models))
	for i := range models {
		agg, err := aggFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: convert aggregation: %w", err)
		}
		result = append(result, agg)
	}
	return result, nil
}

func (s *usageStore) Count(ctx context.Context, filter *usage.QueryFilter) (int64, error) {
	q := s.sdb.NewSelect((*usageModel)(nil))

	if filter != nil {
		if filter.KeyID != nil {
			q = q.Where("key_id = ?", filter.KeyID.String())
		}
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
		if filter.After != nil {
			q = q.Where("created_at >= ?", *filter.After)
		}
		if filter.Before != nil {
			q = q.Where("created_at < ?", *filter.Before)
		}
	}

	count, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/sqlite: count usage: %w", err)
	}
	return count, nil
}

func (s *usageStore) Purge(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.sdb.NewDelete((*usageModel)(nil)).
		Where("created_at < ?", before).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/sqlite: purge usage: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("keysmith/sqlite: purge usage rows: %w", err)
	}
	return rows, nil
}

func (s *usageStore) DailyCount(ctx context.Context, keyID id.KeyID, date time.Time) (int64, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	q := s.sdb.NewSelect((*usageModel)(nil)).
		Where("key_id = ?", keyID.String()).
		Where("created_at >= ?", dayStart).
		Where("created_at < ?", dayEnd)

	count, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/sqlite: daily count: %w", err)
	}
	return count, nil
}

func (s *usageStore) MonthlyCount(ctx context.Context, keyID id.KeyID, month time.Time) (int64, error) {
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)

	q := s.sdb.NewSelect((*usageModel)(nil)).
		Where("key_id = ?", keyID.String()).
		Where("created_at >= ?", monthStart).
		Where("created_at < ?", monthEnd)

	count, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/sqlite: monthly count: %w", err)
	}
	return count, nil
}
