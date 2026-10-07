package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/grove/drivers/pgdriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/groveset"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store"
)

type keyStore struct {
	db *pgdriver.PgDB
}

func (s *keyStore) Create(ctx context.Context, k *key.Key) error {
	m := keyToModel(k)
	_, err := s.db.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: create key: %w", err)
	}
	return nil
}

func (s *keyStore) Get(ctx context.Context, keyID id.KeyID) (*key.Key, error) {
	m := new(keyModel)
	err := s.db.NewSelect(m).Where("id = ?", keyID.String()).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/postgres: get key: %w", err)
	}
	return keyFromModel(m)
}

func (s *keyStore) GetByHash(ctx context.Context, hash string) (*key.Key, error) {
	m := new(keyModel)
	err := s.db.NewSelect(m).Where("key_hash = ?", hash).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/postgres: get key by hash: %w", err)
	}
	return keyFromModel(m)
}

func (s *keyStore) GetByPrefix(ctx context.Context, prefix, hint string) (*key.Key, error) {
	m := new(keyModel)
	err := s.db.NewSelect(m).
		Where("prefix = ?", prefix).
		Where("hint = ?", hint).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/postgres: get key by prefix: %w", err)
	}
	return keyFromModel(m)
}

// Update writes every column the model holds, as a model-based UPDATE would,
// except version, which it counts up in the same statement. Explicit Set
// calls replace the model-based SET wholesale, so groveset lists the columns.
func (s *keyStore) Update(ctx context.Context, k *key.Key) error {
	cols, err := groveset.Columns(keyToModel(k), "version")
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update key: %w", err)
	}
	q := s.db.NewUpdate((*keyModel)(nil))
	for _, c := range cols {
		q = q.Set(`"`+c.Name+`" = ?`, c.Value)
	}
	res, err := q.Set("version = version + 1").
		Where("id = ?", k.ID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update key: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) UpdateIfVersion(ctx context.Context, k *key.Key, version int64) error {
	m := keyToModel(k)
	m.Version = version + 1
	res, err := s.db.NewUpdate(m).
		WherePK().
		Where("version = ?", version).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update key: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return s.missingOrConflict(ctx, k.ID)
	}
	k.Version = version + 1
	return nil
}

// missingOrConflict tells a version-checked update that matched no row
// apart: the key is gone, or it is there at another version.
func (s *keyStore) missingOrConflict(ctx context.Context, keyID id.KeyID) error {
	n, err := s.db.NewSelect((*keyModel)(nil)).Where("id = ?", keyID.String()).Count(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: look up key after a refused update: %w", err)
	}
	if n == 0 {
		return errNotFound("key")
	}
	return store.ErrKeyConflict
}

func (s *keyStore) UpdateState(ctx context.Context, keyID id.KeyID, state key.State) error {
	res, err := s.db.NewUpdate((*keyModel)(nil)).
		Set("state = ?", string(state)).
		Set("updated_at = ?", time.Now().UTC()).
		Set("version = version + 1").
		Where("id = ?", keyID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update key state: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) UpdateLastUsed(ctx context.Context, keyID id.KeyID, at time.Time) error {
	res, err := s.db.NewUpdate((*keyModel)(nil)).
		Set("last_used_at = ?", at).
		Where("id = ?", keyID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update last used: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) Delete(ctx context.Context, keyID id.KeyID) error {
	res, err := s.db.NewDelete((*keyModel)(nil)).
		Where("id = ?", keyID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: delete key: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) List(ctx context.Context, filter *key.ListFilter) ([]*key.Key, error) {
	var models []keyModel
	q := s.db.NewSelect(&models).OrderExpr("created_at DESC, id DESC")

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
		return nil, fmt.Errorf("keysmith/postgres: list keys: %w", err)
	}

	result := make([]*key.Key, 0, len(models))
	for i := range models {
		k, err := keyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: convert key: %w", err)
		}
		result = append(result, k)
	}
	return result, nil
}

func (s *keyStore) Count(ctx context.Context, filter *key.ListFilter) (int64, error) {
	q := s.db.NewSelect((*keyModel)(nil))

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
		return 0, fmt.Errorf("keysmith/postgres: count keys: %w", err)
	}
	return count, nil
}

func (s *keyStore) ListExpired(ctx context.Context, before time.Time) ([]*key.Key, error) {
	var models []keyModel
	err := s.db.NewSelect(&models).
		Where("state = ?", string(key.StateActive)).
		Where("expires_at IS NOT NULL").
		Where("expires_at < ?", before).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: list expired: %w", err)
	}

	result := make([]*key.Key, 0, len(models))
	for i := range models {
		k, err := keyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: convert key: %w", err)
		}
		result = append(result, k)
	}
	return result, nil
}

func (s *keyStore) ListByPolicy(ctx context.Context, policyID id.PolicyID) ([]*key.Key, error) {
	var models []keyModel
	err := s.db.NewSelect(&models).
		Where("policy_id = ?", policyID.String()).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/postgres: list by policy: %w", err)
	}

	result := make([]*key.Key, 0, len(models))
	for i := range models {
		k, err := keyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: convert key: %w", err)
		}
		result = append(result, k)
	}
	return result, nil
}

func (s *keyStore) DeleteByTenant(ctx context.Context, tenantID string) error {
	_, err := s.db.NewDelete((*keyModel)(nil)).
		Where("tenant_id = ?", tenantID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: delete by tenant: %w", err)
	}
	return nil
}
