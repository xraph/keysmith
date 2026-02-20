package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/scope"
)

type scopeStore struct {
	pool *pgxpool.Pool
}

func (s *scopeStore) Create(ctx context.Context, sc *scope.Scope) error {
	meta, err := json.Marshal(sc.Metadata)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: marshal metadata: %w", err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO keysmith_scopes (id, tenant_id, app_id, name, description, parent, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		sc.ID.String(), sc.TenantID, sc.AppID, sc.Name, sc.Description,
		nilIfEmpty(sc.Parent), meta, sc.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: create scope: %w", err)
	}
	return nil
}

func (s *scopeStore) Get(ctx context.Context, scopeID id.ScopeID) (*scope.Scope, error) {
	return s.scanScope(s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, app_id, name, description, parent, metadata, created_at
		FROM keysmith_scopes WHERE id = $1`, scopeID.String()))
}

func (s *scopeStore) GetByName(ctx context.Context, tenantID, name string) (*scope.Scope, error) {
	return s.scanScope(s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, app_id, name, description, parent, metadata, created_at
		FROM keysmith_scopes WHERE tenant_id = $1 AND name = $2`, tenantID, name))
}

func (s *scopeStore) Update(ctx context.Context, sc *scope.Scope) error {
	meta, err := json.Marshal(sc.Metadata)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: marshal metadata: %w", err)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE keysmith_scopes SET
			name = $2, description = $3, parent = $4, metadata = $5
		WHERE id = $1`,
		sc.ID.String(), sc.Name, sc.Description, nilIfEmpty(sc.Parent), meta,
	)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update scope: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("scope")
	}
	return nil
}

func (s *scopeStore) Delete(ctx context.Context, scopeID id.ScopeID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM keysmith_scopes WHERE id = $1`, scopeID.String())
	if err != nil {
		return fmt.Errorf("keysmith/postgres: delete scope: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("scope")
	}
	return nil
}

func (s *scopeStore) List(ctx context.Context, filter *scope.ListFilter) ([]*scope.Scope, error) {
	where, args := buildScopeWhere(filter)
	query := `
		SELECT id, tenant_id, app_id, name, description, parent, metadata, created_at
		FROM keysmith_scopes` + where + ` ORDER BY name ASC`

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
		return nil, fmt.Errorf("keysmith/postgres: list scopes: %w", err)
	}
	defer rows.Close()

	return s.scanScopes(rows)
}

func (s *scopeStore) ListByKey(ctx context.Context, keyID id.KeyID) ([]*scope.Scope, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.tenant_id, s.app_id, s.name, s.description, s.parent, s.metadata, s.created_at
		FROM keysmith_scopes s
		INNER JOIN keysmith_key_scopes ks ON ks.scope_id = s.id
		WHERE ks.key_id = $1
		ORDER BY s.name ASC`, keyID.String())
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: list scopes by key: %w", err)
	}
	defer rows.Close()

	return s.scanScopes(rows)
}

func (s *scopeStore) AssignToKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	if len(scopeNames) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	kid := keyID.String()
	for _, name := range scopeNames {
		// Look up scope ID by name within the same tenant as the key.
		var scopeID string
		err := tx.QueryRow(ctx, `
			SELECT s.id FROM keysmith_scopes s
			INNER JOIN keysmith_keys k ON k.tenant_id = s.tenant_id
			WHERE k.id = $1 AND s.name = $2`, kid, name).Scan(&scopeID)
		if err != nil {
			if isNoRows(err) {
				return errNotFound("scope")
			}
			return fmt.Errorf("keysmith/postgres: lookup scope %q: %w", name, err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO keysmith_key_scopes (key_id, scope_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, kid, scopeID)
		if err != nil {
			return fmt.Errorf("keysmith/postgres: assign scope: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (s *scopeStore) RemoveFromKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	if len(scopeNames) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	kid := keyID.String()
	for _, name := range scopeNames {
		_, err = tx.Exec(ctx, `
			DELETE FROM keysmith_key_scopes
			WHERE key_id = $1 AND scope_id = (
				SELECT s.id FROM keysmith_scopes s
				INNER JOIN keysmith_keys k ON k.tenant_id = s.tenant_id
				WHERE k.id = $1 AND s.name = $2
			)`, kid, name)
		if err != nil {
			return fmt.Errorf("keysmith/postgres: remove scope: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// ── scan helpers ─────────────────────────────────────

func (s *scopeStore) scanScope(row pgx.Row) (*scope.Scope, error) {
	var (
		sc       scope.Scope
		idStr    string
		parent   *string
		metaJSON []byte
	)

	err := row.Scan(
		&idStr, &sc.TenantID, &sc.AppID, &sc.Name, &sc.Description,
		&parent, &metaJSON, &sc.CreatedAt,
	)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("scope")
		}
		return nil, fmt.Errorf("keysmith/postgres: scan scope: %w", err)
	}

	sc.ID, err = id.ParseScopeID(idStr)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: parse scope id: %w", err)
	}

	if parent != nil {
		sc.Parent = *parent
	}

	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &sc.Metadata); err != nil {
			return nil, fmt.Errorf("keysmith/postgres: unmarshal metadata: %w", err)
		}
	}

	return &sc, nil
}

func (s *scopeStore) scanScopes(rows pgx.Rows) ([]*scope.Scope, error) {
	var result []*scope.Scope
	for rows.Next() {
		var (
			sc       scope.Scope
			idStr    string
			parent   *string
			metaJSON []byte
		)

		err := rows.Scan(
			&idStr, &sc.TenantID, &sc.AppID, &sc.Name, &sc.Description,
			&parent, &metaJSON, &sc.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: scan scope: %w", err)
		}

		sc.ID, err = id.ParseScopeID(idStr)
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: parse scope id: %w", err)
		}

		if parent != nil {
			sc.Parent = *parent
		}

		if len(metaJSON) > 0 {
			if err := json.Unmarshal(metaJSON, &sc.Metadata); err != nil {
				return nil, fmt.Errorf("keysmith/postgres: unmarshal metadata: %w", err)
			}
		}

		result = append(result, &sc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: rows: %w", err)
	}
	return result, nil
}

func buildScopeWhere(f *scope.ListFilter) (where string, args []any) {
	if f == nil {
		return "", nil
	}

	var clauses []string

	if f.TenantID != "" {
		args = append(args, f.TenantID)
		clauses = append(clauses, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if f.Parent != "" {
		args = append(args, f.Parent)
		clauses = append(clauses, fmt.Sprintf("parent = $%d", len(args)))
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
