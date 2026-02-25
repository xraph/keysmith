package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/grove/drivers/mongodriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/scope"
)

type scopeStore struct {
	mdb *mongodriver.MongoDB
}

func (s *scopeStore) Create(ctx context.Context, sc *scope.Scope) error {
	m := scopeToModel(sc)
	_, err := s.mdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: create scope: %w", err)
	}
	return nil
}

func (s *scopeStore) Get(ctx context.Context, scopeID id.ScopeID) (*scope.Scope, error) {
	var m scopeModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"_id": scopeID.String()}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, errNotFound("scope")
		}
		return nil, fmt.Errorf("keysmith/mongo: get scope: %w", err)
	}
	return scopeFromModel(&m)
}

func (s *scopeStore) GetByName(ctx context.Context, tenantID, name string) (*scope.Scope, error) {
	var m scopeModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"tenant_id": tenantID, "name": name}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, errNotFound("scope")
		}
		return nil, fmt.Errorf("keysmith/mongo: get scope by name: %w", err)
	}
	return scopeFromModel(&m)
}

func (s *scopeStore) Update(ctx context.Context, sc *scope.Scope) error {
	m := scopeToModel(sc)
	res, err := s.mdb.NewUpdate(m).
		Filter(bson.M{"_id": m.ID}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: update scope: %w", err)
	}
	if res.MatchedCount() == 0 {
		return errNotFound("scope")
	}
	return nil
}

func (s *scopeStore) Delete(ctx context.Context, scopeID id.ScopeID) error {
	res, err := s.mdb.NewDelete((*scopeModel)(nil)).
		Filter(bson.M{"_id": scopeID.String()}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: delete scope: %w", err)
	}
	if res.DeletedCount() == 0 {
		return errNotFound("scope")
	}
	return nil
}

func (s *scopeStore) List(ctx context.Context, filter *scope.ListFilter) ([]*scope.Scope, error) {
	var models []scopeModel

	f := bson.M{}
	if filter != nil {
		if filter.TenantID != "" {
			f["tenant_id"] = filter.TenantID
		}
		if filter.Parent != "" {
			f["parent"] = filter.Parent
		}
	}

	q := s.mdb.NewFind(&models).
		Filter(f).
		Sort(bson.D{{Key: "name", Value: 1}})

	if filter != nil {
		if filter.Limit > 0 {
			q = q.Limit(int64(filter.Limit))
		}
		if filter.Offset > 0 {
			q = q.Skip(int64(filter.Offset))
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/mongo: list scopes: %w", err)
	}

	result := make([]*scope.Scope, 0, len(models))
	for i := range models {
		sc, err := scopeFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert scope: %w", err)
		}
		result = append(result, sc)
	}
	return result, nil
}

func (s *scopeStore) ListByKey(ctx context.Context, keyID id.KeyID) ([]*scope.Scope, error) {
	// First, find all scope IDs assigned to this key.
	var keyScopeModels []keyScopeModel
	err := s.mdb.NewFind(&keyScopeModels).
		Filter(bson.M{"key_id": keyID.String()}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/mongo: list key scopes: %w", err)
	}

	if len(keyScopeModels) == 0 {
		return []*scope.Scope{}, nil
	}

	scopeIDs := make([]string, len(keyScopeModels))
	for i, ks := range keyScopeModels {
		scopeIDs[i] = ks.ScopeID
	}

	// Then, fetch the scope documents.
	var models []scopeModel
	err = s.mdb.NewFind(&models).
		Filter(bson.M{"_id": bson.M{"$in": scopeIDs}}).
		Sort(bson.D{{Key: "name", Value: 1}}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/mongo: list scopes by key: %w", err)
	}

	result := make([]*scope.Scope, 0, len(models))
	for i := range models {
		sc, err := scopeFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert scope: %w", err)
		}
		result = append(result, sc)
	}
	return result, nil
}

func (s *scopeStore) AssignToKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	if len(scopeNames) == 0 {
		return nil
	}

	// Look up the key to get its tenant_id.
	var k keyModel
	err := s.mdb.NewFind(&k).
		Filter(bson.M{"_id": keyID.String()}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return errNotFound("key")
		}
		return fmt.Errorf("keysmith/mongo: lookup key: %w", err)
	}

	kid := keyID.String()
	for _, name := range scopeNames {
		var sc scopeModel
		err := s.mdb.NewFind(&sc).
			Filter(bson.M{"tenant_id": k.TenantID, "name": name}).
			Scan(ctx)
		if err != nil {
			if isNoDocuments(err) {
				return errNotFound("scope")
			}
			return fmt.Errorf("keysmith/mongo: lookup scope %q: %w", name, err)
		}

		m := &keyScopeModel{KeyID: kid, ScopeID: sc.ID}
		// Use upsert to handle duplicates gracefully.
		_, err = s.mdb.NewUpdate(m).
			Filter(bson.M{"key_id": kid, "scope_id": sc.ID}).
			Upsert().
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("keysmith/mongo: assign scope: %w", err)
		}
	}
	return nil
}

func (s *scopeStore) RemoveFromKey(ctx context.Context, keyID id.KeyID, scopeNames []string) error {
	if len(scopeNames) == 0 {
		return nil
	}

	// Look up the key to get its tenant_id.
	var k keyModel
	err := s.mdb.NewFind(&k).
		Filter(bson.M{"_id": keyID.String()}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return errNotFound("key")
		}
		return fmt.Errorf("keysmith/mongo: lookup key for remove: %w", err)
	}

	kid := keyID.String()
	for _, name := range scopeNames {
		var sc scopeModel
		err := s.mdb.NewFind(&sc).
			Filter(bson.M{"tenant_id": k.TenantID, "name": name}).
			Scan(ctx)
		if err != nil {
			if isNoDocuments(err) {
				continue // Skip scopes that don't exist.
			}
			return fmt.Errorf("keysmith/mongo: lookup scope %q: %w", name, err)
		}

		_, err = s.mdb.NewDelete((*keyScopeModel)(nil)).
			Filter(bson.M{"key_id": kid, "scope_id": sc.ID}).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("keysmith/mongo: remove scope: %w", err)
		}
	}
	return nil
}
