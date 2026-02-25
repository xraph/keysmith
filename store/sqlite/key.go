package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
)

type keyStore struct {
	sdb *sqlitedriver.SqliteDB
}

func (s *keyStore) Create(ctx context.Context, k *key.Key) error {
	m := keyToModel(k)
	_, err := s.sdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: create key: %w", err)
	}
	return nil
}

func (s *keyStore) Get(ctx context.Context, keyID id.KeyID) (*key.Key, error) {
	m := new(keyModel)
	err := s.sdb.NewSelect(m).Where("id = ?", keyID.String()).Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/sqlite: get key: %w", err)
	}
	return keyFromModel(m)
}

func (s *keyStore) GetByHash(ctx context.Context, hash string) (*key.Key, error) {
	m := new(keyModel)
	err := s.sdb.NewSelect(m).Where("key_hash = ?", hash).Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/sqlite: get key by hash: %w", err)
	}
	return keyFromModel(m)
}

func (s *keyStore) GetByPrefix(ctx context.Context, prefix, hint string) (*key.Key, error) {
	m := new(keyModel)
	err := s.sdb.NewSelect(m).
		Where("prefix = ?", prefix).
		Where("hint = ?", hint).
		Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/sqlite: get key by prefix: %w", err)
	}
	return keyFromModel(m)
}

func (s *keyStore) Update(ctx context.Context, k *key.Key) error {
	m := keyToModel(k)
	res, err := s.sdb.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: update key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: update key rows: %w", err)
	}
	if rows == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) UpdateState(ctx context.Context, keyID id.KeyID, state key.State) error {
	res, err := s.sdb.NewUpdate((*keyModel)(nil)).
		Set("state = ?", string(state)).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", keyID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: update key state: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: update key state rows: %w", err)
	}
	if rows == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) UpdateLastUsed(ctx context.Context, keyID id.KeyID, at time.Time) error {
	res, err := s.sdb.NewUpdate((*keyModel)(nil)).
		Set("last_used_at = ?", at).
		Where("id = ?", keyID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: update last used: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: update last used rows: %w", err)
	}
	if rows == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) Delete(ctx context.Context, keyID id.KeyID) error {
	res, err := s.sdb.NewDelete((*keyModel)(nil)).
		Where("id = ?", keyID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: delete key: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: delete key rows: %w", err)
	}
	if rows == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) List(ctx context.Context, filter *key.ListFilter) ([]*key.Key, error) {
	var models []keyModel
	q := s.sdb.NewSelect(&models).OrderExpr("created_at DESC")

	if filter != nil {
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
		if filter.Environment != "" {
			q = q.Where("environment = ?", string(filter.Environment))
		}
		if filter.State != "" {
			q = q.Where("state = ?", string(filter.State))
		}
		if filter.PolicyID != nil {
			q = q.Where("policy_id = ?", filter.PolicyID.String())
		}
		if filter.CreatedBy != "" {
			q = q.Where("created_by = ?", filter.CreatedBy)
		}
		if filter.Limit > 0 {
			q = q.Limit(filter.Limit)
		}
		if filter.Offset > 0 {
			q = q.Offset(filter.Offset)
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: list keys: %w", err)
	}

	result := make([]*key.Key, 0, len(models))
	for i := range models {
		k, err := keyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: convert key: %w", err)
		}
		result = append(result, k)
	}
	return result, nil
}

func (s *keyStore) Count(ctx context.Context, filter *key.ListFilter) (int64, error) {
	q := s.sdb.NewSelect((*keyModel)(nil))

	if filter != nil {
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
		if filter.Environment != "" {
			q = q.Where("environment = ?", string(filter.Environment))
		}
		if filter.State != "" {
			q = q.Where("state = ?", string(filter.State))
		}
		if filter.PolicyID != nil {
			q = q.Where("policy_id = ?", filter.PolicyID.String())
		}
		if filter.CreatedBy != "" {
			q = q.Where("created_by = ?", filter.CreatedBy)
		}
	}

	count, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/sqlite: count keys: %w", err)
	}
	return count, nil
}

func (s *keyStore) ListExpired(ctx context.Context, before time.Time) ([]*key.Key, error) {
	var models []keyModel
	err := s.sdb.NewSelect(&models).
		Where("state = ?", string(key.StateActive)).
		Where("expires_at IS NOT NULL").
		Where("expires_at < ?", before).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: list expired: %w", err)
	}

	result := make([]*key.Key, 0, len(models))
	for i := range models {
		k, err := keyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: convert key: %w", err)
		}
		result = append(result, k)
	}
	return result, nil
}

func (s *keyStore) ListByPolicy(ctx context.Context, policyID id.PolicyID) ([]*key.Key, error) {
	var models []keyModel
	err := s.sdb.NewSelect(&models).
		Where("policy_id = ?", policyID.String()).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: list by policy: %w", err)
	}

	result := make([]*key.Key, 0, len(models))
	for i := range models {
		k, err := keyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: convert key: %w", err)
		}
		result = append(result, k)
	}
	return result, nil
}

func (s *keyStore) DeleteByTenant(ctx context.Context, tenantID string) error {
	_, err := s.sdb.NewDelete((*keyModel)(nil)).
		Where("tenant_id = ?", tenantID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: delete by tenant: %w", err)
	}
	return nil
}
