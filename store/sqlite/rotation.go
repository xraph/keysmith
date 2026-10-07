package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/rotation"
)

type rotationStore struct {
	sdb *sqlitedriver.SqliteDB
}

func (s *rotationStore) Create(ctx context.Context, rec *rotation.Record) error {
	m := rotationToModel(rec)
	_, err := s.sdb.NewInsert(m).Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: create rotation: %w", err)
	}
	return nil
}

func (s *rotationStore) Get(ctx context.Context, rotID id.RotationID) (*rotation.Record, error) {
	m := new(rotationModel)
	err := s.sdb.NewSelect(m).Where("id = ?", rotID.String()).Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("rotation")
		}
		return nil, fmt.Errorf("keysmith/sqlite: get rotation: %w", err)
	}
	return rotationFromModel(m)
}

func (s *rotationStore) List(ctx context.Context, filter *rotation.ListFilter) ([]*rotation.Record, error) {
	var models []rotationModel
	q := s.sdb.NewSelect(&models).OrderExpr("created_at DESC, id DESC")

	if filter != nil {
		if filter.KeyID != nil {
			q = q.Where("key_id = ?", filter.KeyID.String())
		}
		if filter.TenantID != "" {
			q = q.Where("tenant_id = ?", filter.TenantID)
		}
		if filter.Reason != "" {
			q = q.Where("reason = ?", string(filter.Reason))
		}
		if filter.Limit > 0 {
			q = q.Limit(filter.Limit)
		}
		if filter.Offset > 0 {
			q = q.Offset(filter.Offset)
		}
	}

	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: list rotations: %w", err)
	}

	result := make([]*rotation.Record, 0, len(models))
	for i := range models {
		rec, err := rotationFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: convert rotation: %w", err)
		}
		result = append(result, rec)
	}
	return result, nil
}

func (s *rotationStore) ListPendingGrace(ctx context.Context, now time.Time) ([]*rotation.Record, error) {
	var models []rotationModel
	err := s.sdb.NewSelect(&models).
		Where("grace_ends > ?", dbTime(now)).
		OrderExpr("grace_ends ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("keysmith/sqlite: list pending grace: %w", err)
	}

	result := make([]*rotation.Record, 0, len(models))
	for i := range models {
		rec, err := rotationFromModel(&models[i])
		if err != nil {
			return nil, fmt.Errorf("keysmith/sqlite: convert rotation: %w", err)
		}
		result = append(result, rec)
	}
	return result, nil
}

func (s *rotationStore) LatestForKey(ctx context.Context, keyID id.KeyID) (*rotation.Record, error) {
	m := new(rotationModel)
	err := s.sdb.NewSelect(m).
		Where("key_id = ?", keyID.String()).
		OrderExpr("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("rotation")
		}
		return nil, fmt.Errorf("keysmith/sqlite: latest for key: %w", err)
	}
	return rotationFromModel(m)
}

func (s *rotationStore) GetInGraceByOldHash(ctx context.Context, hash string, now time.Time) (*rotation.Record, error) {
	m := new(rotationModel)
	err := s.sdb.NewSelect(m).
		Where("old_key_hash = ?", hash).
		Where("grace_ends > ?", dbTime(now)).
		OrderExpr("grace_ends DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if isNoRows(err) {
			return nil, errNotFound("rotation")
		}
		return nil, fmt.Errorf("keysmith/sqlite: get in-grace rotation: %w", err)
	}
	return rotationFromModel(m)
}

func (s *rotationStore) EndGrace(ctx context.Context, keyID id.KeyID, at time.Time) (int64, error) {
	res, err := s.sdb.NewUpdate((*rotationModel)(nil)).
		Set("grace_ends = ?", dbTime(at)).
		Where("key_id = ?", keyID.String()).
		Where("grace_ends > ?", dbTime(at)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("keysmith/sqlite: end grace: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *rotationStore) EndGraceByID(ctx context.Context, rotID id.RotationID, at time.Time) error {
	_, err := s.sdb.NewUpdate((*rotationModel)(nil)).
		Set("grace_ends = ?", dbTime(at)).
		Where("id = ?", rotID.String()).
		Where("grace_ends > ?", dbTime(at)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("keysmith/sqlite: end grace of rotation: %w", err)
	}
	return nil
}
