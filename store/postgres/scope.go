package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/xraph/grove/driver"
	"github.com/xraph/grove/drivers/pgdriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/scope"
)

type scopeStore struct {
	db *pgdriver.PgDB
}

func (s *scopeStore) Create(ctx context.Context, sc *scope.Scope) error {
	m := scopeToModel(sc)
	_, err := s.db.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: create scope: %w", err)
	}
	return nil
}

func (s *scopeStore) Get(ctx context.Context, scopeID id.ScopeID) (*scope.Scope, error) {
	m := new(scopeModel)
	err := s.db.NewSelect(m).Where("id = ?", scopeID.String()).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("scope")
		}
		return nil, fmt.Errorf("keysmith/postgres: get scope: %w", err)
	}
	return scopeFromModel(m)
}

func (s *scopeStore) GetByName(ctx context.Context, tenantID, name string) (*scope.Scope, error) {
	m := new(scopeModel)
	err := s.db.NewSelect(m).
		Where("tenant_id = ?", tenantID).
		Where("name = ?", name).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("scope")
		}
		return nil, fmt.Errorf("keysmith/postgres: get scope by name: %w", err)
	}
	return scopeFromModel(m)
}

func (s *scopeStore) Update(ctx context.Context, sc *scope.Scope) error {
	m := scopeToModel(sc)
	res, err := s.db.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update scope: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errNotFound("scope")
	}
	return nil
}

func (s *scopeStore) Delete(ctx context.Context, scopeID id.ScopeID) error {
	res, err := s.db.NewDelete((*scopeModel)(nil)).
		Where("id = ?", scopeID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: delete scope: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errNotFound("scope")
	}
	return nil
}

func (s *scopeStore) List(ctx context.Context, filter *scope.ListFilter) ([]*scope.Scope, error) {
	var models []scopeModel
	q := s.db.NewSelect(&models).OrderExpr("name ASC, id ASC")

	if filter != nil {
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
		if filter.Parent != "" {
			q = q.Where("parent = ?", filter.Parent)
		}
		if filter.Limit > 0 {
			q = q.Limit(filter.Limit)
		}
		if filter.Offset > 0 {
			q = q.Offset(filter.Offset)
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: list scopes: %w", err)
	}

	result := make([]*scope.Scope, 0, len(models))
	for i := range models {
		sc, err := scopeFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: convert scope: %w", err)
		}
		result = append(result, sc)
	}
	return result, nil
}

func (s *scopeStore) ListByKey(ctx context.Context, keyID id.KeyID) ([]*scope.Scope, error) {
	var models []scopeModel
	err := s.db.NewSelect(&models).
		Join("INNER JOIN", "keysmith_key_scopes AS ks", "ks.scope_id = keysmith_scopes.id").
		Where("ks.key_id = ?", keyID.String()).
		OrderExpr("keysmith_scopes.name ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: list scopes by key: %w", err)
	}

	result := make([]*scope.Scope, 0, len(models))
	for i := range models {
		sc, err := scopeFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: convert scope: %w", err)
		}
		result = append(result, sc)
	}
	return result, nil
}

func (s *scopeStore) AssignToKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	if len(scopeNames) == 0 {
		return nil
	}

	tx, err := s.db.BeginTxQuery(ctx, &driver.TxOptions{})
	if err != nil {
		return fmt.Errorf("keysmith/postgres: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	kid := keyID.String()
	for _, name := range scopeNames {
		// Look up scope ID by name within the same tenant as the key.
		var scopeID string
		err := tx.NewRaw(`
			SELECT s.id FROM keysmith_scopes s
			INNER JOIN keysmith_keys k ON k.tenant_id = s.tenant_id
			WHERE k.id = $1 AND s.name = $2`, kid, name).Scan(ctx, &scopeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errNotFound("scope")
			}
			return fmt.Errorf("keysmith/postgres: lookup scope %q: %w", name, err)
		}

		m := &keyScopeModel{KeyID: kid, ScopeID: scopeID}
		_, err = tx.NewInsert(m).OnConflict("DO NOTHING").Exec(ctx)
		if err != nil {
			return fmt.Errorf("keysmith/postgres: assign scope: %w", err)
		}
	}

	return tx.Commit()
}

func (s *scopeStore) RemoveFromKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	if len(scopeNames) == 0 {
		return nil
	}

	tx, err := s.db.BeginTxQuery(ctx, &driver.TxOptions{})
	if err != nil {
		return fmt.Errorf("keysmith/postgres: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	kid := keyID.String()
	for _, name := range scopeNames {
		_, err = tx.NewRaw(`
			DELETE FROM keysmith_key_scopes
			WHERE key_id = $1 AND scope_id = (
				SELECT s.id FROM keysmith_scopes s
				INNER JOIN keysmith_keys k ON k.tenant_id = s.tenant_id
				WHERE k.id = $1 AND s.name = $2
			)`, kid, name).Exec(ctx)
		if err != nil {
			return fmt.Errorf("keysmith/postgres: remove scope: %w", err)
		}
	}

	return tx.Commit()
}
