package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/grove/drivers/mongodriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/policy"
)

type policyStore struct {
	mdb *mongodriver.MongoDB
}

func (s *policyStore) Create(ctx context.Context, pol *policy.Policy) error {
	m := policyToModel(pol)
	_, err := s.mdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: create policy: %w", err)
	}
	return nil
}

func (s *policyStore) Get(ctx context.Context, polID id.PolicyID) (*policy.Policy, error) {
	var m policyModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"_id": polID.String()}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, errNotFound("policy")
		}
		return nil, fmt.Errorf("keysmith/mongo: get policy: %w", err)
	}
	return policyFromModel(&m)
}

func (s *policyStore) GetByName(ctx context.Context, tenantID, name string) (*policy.Policy, error) {
	var m policyModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"tenant_id": tenantID, "name": name}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, errNotFound("policy")
		}
		return nil, fmt.Errorf("keysmith/mongo: get policy by name: %w", err)
	}
	return policyFromModel(&m)
}

func (s *policyStore) Update(ctx context.Context, pol *policy.Policy) error {
	m := policyToModel(pol)
	res, err := s.mdb.NewUpdate(m).
		Filter(bson.M{"_id": m.ID}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: update policy: %w", err)
	}
	if res.MatchedCount() == 0 {
		return errNotFound("policy")
	}
	return nil
}

func (s *policyStore) Delete(ctx context.Context, polID id.PolicyID) error {
	res, err := s.mdb.NewDelete((*policyModel)(nil)).
		Filter(bson.M{"_id": polID.String()}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: delete policy: %w", err)
	}
	if res.DeletedCount() == 0 {
		return errNotFound("policy")
	}
	return nil
}

func (s *policyStore) List(ctx context.Context, filter *policy.ListFilter) ([]*policy.Policy, error) {
	var models []policyModel

	f := bson.M{}
	if filter != nil {
		if filter.TenantID != "" {
			f["tenant_id"] = filter.TenantID
		}
	}

	q := s.mdb.NewFind(&models).
		Filter(f).
		Sort(bson.D{{Key: "created_at", Value: -1}})

	if filter != nil {
		if filter.Limit > 0 {
			q = q.Limit(int64(filter.Limit))
		}
		if filter.Offset > 0 {
			q = q.Skip(int64(filter.Offset))
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/mongo: list policies: %w", err)
	}

	result := make([]*policy.Policy, 0, len(models))
	for i := range models {
		pol, err := policyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert policy: %w", err)
		}
		result = append(result, pol)
	}
	return result, nil
}

func (s *policyStore) Count(ctx context.Context, filter *policy.ListFilter) (int64, error) {
	f := bson.M{}
	if filter != nil {
		if filter.TenantID != "" {
			f["tenant_id"] = filter.TenantID
		}
	}

	count, err := s.mdb.NewFind((*policyModel)(nil)).
		Filter(f).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/mongo: count policies: %w", err)
	}
	return count, nil
}
