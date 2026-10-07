package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/grove/drivers/mongodriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/groveset"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store"
)

type keyStore struct {
	mdb *mongodriver.MongoDB
}

func (s *keyStore) Create(ctx context.Context, k *key.Key) error {
	m := keyToModel(k)
	_, err := s.mdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: create key: %w", err)
	}
	return nil
}

func (s *keyStore) Get(ctx context.Context, keyID id.KeyID) (*key.Key, error) {
	var m keyModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"_id": keyID.String()}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/mongo: get key: %w", err)
	}
	return keyFromModel(&m)
}

func (s *keyStore) GetByHash(ctx context.Context, hash string) (*key.Key, error) {
	var m keyModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"key_hash": hash}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/mongo: get key by hash: %w", err)
	}
	return keyFromModel(&m)
}

func (s *keyStore) GetByPrefix(ctx context.Context, prefix, hint string) (*key.Key, error) {
	var m keyModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"prefix": prefix, "hint": hint}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, errNotFound("key")
		}
		return nil, fmt.Errorf("keysmith/mongo: get key by prefix: %w", err)
	}
	return keyFromModel(&m)
}

// Update sets every field the model holds, as a model-based update would,
// except version, which it counts up with $inc in the same update. A
// model-based update builds only $set, so groveset lists the fields.
func (s *keyStore) Update(ctx context.Context, k *key.Key) error {
	cols, err := groveset.Columns(keyToModel(k), "version")
	if err != nil {
		return fmt.Errorf("keysmith/mongo: update key: %w", err)
	}
	set := make(bson.M, len(cols))
	for _, c := range cols {
		set[c.Name] = c.Value
	}
	res, err := s.mdb.NewUpdate((*keyModel)(nil)).
		Filter(bson.M{"_id": k.ID.String()}).
		SetUpdate(bson.M{"$set": set, "$inc": bson.M{"version": int64(1)}}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: update key: %w", err)
	}
	if res.MatchedCount() == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) UpdateIfVersion(ctx context.Context, k *key.Key, version int64) error {
	m := keyToModel(k)
	m.Version = version + 1
	res, err := s.mdb.NewUpdate(m).
		Filter(versionFilter(m.ID, version)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: update key: %w", err)
	}
	if res.MatchedCount() == 0 {
		return s.missingOrConflict(ctx, k.ID)
	}
	k.Version = version + 1
	return nil
}

// versionFilter matches the key stored at version. A document written before
// the version field existed has none and reads back as version 0, so version
// 0 matches a missing field too. The key_version migration fills the field
// in, but Store.Migrate only builds indexes and never runs it.
func versionFilter(keyID string, version int64) bson.M {
	if version == 0 {
		return bson.M{"_id": keyID, "$or": bson.A{
			bson.M{"version": int64(0)},
			bson.M{"version": bson.M{"$exists": false}},
		}}
	}
	return bson.M{"_id": keyID, "version": version}
}

// missingOrConflict tells a version-checked update that matched nothing
// apart: the key is gone, or it is there at another version.
func (s *keyStore) missingOrConflict(ctx context.Context, keyID id.KeyID) error {
	n, err := s.mdb.NewFind((*keyModel)(nil)).
		Filter(bson.M{"_id": keyID.String()}).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: look up key after a refused update: %w", err)
	}
	if n == 0 {
		return errNotFound("key")
	}
	return store.ErrKeyConflict
}

func (s *keyStore) UpdateState(ctx context.Context, keyID id.KeyID, state key.State) error {
	res, err := s.mdb.NewUpdate((*keyModel)(nil)).
		Filter(bson.M{"_id": keyID.String()}).
		SetUpdate(bson.M{
			"$set": bson.M{"state": string(state), "updated_at": now()},
			"$inc": bson.M{"version": int64(1)},
		}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: update key state: %w", err)
	}
	if res.MatchedCount() == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) UpdateLastUsed(ctx context.Context, keyID id.KeyID, at time.Time) error {
	res, err := s.mdb.NewUpdate((*keyModel)(nil)).
		Filter(bson.M{"_id": keyID.String()}).
		Set("last_used_at", at).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: update last used: %w", err)
	}
	if res.MatchedCount() == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) Delete(ctx context.Context, keyID id.KeyID) error {
	res, err := s.mdb.NewDelete((*keyModel)(nil)).
		Filter(bson.M{"_id": keyID.String()}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: delete key: %w", err)
	}
	if res.DeletedCount() == 0 {
		return errNotFound("key")
	}
	return nil
}

func (s *keyStore) List(ctx context.Context, filter *key.ListFilter) ([]*key.Key, error) {
	var models []keyModel

	f := bson.M{}
	if filter != nil {
		if filter.TenantID != "" {
			f["tenant_id"] = filter.TenantID
		}
		if filter.Environment != "" {
			f["environment"] = string(filter.Environment)
		}
		if filter.State != "" {
			f["state"] = string(filter.State)
		}
		if filter.PolicyID != nil {
			f["policy_id"] = filter.PolicyID.String()
		}
		if filter.CreatedBy != "" {
			f["created_by"] = filter.CreatedBy
		}
	}

	q := s.mdb.NewFind(&models).
		Filter(f).
		Sort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}})

	if filter != nil {
		if filter.Limit > 0 {
			q = q.Limit(int64(filter.Limit))
		}
		if filter.Offset > 0 {
			q = q.Skip(int64(filter.Offset))
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/mongo: list keys: %w", err)
	}

	result := make([]*key.Key, 0, len(models))
	for i := range models {
		k, err := keyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert key: %w", err)
		}
		result = append(result, k)
	}
	return result, nil
}

func (s *keyStore) Count(ctx context.Context, filter *key.ListFilter) (int64, error) {
	f := bson.M{}
	if filter != nil {
		if filter.TenantID != "" {
			f["tenant_id"] = filter.TenantID
		}
		if filter.Environment != "" {
			f["environment"] = string(filter.Environment)
		}
		if filter.State != "" {
			f["state"] = string(filter.State)
		}
		if filter.PolicyID != nil {
			f["policy_id"] = filter.PolicyID.String()
		}
		if filter.CreatedBy != "" {
			f["created_by"] = filter.CreatedBy
		}
	}

	count, err := s.mdb.NewFind((*keyModel)(nil)).
		Filter(f).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/mongo: count keys: %w", err)
	}
	return count, nil
}

func (s *keyStore) ListExpired(ctx context.Context, before time.Time) ([]*key.Key, error) {
	var models []keyModel
	err := s.mdb.NewFind(&models).
		Filter(bson.M{
			"state":      string(key.StateActive),
			"expires_at": bson.M{"$ne": nil, "$lt": before},
		}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/mongo: list expired: %w", err)
	}

	result := make([]*key.Key, 0, len(models))
	for i := range models {
		k, err := keyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert key: %w", err)
		}
		result = append(result, k)
	}
	return result, nil
}

func (s *keyStore) ListByPolicy(ctx context.Context, policyID id.PolicyID) ([]*key.Key, error) {
	var models []keyModel
	err := s.mdb.NewFind(&models).
		Filter(bson.M{"policy_id": policyID.String()}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/mongo: list by policy: %w", err)
	}

	result := make([]*key.Key, 0, len(models))
	for i := range models {
		k, err := keyFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert key: %w", err)
		}
		result = append(result, k)
	}
	return result, nil
}

func (s *keyStore) DeleteByTenant(ctx context.Context, tenantID string) error {
	_, err := s.mdb.NewDelete((*keyModel)(nil)).
		Many().
		Filter(bson.M{"tenant_id": tenantID}).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: delete by tenant: %w", err)
	}
	return nil
}
