package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/grove/driver"
	"github.com/xraph/grove/drivers/pgdriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/usage"
)

type usageStore struct {
	db *pgdriver.PgDB
}

func (s *usageStore) Record(ctx context.Context, rec *usage.Record) error {
	m := usageToModel(rec)
	_, err := s.db.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: record usage: %w", err)
	}
	return nil
}

func (s *usageStore) RecordBatch(ctx context.Context, recs []*usage.Record) error {
	if len(recs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTxQuery(ctx, &driver.TxOptions{})
	if err != nil {
		return fmt.Errorf("keysmith/postgres: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, rec := range recs {
		m := usageToModel(rec)
		_, err := tx.NewInsert(m).Exec(ctx)
		if err != nil {
			return fmt.Errorf("keysmith/postgres: record batch usage: %w", err)
		}
	}

	return tx.Commit()
}

func (s *usageStore) Query(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Record, error) {
	var models []usageModel
	q := s.db.NewSelect(&models).OrderExpr("created_at DESC")

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
		return nil, fmt.Errorf("keysmith/postgres: query usage: %w", err)
	}

	result := make([]*usage.Record, 0, len(models))
	for i := range models {
		rec, err := usageFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: convert usage: %w", err)
		}
		result = append(result, rec)
	}
	return result, nil
}

func (s *usageStore) Aggregate(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Aggregation, error) {
	if filter == nil {
		return nil, usage.ErrInvalidPeriod
	}
	// The unit comes from this switch and is still bound as a parameter.
	var unit string
	switch filter.Period {
	case usage.PeriodHourly:
		unit = "hour"
	case usage.PeriodDaily:
		unit = "day"
	case usage.PeriodMonthly:
		unit = "month"
	default:
		return nil, usage.ErrInvalidPeriod
	}

	// Postgres rejects a constant in GROUP BY, so the cross-key query selects
	// an empty key and leaves it out of the grouping.
	keyExpr, groupKey := "''", ""
	if filter.KeyID != nil {
		keyExpr, groupKey = "key_id", "key_id, "
	}
	args := []any{unit}
	where := "TRUE"
	if filter.TenantID != "" {
		args = append(args, filter.TenantID)
		where += fmt.Sprintf(" AND tenant_id = $%d", len(args))
	}
	if filter.KeyID != nil {
		args = append(args, filter.KeyID.String())
		where += fmt.Sprintf(" AND key_id = $%d", len(args))
	}
	if filter.After != nil {
		args = append(args, filter.After.UTC())
		where += fmt.Sprintf(" AND created_at >= $%d", len(args))
	}
	if filter.Before != nil {
		args = append(args, filter.Before.UTC())
		where += fmt.Sprintf(" AND created_at < $%d", len(args))
	}

	// date_trunc on created_at AT TIME ZONE 'UTC' buckets on UTC wall-clock
	// time and returns a timestamp without time zone.
	query := fmt.Sprintf(`SELECT date_trunc($1, created_at AT TIME ZONE 'UTC') AS bucket, %[1]s AS key_id, tenant_id,
       COUNT(*),
       COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(latency_ms), 0)
FROM keysmith_usage
WHERE %[2]s
GROUP BY bucket, %[3]stenant_id
ORDER BY bucket ASC, tenant_id ASC, key_id ASC`, keyExpr, where, groupKey)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: aggregate usage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]*usage.Aggregation, 0)
	for rows.Next() {
		var (
			bucket                                  time.Time
			keyStr, tenant                          string
			requests, errCount, serverErrs, latency int64
		)
		if err := rows.Scan(&bucket, &keyStr, &tenant, &requests, &errCount, &serverErrs, &latency); err != nil {
			return nil, fmt.Errorf("keysmith/postgres: scan aggregation: %w", err)
		}
		// The column has no zone, so read its fields as UTC whatever
		// location the driver attached.
		start := time.Date(bucket.Year(), bucket.Month(), bucket.Day(), bucket.Hour(), bucket.Minute(), bucket.Second(), 0, time.UTC)
		agg := &usage.Aggregation{
			TenantID:         tenant,
			Period:           filter.Period,
			PeriodStart:      start,
			RequestCount:     requests,
			ErrorCount:       errCount,
			ServerErrorCount: serverErrs,
			TotalLatency:     latency,
		}
		if keyStr != "" {
			kid, err := id.ParseKeyID(keyStr)
			if err != nil {
				return nil, fmt.Errorf("keysmith/postgres: parse key id %q: %w", keyStr, err)
			}
			agg.KeyID = kid
		}
		result = append(result, agg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: aggregate usage rows: %w", err)
	}
	return result, nil
}

func (s *usageStore) Count(ctx context.Context, filter *usage.QueryFilter) (int64, error) {
	q := s.db.NewSelect((*usageModel)(nil))

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
		return 0, fmt.Errorf("keysmith/postgres: count usage: %w", err)
	}
	return count, nil
}

func (s *usageStore) Purge(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.NewDelete((*usageModel)(nil)).
		Where("created_at < ?", before).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: purge usage: %w", err)
	}
	affected, _ := res.RowsAffected()
	return affected, nil
}

func (s *usageStore) DailyCount(ctx context.Context, keyID id.KeyID, date time.Time) (int64, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	q := s.db.NewSelect((*usageModel)(nil)).
		Where("key_id = ?", keyID.String()).
		Where("created_at >= ?", dayStart).
		Where("created_at < ?", dayEnd)

	count, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: daily count: %w", err)
	}
	return count, nil
}

func (s *usageStore) MonthlyCount(ctx context.Context, keyID id.KeyID, month time.Time) (int64, error) {
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)

	q := s.db.NewSelect((*usageModel)(nil)).
		Where("key_id = ?", keyID.String()).
		Where("created_at >= ?", monthStart).
		Where("created_at < ?", monthEnd)

	count, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: monthly count: %w", err)
	}
	return count, nil
}
