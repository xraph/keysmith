package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/policy"
)

type policyStore struct {
	pool *pgxpool.Pool
}

func (s *policyStore) Create(ctx context.Context, pol *policy.Policy) error {
	meta, err := json.Marshal(pol.Metadata)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: marshal metadata: %w", err)
	}

	scopesJSON, _ := json.Marshal(pol.AllowedScopes)
	ipsJSON, _ := json.Marshal(pol.AllowedIPs)
	originsJSON, _ := json.Marshal(pol.AllowedOrigins)
	methodsJSON, _ := json.Marshal(pol.AllowedMethods)
	pathsJSON, _ := json.Marshal(pol.AllowedPaths)

	_, err = s.pool.Exec(ctx, `
		INSERT INTO keysmith_policies (
			id, tenant_id, app_id, name, description,
			rate_limit, rate_limit_window, burst_limit,
			allowed_scopes, allowed_ips, allowed_origins, allowed_methods, allowed_paths,
			max_key_lifetime, rotation_period, grace_period,
			daily_quota, monthly_quota, metadata, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11, $12, $13,
			$14, $15, $16,
			$17, $18, $19, $20, $21
		)`,
		pol.ID.String(), pol.TenantID, pol.AppID, pol.Name, pol.Description,
		pol.RateLimit, pol.RateLimitWindow.Milliseconds(), pol.BurstLimit,
		scopesJSON, ipsJSON, originsJSON, methodsJSON, pathsJSON,
		pol.MaxKeyLifetime.Milliseconds(), pol.RotationPeriod.Milliseconds(),
		pol.GracePeriod.Milliseconds(),
		pol.DailyQuota, pol.MonthlyQuota, meta, pol.CreatedAt, pol.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: create policy: %w", err)
	}
	return nil
}

func (s *policyStore) Get(ctx context.Context, polID id.PolicyID) (*policy.Policy, error) {
	return s.scanPolicy(s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, app_id, name, description,
		       rate_limit, rate_limit_window, burst_limit,
		       allowed_scopes, allowed_ips, allowed_origins, allowed_methods, allowed_paths,
		       max_key_lifetime, rotation_period, grace_period,
		       daily_quota, monthly_quota, metadata, created_at, updated_at
		FROM keysmith_policies WHERE id = $1`, polID.String()))
}

func (s *policyStore) GetByName(ctx context.Context, tenantID, name string) (*policy.Policy, error) {
	return s.scanPolicy(s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, app_id, name, description,
		       rate_limit, rate_limit_window, burst_limit,
		       allowed_scopes, allowed_ips, allowed_origins, allowed_methods, allowed_paths,
		       max_key_lifetime, rotation_period, grace_period,
		       daily_quota, monthly_quota, metadata, created_at, updated_at
		FROM keysmith_policies WHERE tenant_id = $1 AND name = $2`, tenantID, name))
}

func (s *policyStore) Update(ctx context.Context, pol *policy.Policy) error {
	meta, err := json.Marshal(pol.Metadata)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: marshal metadata: %w", err)
	}

	scopesJSON, _ := json.Marshal(pol.AllowedScopes)
	ipsJSON, _ := json.Marshal(pol.AllowedIPs)
	originsJSON, _ := json.Marshal(pol.AllowedOrigins)
	methodsJSON, _ := json.Marshal(pol.AllowedMethods)
	pathsJSON, _ := json.Marshal(pol.AllowedPaths)

	tag, err := s.pool.Exec(ctx, `
		UPDATE keysmith_policies SET
			name = $2, description = $3,
			rate_limit = $4, rate_limit_window = $5, burst_limit = $6,
			allowed_scopes = $7, allowed_ips = $8, allowed_origins = $9,
			allowed_methods = $10, allowed_paths = $11,
			max_key_lifetime = $12, rotation_period = $13, grace_period = $14,
			daily_quota = $15, monthly_quota = $16, metadata = $17, updated_at = $18
		WHERE id = $1`,
		pol.ID.String(), pol.Name, pol.Description,
		pol.RateLimit, pol.RateLimitWindow.Milliseconds(), pol.BurstLimit,
		scopesJSON, ipsJSON, originsJSON,
		methodsJSON, pathsJSON,
		pol.MaxKeyLifetime.Milliseconds(), pol.RotationPeriod.Milliseconds(),
		pol.GracePeriod.Milliseconds(),
		pol.DailyQuota, pol.MonthlyQuota, meta, pol.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("policy")
	}
	return nil
}

func (s *policyStore) Delete(ctx context.Context, polID id.PolicyID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM keysmith_policies WHERE id = $1`, polID.String())
	if err != nil {
		return fmt.Errorf("keysmith/postgres: delete policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("policy")
	}
	return nil
}

func (s *policyStore) List(ctx context.Context, filter *policy.ListFilter) ([]*policy.Policy, error) {
	where, args := buildPolicyWhere(filter)
	query := `
		SELECT id, tenant_id, app_id, name, description,
		       rate_limit, rate_limit_window, burst_limit,
		       allowed_scopes, allowed_ips, allowed_origins, allowed_methods, allowed_paths,
		       max_key_lifetime, rotation_period, grace_period,
		       daily_quota, monthly_quota, metadata, created_at, updated_at
		FROM keysmith_policies` + where + ` ORDER BY created_at DESC`

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
		return nil, fmt.Errorf("keysmith/postgres: list policies: %w", err)
	}
	defer rows.Close()

	return s.scanPolicies(rows)
}

func (s *policyStore) Count(ctx context.Context, filter *policy.ListFilter) (int64, error) {
	where, args := buildPolicyWhere(filter)
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM keysmith_policies`+where, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: count policies: %w", err)
	}
	return count, nil
}

// ── scan helpers ─────────────────────────────────────

func (s *policyStore) scanPolicy(row pgx.Row) (*policy.Policy, error) {
	var (
		pol         policy.Policy
		idStr       string
		rlWindow    int64
		maxLifetime int64
		rotPeriod   int64
		gracePeriod int64
		scopesJSON  []byte
		ipsJSON     []byte
		originsJSON []byte
		methodsJSON []byte
		pathsJSON   []byte
		metaJSON    []byte
	)

	err := row.Scan(
		&idStr, &pol.TenantID, &pol.AppID, &pol.Name, &pol.Description,
		&pol.RateLimit, &rlWindow, &pol.BurstLimit,
		&scopesJSON, &ipsJSON, &originsJSON, &methodsJSON, &pathsJSON,
		&maxLifetime, &rotPeriod, &gracePeriod,
		&pol.DailyQuota, &pol.MonthlyQuota, &metaJSON,
		&pol.CreatedAt, &pol.UpdatedAt,
	)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("policy")
		}
		return nil, fmt.Errorf("keysmith/postgres: scan policy: %w", err)
	}

	pol.ID, err = id.ParsePolicyID(idStr)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: parse policy id: %w", err)
	}

	pol.RateLimitWindow = time.Duration(rlWindow) * time.Millisecond
	pol.MaxKeyLifetime = time.Duration(maxLifetime) * time.Millisecond
	pol.RotationPeriod = time.Duration(rotPeriod) * time.Millisecond
	pol.GracePeriod = time.Duration(gracePeriod) * time.Millisecond

	_ = json.Unmarshal(scopesJSON, &pol.AllowedScopes)
	_ = json.Unmarshal(ipsJSON, &pol.AllowedIPs)
	_ = json.Unmarshal(originsJSON, &pol.AllowedOrigins)
	_ = json.Unmarshal(methodsJSON, &pol.AllowedMethods)
	_ = json.Unmarshal(pathsJSON, &pol.AllowedPaths)

	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &pol.Metadata); err != nil {
			return nil, fmt.Errorf("keysmith/postgres: unmarshal metadata: %w", err)
		}
	}

	return &pol, nil
}

func (s *policyStore) scanPolicies(rows pgx.Rows) ([]*policy.Policy, error) {
	var result []*policy.Policy
	for rows.Next() {
		var (
			pol         policy.Policy
			idStr       string
			rlWindow    int64
			maxLifetime int64
			rotPeriod   int64
			gracePeriod int64
			scopesJSON  []byte
			ipsJSON     []byte
			originsJSON []byte
			methodsJSON []byte
			pathsJSON   []byte
			metaJSON    []byte
		)

		err := rows.Scan(
			&idStr, &pol.TenantID, &pol.AppID, &pol.Name, &pol.Description,
			&pol.RateLimit, &rlWindow, &pol.BurstLimit,
			&scopesJSON, &ipsJSON, &originsJSON, &methodsJSON, &pathsJSON,
			&maxLifetime, &rotPeriod, &gracePeriod,
			&pol.DailyQuota, &pol.MonthlyQuota, &metaJSON,
			&pol.CreatedAt, &pol.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: scan policy: %w", err)
		}

		pol.ID, err = id.ParsePolicyID(idStr)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: parse policy id: %w", err)
		}

		pol.RateLimitWindow = time.Duration(rlWindow) * time.Millisecond
		pol.MaxKeyLifetime = time.Duration(maxLifetime) * time.Millisecond
		pol.RotationPeriod = time.Duration(rotPeriod) * time.Millisecond
		pol.GracePeriod = time.Duration(gracePeriod) * time.Millisecond

		_ = json.Unmarshal(scopesJSON, &pol.AllowedScopes)
		_ = json.Unmarshal(ipsJSON, &pol.AllowedIPs)
		_ = json.Unmarshal(originsJSON, &pol.AllowedOrigins)
		_ = json.Unmarshal(methodsJSON, &pol.AllowedMethods)
		_ = json.Unmarshal(pathsJSON, &pol.AllowedPaths)

		if len(metaJSON) > 0 {
			if err := json.Unmarshal(metaJSON, &pol.Metadata); err != nil {
				return nil, fmt.Errorf("keysmith/postgres: unmarshal metadata: %w", err)
			}
		}

		result = append(result, &pol)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: rows: %w", err)
	}
	return result, nil
}

func buildPolicyWhere(f *policy.ListFilter) (where string, args []any) {
	if f == nil || f.TenantID == "" {
		return "", nil
	}
	return " WHERE tenant_id = $1", []any{f.TenantID}
}
