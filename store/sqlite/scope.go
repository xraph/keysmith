package sqlite

import (
	"context"
	"fmt"

	"github.com/xraph/grove/driver"
	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/scope"
)

type scopeStore struct {
	sdb *sqlitedriver.SqliteDB
}

func (s *scopeStore) Create(ctx context.Context, sc *scope.Scope) error {
	m := scopeToModel(sc)
	_, err := s.sdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: create scope: %w", err)
	}
	return nil
}

func (s *scopeStore) Get(ctx context.Context, scopeID id.ScopeID) (*scope.Scope, error) {
	m := new(scopeModel)
	err := s.sdb.NewSelect(m).Where("id = ?", scopeID.String()).Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("scope")
		}
		return nil, fmt.Errorf("keysmith/sqlite: get scope: %w", err)
	}
	return scopeFromModel(m)
}

func (s *scopeStore) GetByName(ctx context.Context, tenantID, name string) (*scope.Scope, error) {
	m := new(scopeModel)
	err := s.sdb.NewSelect(m).
		Where("tenant_id = ?", tenantID).
		Where("name = ?", name).
		Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("scope")
		}
		return nil, fmt.Errorf("keysmith/sqlite: get scope by name: %w", err)
	}
	return scopeFromModel(m)
}

func (s *scopeStore) Update(ctx context.Context, sc *scope.Scope) error {
	m := scopeToModel(sc)
	res, err := s.sdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: update scope: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: update scope rows: %w", err)
	}
	if rows == 0 {
		return errNotFound("scope")
	}
	return nil
}

func (s *scopeStore) Delete(ctx context.Context, scopeID id.ScopeID) error {
	res, err := s.sdb.NewDelete((*scopeModel)(nil)).
		Where("id = ?", scopeID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: delete scope: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: delete scope rows: %w", err)
	}
	if rows == 0 {
		return errNotFound("scope")
	}
	return nil
}

func (s *scopeStore) List(ctx context.Context, filter *scope.ListFilter) ([]*scope.Scope, error) {
	var models []scopeModel
	q := s.sdb.NewSelect(&models).OrderExpr("name ASC")

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
		return nil, fmt.Errorf("keysmith/sqlite: list scopes: %w", err)
	}

	result := make([]*scope.Scope, 0, len(models))
	for i := range models {
		sc, err := scopeFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: convert scope: %w", err)
		}
		result = append(result, sc)
	}
	return result, nil
}

func (s *scopeStore) ListByKey(ctx context.Context, keyID id.KeyID) ([]*scope.Scope, error) {
	var models []scopeModel
	err := s.sdb.NewSelect(&models).
		Join("INNER JOIN", "keysmith_key_scopes AS ks", "ks.scope_id = keysmith_scopes.id").
		Where("ks.key_id = ?", keyID.String()).
		OrderExpr("keysmith_scopes.name ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: list scopes by key: %w", err)
	}

	result := make([]*scope.Scope, 0, len(models))
	for i := range models {
		sc, err := scopeFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: convert scope: %w", err)
		}
		result = append(result, sc)
	}
	return result, nil
}

func (s *scopeStore) AssignToKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	if len(scopeNames) == 0 {
		return nil
	}

	tx, err := s.sdb.BeginTxQuery(ctx, &driver.TxOptions{})
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	kid := keyID.String()
	for _, name := range scopeNames {
		var scopeID string
		err := tx.NewRaw(`
			SELECT s.id FROM keysmith_scopes s
			INNER JOIN keysmith_keys k ON k.tenant_id = s.tenant_id
			WHERE k.id = ? AND s.name = ?`, kid, name).Scan(ctx, &scopeID)
		if err != nil {
			if isNoRows(err) {
				return errNotFound("scope")
			}
			return fmt.Errorf("keysmith/sqlite: lookup scope %q: %w", name, err)
		}

		m := &keyScopeModel{KeyID: kid, ScopeID: scopeID}
		_, err = tx.NewInsert(m).OnConflict("DO NOTHING").Exec(ctx)
		if err != nil {
			return fmt.Errorf("keysmith/sqlite: assign scope: %w", err)
		}
	}

	return tx.Commit()
}

func (s *scopeStore) RemoveFromKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	if len(scopeNames) == 0 {
		return nil
	}

	tx, err := s.sdb.BeginTxQuery(ctx, &driver.TxOptions{})
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	kid := keyID.String()
	for _, name := range scopeNames {
		_, err = tx.NewRaw(`
			DELETE FROM keysmith_key_scopes
			WHERE key_id = ? AND scope_id = (
				SELECT s.id FROM keysmith_scopes s
				INNER JOIN keysmith_keys k ON k.tenant_id = s.tenant_id
				WHERE k.id = ? AND s.name = ?
			)`, kid, kid, name).Exec(ctx)
		if err != nil {
			return fmt.Errorf("keysmith/sqlite: remove scope: %w", err)
		}
	}

	return tx.Commit()
}
