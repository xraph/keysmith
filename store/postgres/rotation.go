package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/rotation"
)

type rotationStore struct {
	pool *pgxpool.Pool
}

func (s *rotationStore) Create(ctx context.Context, rec *rotation.Record) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO keysmith_rotations (
			id, key_id, tenant_id, old_key_hash, new_key_hash,
			reason, grace_ttl_ms, grace_ends, rotated_by, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		rec.ID.String(), rec.KeyID.String(), rec.TenantID,
		rec.OldKeyHash, rec.NewKeyHash,
		string(rec.Reason), rec.GraceTTL.Milliseconds(),
		rec.GraceEnds, rec.RotatedBy, rec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: create rotation: %w", err)
	}
	return nil
}

func (s *rotationStore) Get(ctx context.Context, rotID id.RotationID) (*rotation.Record, error) {
	return s.scanRecord(s.pool.QueryRow(ctx, `
		SELECT id, key_id, tenant_id, old_key_hash, new_key_hash,
		       reason, grace_ttl_ms, grace_ends, rotated_by, created_at
		FROM keysmith_rotations WHERE id = $1`, rotID.String()))
}

func (s *rotationStore) List(ctx context.Context, filter *rotation.ListFilter) ([]*rotation.Record, error) {
	where, args := buildRotationWhere(filter)
	query := `
		SELECT id, key_id, tenant_id, old_key_hash, new_key_hash,
		       reason, grace_ttl_ms, grace_ends, rotated_by, created_at
		FROM keysmith_rotations` + where + ` ORDER BY created_at DESC`

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
		return nil, fmt.Errorf("keysmith/postgres: list rotations: %w", err)
	}
	defer rows.Close()

	return s.scanRecords(rows)
}

func (s *rotationStore) ListPendingGrace(ctx context.Context, now time.Time) ([]*rotation.Record, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, key_id, tenant_id, old_key_hash, new_key_hash,
		       reason, grace_ttl_ms, grace_ends, rotated_by, created_at
		FROM keysmith_rotations
		WHERE grace_ends > $1
		ORDER BY grace_ends ASC`, now)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: list pending grace: %w", err)
	}
	defer rows.Close()

	return s.scanRecords(rows)
}

func (s *rotationStore) LatestForKey(ctx context.Context, keyID id.KeyID) (*rotation.Record, error) {
	return s.scanRecord(s.pool.QueryRow(ctx, `
		SELECT id, key_id, tenant_id, old_key_hash, new_key_hash,
		       reason, grace_ttl_ms, grace_ends, rotated_by, created_at
		FROM keysmith_rotations
		WHERE key_id = $1
		ORDER BY created_at DESC
		LIMIT 1`, keyID.String()))
}

// ── scan helpers ─────────────────────────────────────

func (s *rotationStore) scanRecord(row pgx.Row) (*rotation.Record, error) {
	var (
		rec        rotation.Record
		idStr      string
		keyIDStr   string
		reason     string
		graceTTLMs int64
	)

	err := row.Scan(
		&idStr, &keyIDStr, &rec.TenantID,
		&rec.OldKeyHash, &rec.NewKeyHash,
		&reason, &graceTTLMs, &rec.GraceEnds,
		&rec.RotatedBy, &rec.CreatedAt,
	)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("rotation")
		}
		return nil, fmt.Errorf("keysmith/postgres: scan rotation: %w", err)
	}

	rec.ID, err = id.ParseRotationID(idStr)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: parse rotation id: %w", err)
	}

	rec.KeyID, err = id.ParseKeyID(keyIDStr)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: parse key id: %w", err)
	}

	rec.Reason = rotation.Reason(reason)
	rec.GraceTTL = time.Duration(graceTTLMs) * time.Millisecond

	return &rec, nil
}

func (s *rotationStore) scanRecords(rows pgx.Rows) ([]*rotation.Record, error) {
	var result []*rotation.Record
	for rows.Next() {
		var (
			rec        rotation.Record
			idStr      string
			keyIDStr   string
			reason     string
			graceTTLMs int64
		)

		err := rows.Scan(
			&idStr, &keyIDStr, &rec.TenantID,
			&rec.OldKeyHash, &rec.NewKeyHash,
			&reason, &graceTTLMs, &rec.GraceEnds,
			&rec.RotatedBy, &rec.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: scan rotation: %w", err)
		}

		rec.ID, err = id.ParseRotationID(idStr)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: parse rotation id: %w", err)
		}

		rec.KeyID, err = id.ParseKeyID(keyIDStr)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: parse key id: %w", err)
		}

		rec.Reason = rotation.Reason(reason)
		rec.GraceTTL = time.Duration(graceTTLMs) * time.Millisecond

		result = append(result, &rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: rows: %w", err)
	}
	return result, nil
}

func buildRotationWhere(f *rotation.ListFilter) (where string, args []any) {
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
	if f.Reason != "" {
		args = append(args, string(f.Reason))
		clauses = append(clauses, fmt.Sprintf("reason = $%d", len(args)))
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
