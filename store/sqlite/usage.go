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
	q := s.sdb.NewSelect(&models).OrderExpr("created_at DESC, id DESC")

	if filter != nil {
		if filter.KeyID != nil {
			q = q.Where("key_id = ?", filter.KeyID.String())
		}
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
		if filter.After != nil {
			q = q.Where("created_at >= ?", dbTime(*filter.After))
		}
		if filter.Before != nil {
			q = q.Where("created_at < ?", dbTime(*filter.Before))
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
	if filter == nil {
		return nil, usage.ErrInvalidPeriod
	}
	// created_at is UTC text, "2006-01-02 15:04:05 +0000 UTC", so a bucket is
	// a prefix of it. The prefix length and parse layout come from this
	// switch and never from the caller's string.
	var prefixLen int
	var layout string
	switch filter.Period {
	case usage.PeriodHourly:
		prefixLen, layout = 13, "2006-01-02 15"
	case usage.PeriodDaily:
		prefixLen, layout = 10, "2006-01-02"
	case usage.PeriodMonthly:
		prefixLen, layout = 7, "2006-01"
	default:
		return nil, usage.ErrInvalidPeriod
	}

	keyExpr := "''"
	if filter.KeyID != nil {
		keyExpr = "key_id"
	}
	args := []any{prefixLen}
	where := "1=1"
	if filter.TenantID != "" {
		where += " AND tenant_id = ?"
		args = append(args, filter.TenantID)
	}
	if filter.KeyID != nil {
		where += " AND key_id = ?"
		args = append(args, filter.KeyID.String())
	}
	if filter.After != nil {
		where += " AND created_at >= ?"
		args = append(args, dbTime(*filter.After))
	}
	if filter.Before != nil {
		where += " AND created_at < ?"
		args = append(args, dbTime(*filter.Before))
	}

	query := fmt.Sprintf(`SELECT substr(created_at, 1, ?) AS bucket, %[1]s AS key_id, tenant_id,
       COUNT(*),
       COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN status_code >= 500 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(latency_ms), 0)
FROM keysmith_usage
WHERE %[2]s
GROUP BY bucket, %[1]s, tenant_id
ORDER BY bucket ASC, tenant_id ASC, key_id ASC`, keyExpr, where)

	rows, err := s.sdb.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: aggregate usage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]*usage.Aggregation, 0)
	for rows.Next() {
		var (
			bucket, keyStr, tenant                  string
			requests, errCount, serverErrs, latency int64
		)
		if err := rows.Scan(&bucket, &keyStr, &tenant, &requests, &errCount, &serverErrs, &latency); err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: scan aggregation: %w", err)
		}
		start, err := time.ParseInLocation(layout, bucket, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: parse bucket %q: %w", bucket, err)
		}
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
				return nil, fmt.Errorf("keysmith/sqlite: parse key id %q: %w", keyStr, err)
			}
			agg.KeyID = kid
		}
		result = append(result, agg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: aggregate usage rows: %w", err)
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
			q = q.Where("created_at >= ?", dbTime(*filter.After))
		}
		if filter.Before != nil {
			q = q.Where("created_at < ?", dbTime(*filter.Before))
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
		Where("created_at < ?", dbTime(before)).
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
		Where("created_at >= ?", dbTime(dayStart)).
		Where("created_at < ?", dbTime(dayEnd))

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
		Where("created_at >= ?", dbTime(monthStart)).
		Where("created_at < ?", dbTime(monthEnd))

	count, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/sqlite: monthly count: %w", err)
	}
	return count, nil
}
