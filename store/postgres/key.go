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
	"github.com/xraph/keysmith/key"
)

type keyStore struct {
	pool *pgxpool.Pool
}

func (s *keyStore) Create(ctx context.Context, k *key.Key) error {
	meta, err := json.Marshal(k.Metadata)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: marshal metadata: %w", err)
	}

	var policyID *string
	if k.PolicyID != nil {
		s := k.PolicyID.String()
		policyID = &s
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO keysmith_keys (
			id, tenant_id, app_id, name, description, prefix, hint, key_hash,
			environment, state, policy_id, metadata, created_by,
			expires_at, last_used_at, rotated_at, revoked_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19
		)`,
		k.ID.String(), k.TenantID, k.AppID, k.Name, k.Description,
		k.Prefix, k.Hint, k.KeyHash,
		string(k.Environment), string(k.State), policyID, meta, k.CreatedBy,
		k.ExpiresAt, k.LastUsedAt, k.RotatedAt, k.RevokedAt, k.CreatedAt, k.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: create key: %w", err)
	}
	return nil
}

func (s *keyStore) Get(ctx context.Context, keyID id.KeyID) (*key.Key, error) {
	return s.scanKey(s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, app_id, name, description, prefix, hint, key_hash,
		       environment, state, policy_id, metadata, created_by,
		       expires_at, last_used_at, rotated_at, revoked_at, created_at, updated_at
		FROM keysmith_keys WHERE id = $1`, keyID.String()))
}

func (s *keyStore) GetByHash(ctx context.Context, hash string) (*key.Key, error) {
	return s.scanKey(s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, app_id, name, description, prefix, hint, key_hash,
		       environment, state, policy_id, metadata, created_by,
		       expires_at, last_used_at, rotated_at, revoked_at, created_at, updated_at
		FROM keysmith_keys WHERE key_hash = $1`, hash))
}

func (s *keyStore) GetByPrefix(ctx context.Context, prefix, hint string) (*key.Key, error) {
	return s.scanKey(s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, app_id, name, description, prefix, hint, key_hash,
		       environment, state, policy_id, metadata, created_by,
		       expires_at, last_used_at, rotated_at, revoked_at, created_at, updated_at
		FROM keysmith_keys WHERE prefix = $1 AND hint = $2`, prefix, hint))
}

func (s *keyStore) Update(ctx context.Context, k *key.Key) error {
	meta, err := json.Marshal(k.Metadata)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: marshal metadata: %w", err)
	}

	var policyID *string
	if k.PolicyID != nil {
		s := k.PolicyID.String()
		policyID = &s
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE keysmith_keys SET
			name = $2, description = $3, prefix = $4, hint = $5, key_hash = $6,
			environment = $7, state = $8, policy_id = $9, metadata = $10, created_by = $11,
			expires_at = $12, last_used_at = $13, rotated_at = $14, revoked_at = $15,
			updated_at = $16
		WHERE id = $1`,
		k.ID.String(), k.Name, k.Description, k.Prefix, k.Hint, k.KeyHash,
		string(k.Environment), string(k.State), policyID, meta, k.CreatedBy,
		k.ExpiresAt, k.LastUsedAt, k.RotatedAt, k.RevokedAt, k.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) UpdateState(ctx context.Context, keyID id.KeyID, state key.State) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE keysmith_keys SET state = $2, updated_at = NOW() WHERE id = $1`,
		keyID.String(), string(state))
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update key state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) UpdateLastUsed(ctx context.Context, keyID id.KeyID, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE keysmith_keys SET last_used_at = $2 WHERE id = $1`,
		keyID.String(), at)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update last used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) Delete(ctx context.Context, keyID id.KeyID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM keysmith_keys WHERE id = $1`, keyID.String())
	if err != nil {
		return fmt.Errorf("keysmith/postgres: delete key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) List(ctx context.Context, filter *key.ListFilter) ([]*key.Key, error) {
	where, args := buildKeyWhere(filter)
	query := `
		SELECT id, tenant_id, app_id, name, description, prefix, hint, key_hash,
		       environment, state, policy_id, metadata, created_by,
		       expires_at, last_used_at, rotated_at, revoked_at, created_at, updated_at
		FROM keysmith_keys` + where + ` ORDER BY created_at DESC`

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
		return nil, fmt.Errorf("keysmith/postgres: list keys: %w", err)
	}
	defer rows.Close()

	return s.scanKeys(rows)
}

func (s *keyStore) Count(ctx context.Context, filter *key.ListFilter) (int64, error) {
	where, args := buildKeyWhere(filter)
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM keysmith_keys`+where, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: count keys: %w", err)
	}
	return count, nil
}

func (s *keyStore) ListExpired(ctx context.Context, before time.Time) ([]*key.Key, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, app_id, name, description, prefix, hint, key_hash,
		       environment, state, policy_id, metadata, created_by,
		       expires_at, last_used_at, rotated_at, revoked_at, created_at, updated_at
		FROM keysmith_keys
		WHERE state = 'active' AND expires_at IS NOT NULL AND expires_at < $1`, before)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: list expired: %w", err)
	}
	defer rows.Close()

	return s.scanKeys(rows)
}

func (s *keyStore) ListByPolicy(ctx context.Context, policyID id.PolicyID) ([]*key.Key, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, app_id, name, description, prefix, hint, key_hash,
		       environment, state, policy_id, metadata, created_by,
		       expires_at, last_used_at, rotated_at, revoked_at, created_at, updated_at
		FROM keysmith_keys WHERE policy_id = $1`, policyID.String())
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: list by policy: %w", err)
	}
	defer rows.Close()

	return s.scanKeys(rows)
}

func (s *keyStore) DeleteByTenant(ctx context.Context, tenantID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM keysmith_keys WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: delete by tenant: %w", err)
	}
	return nil
}

// ── scan helpers ─────────────────────────────────────

func (s *keyStore) scanKey(row pgx.Row) (*key.Key, error) {
	var (
		k        key.Key
		idStr    string
		env      string
		state    string
		polID    *string
		metaJSON []byte
	)

	err := row.Scan(
		&idStr, &k.TenantID, &k.AppID, &k.Name, &k.Description,
		&k.Prefix, &k.Hint, &k.KeyHash,
		&env, &state, &polID, &metaJSON, &k.CreatedBy,
		&k.ExpiresAt, &k.LastUsedAt, &k.RotatedAt, &k.RevokedAt,
		&k.CreatedAt, &k.UpdatedAt,
	)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/postgres: scan key: %w", err)
	}

	k.ID, err = id.ParseKeyID(idStr)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: parse key id: %w", err)
	}
	k.Environment = key.Environment(env)
	k.State = key.State(state)

	if polID != nil {
		pid, err := id.ParsePolicyID(*polID)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: parse policy id: %w", err)
		}
		k.PolicyID = &pid
	}

	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &k.Metadata); err != nil {
			return nil, fmt.Errorf("keysmith/postgres: unmarshal metadata: %w", err)
		}
	}

	return &k, nil
}

func (s *keyStore) scanKeys(rows pgx.Rows) ([]*key.Key, error) {
	var result []*key.Key
	for rows.Next() {
		var (
			k        key.Key
			idStr    string
			env      string
			state    string
			polID    *string
			metaJSON []byte
		)

		err := rows.Scan(
			&idStr, &k.TenantID, &k.AppID, &k.Name, &k.Description,
			&k.Prefix, &k.Hint, &k.KeyHash,
			&env, &state, &polID, &metaJSON, &k.CreatedBy,
			&k.ExpiresAt, &k.LastUsedAt, &k.RotatedAt, &k.RevokedAt,
			&k.CreatedAt, &k.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: scan key: %w", err)
		}

		k.ID, err = id.ParseKeyID(idStr)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: parse key id: %w", err)
		}
		k.Environment = key.Environment(env)
		k.State = key.State(state)

		if polID != nil {
			pid, err := id.ParsePolicyID(*polID)
			if err != nil {
				return nil, fmt.Errorf("keysmith/postgres: parse policy id: %w", err)
			}
			k.PolicyID = &pid
		}

		if len(metaJSON) > 0 {
			if err := json.Unmarshal(metaJSON, &k.Metadata); err != nil {
				return nil, fmt.Errorf("keysmith/postgres: unmarshal metadata: %w", err)
			}
		}

		result = append(result, &k)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: rows: %w", err)
	}
	return result, nil
}

// ── filter helpers ───────────────────────────────────

func buildKeyWhere(f *key.ListFilter) (where string, args []any) {
	if f == nil {
		return "", nil
	}

	var clauses []string

	if f.TenantID != "" {
		args = append(args, f.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if f.Environment != "" {
		args = append(args, string(f.Environment))
		clauses = append(clauses, fmt.Sprintf("environment = $%d", len(args)))
	}
	if f.State != "" {
		args = append(args, string(f.State))
		clauses = append(clauses, fmt.Sprintf("state = $%d", len(args)))
	}
	if f.PolicyID != nil {
		args = append(args, f.PolicyID.String())
		clauses = append(clauses, fmt.Sprintf("policy_id = $%d", len(args)))
	}
	if f.CreatedBy != "" {
		args = append(args, f.CreatedBy)
		clauses = append(clauses, fmt.Sprintf("created_by = $%d", len(args)))
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
