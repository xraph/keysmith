package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/grove/drivers/mongodriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/rotation"
)

type rotationStore struct {
	mdb *mongodriver.MongoDB
}

func (s *rotationStore) Create(ctx context.Context, rec *rotation.Record) error {
	m := rotationToModel(rec)
	_, err := s.mdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/mongo: create rotation: %w", err)
	}
	return nil
}

func (s *rotationStore) Get(ctx context.Context, rotID id.RotationID) (*rotation.Record, error) {
	var m rotationModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"_id": rotID.String()}).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, errNotFound("rotation")
		}
		return nil, fmt.Errorf("keysmith/mongo: get rotation: %w", err)
	}
	return rotationFromModel(&m)
}

func (s *rotationStore) List(ctx context.Context, filter *rotation.ListFilter) ([]*rotation.Record, error) {
	var models []rotationModel

	f := bson.M{}
	if filter != nil {
		if filter.KeyID != nil {
			f["key_id"] = filter.KeyID.String()
		}
		if filter.TenantID != "" {
			f["tenant_id"] = filter.TenantID
		}
		if filter.Reason != "" {
			f["reason"] = string(filter.Reason)
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
		return nil, fmt.Errorf("keysmith/mongo: list rotations: %w", err)
	}

	result := make([]*rotation.Record, 0, len(models))
	for i := range models {
		rec, err := rotationFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert rotation: %w", err)
		}
		result = append(result, rec)
	}
	return result, nil
}

func (s *rotationStore) ListPendingGrace(ctx context.Context, now time.Time) ([]*rotation.Record, error) {
	var models []rotationModel
	err := s.mdb.NewFind(&models).
		Filter(bson.M{"grace_ends": bson.M{"$gt": now}}).
		Sort(bson.D{{Key: "grace_ends", Value: 1}}).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/mongo: list pending grace: %w", err)
	}

	result := make([]*rotation.Record, 0, len(models))
	for i := range models {
		rec, err := rotationFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/mongo: convert rotation: %w", err)
		}
		result = append(result, rec)
	}
	return result, nil
}

func (s *rotationStore) LatestForKey(ctx context.Context, keyID id.KeyID) (*rotation.Record, error) {
	var m rotationModel
	err := s.mdb.NewFind(&m).
		Filter(bson.M{"key_id": keyID.String()}).
		Sort(bson.D{{Key: "created_at", Value: -1}}).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if isNoDocuments(err) {
			return nil, errNotFound("rotation")
		}
		return nil, fmt.Errorf("keysmith/mongo: latest for key: %w", err)
	}
	return rotationFromModel(&m)
}
