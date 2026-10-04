package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/xraph/grove/drivers/pgdriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/policy"
)

type policyStore struct {
	db *pgdriver.PgDB
}

func (s *policyStore) Create(ctx context.Context, pol *policy.Policy) error {
	m := policyToModel(pol)
	_, err := s.db.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: create policy: %w", err)
	}
	return nil
}

func (s *policyStore) Get(ctx context.Context, polID id.PolicyID) (*policy.Policy, error) {
	m := new(policyModel)
	err := s.db.NewSelect(m).Where("id = ?", polID.String()).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("policy")
		}
		return nil, fmt.Errorf("keysmith/postgres: get policy: %w", err)
	}
	return policyFromModel(m)
}

func (s *policyStore) GetByName(ctx context.Context, tenantID, name string) (*policy.Policy, error) {
	m := new(policyModel)
	err := s.db.NewSelect(m).
		Where("tenant_id = ?", tenantID).
		Where("name = ?", name).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound("policy")
		}
		return nil, fmt.Errorf("keysmith/postgres: get policy by name: %w", err)
	}
	return policyFromModel(m)
}

func (s *policyStore) Update(ctx context.Context, pol *policy.Policy) error {
	m := policyToModel(pol)
	res, err := s.db.NewUpdate(m).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: update policy: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errNotFound("policy")
	}
	return nil
}

func (s *policyStore) Delete(ctx context.Context, polID id.PolicyID) error {
	res, err := s.db.NewDelete((*policyModel)(nil)).
		Where("id = ?", polID.String()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/postgres: delete policy: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errNotFound("policy")
	}
	return nil
}

func (s *policyStore) List(ctx context.Context, filter *policy.ListFilter) ([]*policy.Policy, error) {
	var models []policyModel
	q := s.db.NewSelect(&models).OrderExpr("created_at DESC, id DESC")

	if filter != nil {
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
		if filter.Limit > 0 {
			q = q.Limit(filter.Limit)
		}
		if filter.Offset > 0 {
			q = q.Offset(filter.Offset)
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/postgres: list policies: %w", err)
	}

	result := make([]*policy.Policy, 0, len(models))
	for i := range models {
		pol, err := policyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/postgres: convert policy: %w", err)
		}
		result = append(result, pol)
	}
	return result, nil
}

func (s *policyStore) Count(ctx context.Context, filter *policy.ListFilter) (int64, error) {
	q := s.db.NewSelect((*policyModel)(nil))

	if filter != nil {
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
	}

	count, err := q.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/postgres: count policies: %w", err)
	}
	return count, nil
}
