package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/usage"
)

type usageStore struct {
	pool *pgxpool.Pool
}

func (s *usageStore) Record(ctx context.Context, rec *usage.Record) error {
	meta, err := json.Marshal(rec.Metadata)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: marshal metadata: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO keysmith_usage (
			id, key_id, tenant_id, endpoint, method, status_code,
			ip_address, user_agent, latency_ms, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		rec.ID.String(), rec.KeyID.String(), rec.TenantID,
		rec.Endpoint, rec.Method, rec.StatusCode,
		rec.IPAddress, rec.UserAgent, rec.Latency.Milliseconds(),
		meta, rec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: record usage: %w", err)
	}
	return nil
}

func (s *usageStore) RecordBatch(ctx context.Context, recs []*usage.Record) error {
	if len(recs) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, rec := range recs {
		meta, err := json.Marshal(rec.Metadata)
		if err != nil {
			return fmt.Errorf("keysmith/postgres: marshal metadata: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO keysmith_usage (
				id, key_id, tenant_id, endpoint, method, status_code,
				ip_address, user_agent, latency_ms, metadata, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			rec.ID.String(), rec.KeyID.String(), rec.TenantID,
			rec.Endpoint, rec.Method, rec.StatusCode,
			rec.IPAddress, rec.UserAgent, rec.Latency.Milliseconds(),
			meta, rec.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("keysmith/postgres: record batch usage: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (s *usageStore) Query(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Record, error) {
	where, args := buildUsageWhere(filter)
	query := `
		SELECT id, key_id, tenant_id, endpoint, method, status_code,
		       ip_address, user_agent, latency_ms, metadata, created_at
		FROM keysmith_usage` + where + ` ORDER BY created_at DESC`

	if filter != nil {
		if filter.Limit > 0 {
			args = append(args, filter.Limit)
			query += fmt.Sprintf(" LIMIT $%d", len(args))
		}
		if filter.Offset > 0 {
			args = append(args, filter.Offset)
			query += fmt.Sprintf(" OFFSET $%d", len(args))
		}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: query usage: %w", err)
	}
	defer rows.Close()

	return s.scanRecords(rows)
}

func (s *usageStore) Aggregate(ctx context.Context, filter *usage.QueryFilter) ([]*usage.Aggregation, error) {
	where, args := buildUsageAggWhere(filter)
	query := `
		SELECT key_id, tenant_id, period, period_start,
		       request_count, error_count, total_latency, p50_latency, p99_latency
		FROM keysmith_usage_agg` + where + ` ORDER BY period_start DESC`

	if filter != nil {
		if filter.Limit > 0 {
			args = append(args, filter.Limit)
			query += fmt.Sprintf(" LIMIT $%d", len(args))
		}
		if filter.Offset > 0 {
			args = append(args, filter.Offset)
			query += fmt.Sprintf(" OFFSET $%d", len(args))
		}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: aggregate usage: %w", err)
	}
	defer rows.Close()

	var result []*usage.Aggregation
	for rows.Next() {
		var (
			agg      usage.Aggregation
			keyIDStr string
		)

		err := rows.Scan(
			&keyIDStr, &agg.TenantID, &agg.Period, &agg.PeriodStart,
			&agg.RequestCount, &agg.ErrorCount, &agg.TotalLatency,
			&agg.P50Latency, &agg.P99Latency,
		)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: scan aggregation: %w", err)
		}

		agg.KeyID, err = id.ParseKeyID(keyIDStr)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: parse key id: %w", err)
		}

		result = append(result, &agg)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: rows: %w", err)
	}
	return result, nil
}

func (s *usageStore) Count(ctx context.Context, filter *usage.QueryFilter) (int64, error) {
	where, args := buildUsageWhere(filter)
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM keysmith_usage`+where, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: count usage: %w", err)
	}
	return count, nil
}

func (s *usageStore) Purge(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM keysmith_usage WHERE created_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: purge usage: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *usageStore) DailyCount(ctx context.Context, keyID id.KeyID, date time.Time) (int64, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	var count int64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM keysmith_usage
		WHERE key_id = $1 AND created_at >= $2 AND created_at < $3`,
		keyID.String(), dayStart, dayEnd).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: daily count: %w", err)
	}
	return count, nil
}

func (s *usageStore) MonthlyCount(ctx context.Context, keyID id.KeyID, month time.Time) (int64, error) {
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)

	var count int64
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM keysmith_usage
		WHERE key_id = $1 AND created_at >= $2 AND created_at < $3`,
		keyID.String(), monthStart, monthEnd).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: monthly count: %w", err)
	}
	return count, nil
}

// ── scan helpers ─────────────────────────────────────

func (s *usageStore) scanRecords(rows pgx.Rows) ([]*usage.Record, error) {
	var result []*usage.Record
	for rows.Next() {
		var (
			rec       usage.Record
			idStr     string
			keyIDStr  string
			latencyMs int64
			metaJSON  []byte
		)

		err := rows.Scan(
			&idStr, &keyIDStr, &rec.TenantID,
			&rec.Endpoint, &rec.Method, &rec.StatusCode,
			&rec.IPAddress, &rec.UserAgent, &latencyMs,
			&metaJSON, &rec.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: scan usage: %w", err)
		}

		rec.ID, err = id.ParseUsageID(idStr)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: parse usage id: %w", err)
		}

		rec.KeyID, err = id.ParseKeyID(keyIDStr)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: parse key id: %w", err)
		}

		rec.Latency = time.Duration(latencyMs) * time.Millisecond

		if len(metaJSON) > 0 {
			if err := json.Unmarshal(metaJSON, &rec.Metadata); err != nil {
				return nil, fmt.Errorf("keysmith/postgres: unmarshal metadata: %w", err)
			}
		}

		result = append(result, &rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: rows: %w", err)
	}
	return result, nil
}

// ── filter helpers ───────────────────────────────────

func buildUsageWhere(f *usage.QueryFilter) (where string, args []any) {
	if f == nil {
		return "", nil
	}

	var clauses []string

	if f.KeyID != nil {
		args = append(args, f.KeyID.String())
		clauses = append(clauses, fmt.Sprintf("key_id = $%d", len(args)))
	}
	if f.TenantID != "" {
		args = append(args, f.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if f.After != nil {
		args = append(args, *f.After)
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", len(args)))
	}
	if f.Before != nil {
		args = append(args, *f.Before)
		clauses = append(clauses, fmt.Sprintf("created_at < $%d", len(args)))
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func buildUsageAggWhere(f *usage.QueryFilter) (where string, args []any) {
	if f == nil {
		return "", nil
	}

	var clauses []string

	if f.KeyID != nil {
		args = append(args, f.KeyID.String())
		clauses = append(clauses, fmt.Sprintf("key_id = $%d", len(args)))
	}
	if f.TenantID != "" {
		args = append(args, f.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if f.Period != "" {
		args = append(args, f.Period)
		clauses = append(clauses, fmt.Sprintf("period = $%d", len(args)))
	}
	if f.After != nil {
		args = append(args, *f.After)
		clauses = append(clauses, fmt.Sprintf("period_start >= $%d", len(args)))
	}
	if f.Before != nil {
		args = append(args, *f.Before)
		clauses = append(clauses, fmt.Sprintf("period_start < $%d", len(args)))
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
