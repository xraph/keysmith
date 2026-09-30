package mongo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/mongodriver"
	"github.com/xraph/grove/drivers/mongodriver/mongomigrate"
	"github.com/xraph/grove/migrate"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/rotation"
)

// openTestStore opens a throwaway database on KEYSMITH_TEST_MONGO_URI and
// drops it afterwards. `make test-backends` sets the URI.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	uri := os.Getenv("KEYSMITH_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("KEYSMITH_TEST_MONGO_URI not set")
	}
	ctx := context.Background()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	md := mongodriver.New()
	require.NoError(t, md.Open(ctx, uri, mongodriver.WithDatabase("ks_"+hex.EncodeToString(b)), mongodriver.WithTimeout(10*time.Second)))
	db, err := grove.Open(md)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = md.Database().Drop(context.Background())
		_ = db.Close()
	})
	s := New(db)
	require.NoError(t, s.Migrate(ctx))
	return s
}

func migrationByVersion(t *testing.T, version string) *migrate.Migration {
	t.Helper()
	for _, m := range Migrations.Migrations() {
		if m.Version == version {
			return m
		}
	}
	t.Fatalf("no migration with version %s", version)
	return nil
}

// Rotations recorded before the grace fix have no old hint (an empty string,
// or no field at all on documents written before the hint existed) and a
// window the engine never honoured. The migration closes those windows and
// leaves rotations recorded since alone.
func TestCloseLegacyGraceWindows(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	keyID := id.NewKeyID()
	created := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	ends := created.Add(24 * time.Hour)

	emptyHint := &rotation.Record{
		ID: id.NewRotationID(), KeyID: keyID, TenantID: "t1",
		OldKeyHash: "hash-empty", NewKeyHash: "hash-cur", Reason: rotation.ReasonCompromise,
		GraceTTL: 24 * time.Hour, GraceEnds: ends, CreatedAt: created,
	}
	modern := &rotation.Record{
		ID: id.NewRotationID(), KeyID: keyID, TenantID: "t1",
		OldKeyHash: "hash-modern", NewKeyHash: "hash-cur", OldHint: "b7c2", NewHint: "a3f8",
		Reason: rotation.ReasonManual, GraceTTL: 24 * time.Hour, GraceEnds: ends, CreatedAt: created,
	}
	require.NoError(t, s.Rotations().Create(ctx, emptyHint))
	require.NoError(t, s.Rotations().Create(ctx, modern))
	// A document from before the hint fields existed.
	noHintID := id.NewRotationID()
	_, err := s.mdb.Collection(colRotations).InsertOne(ctx, bson.M{
		"_id": noHintID.String(), "key_id": keyID.String(), "tenant_id": "t1",
		"old_key_hash": "hash-missing", "new_key_hash": "hash-cur", "reason": "compromise",
		"grace_ttl_ms": int64(86400000), "grace_ends": ends, "rotated_by": "", "created_at": created,
	})
	require.NoError(t, err)

	// The $expr comparison is only like with like if grove stores both times
	// as BSON dates.
	var raw bson.Raw
	require.NoError(t, s.mdb.Collection(colRotations).FindOne(ctx, bson.M{"_id": emptyHint.ID.String()}).Decode(&raw))
	assert.Equal(t, bson.TypeDateTime, raw.Lookup("grace_ends").Type)
	assert.Equal(t, bson.TypeDateTime, raw.Lookup("created_at").Type)

	require.NoError(t, migrationByVersion(t, "20260930000003").Up(ctx, mongomigrate.New(s.mdb)))

	for _, rid := range []id.RotationID{emptyHint.ID, noHintID} {
		got, gErr := s.Rotations().Get(ctx, rid)
		require.NoError(t, gErr)
		assert.True(t, got.GraceEnds.Equal(got.CreatedAt), "legacy window closed: grace_ends %v, created_at %v", got.GraceEnds, got.CreatedAt)
	}
	got, err := s.Rotations().Get(ctx, modern.ID)
	require.NoError(t, err)
	assert.True(t, got.GraceEnds.Equal(ends), "a hinted rotation keeps its window")
}
