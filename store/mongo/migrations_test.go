package mongo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/usage"
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

// planOf explains a newest-first find on col, as the usage and rotation
// pages run it, and answers every stage and index name in the winning plan.
func planOf(t *testing.T, s *Store, col string, filter bson.D, limit, skip int64) (stages, indexes []string) {
	t.Helper()
	cmd := bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "find", Value: col},
			{Key: "filter", Value: filter},
			{Key: "sort", Value: bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}},
			{Key: "limit", Value: limit},
			{Key: "skip", Value: skip},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}
	var out bson.D
	require.NoError(t, s.mdb.Database().RunCommand(context.Background(), cmd).Decode(&out))
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case bson.D:
			for _, e := range x {
				switch e.Key {
				case "stage":
					stages = append(stages, fmt.Sprint(e.Value))
				case "indexName":
					indexes = append(indexes, fmt.Sprint(e.Value))
				}
				walk(e.Value)
			}
		case bson.M:
			for k, e := range x {
				walk(bson.D{{Key: k, Value: e}})
			}
		case bson.A:
			for _, e := range x {
				walk(e)
			}
		}
	}
	var planner any
	for _, e := range out {
		if e.Key == "queryPlanner" {
			planner = e.Value
		}
	}
	require.NotNil(t, planner, "explain answered no queryPlanner")
	for _, e := range planner.(bson.D) {
		if e.Key == "winningPlan" {
			walk(e.Value)
		}
	}
	require.NotEmpty(t, stages, "no winning plan in the explain output")
	return stages, indexes
}

// MongoDB cannot sort a prefix of an index and finish the rest in memory, so
// a sort on {created_at, _id} that no index covers reads and sorts the
// tenant's whole history, even for one row. These indexes let the newest
// page, and usageRecorded's single row, come straight off the index.
func TestStableSortIndexesServeNewestFirstPages(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	keyID := id.NewKeyID()
	at := time.Now().UTC().Truncate(time.Millisecond)
	for i := range 20 {
		require.NoError(t, s.Usages().Record(ctx, &usage.Record{
			ID: id.NewUsageID(), KeyID: keyID, TenantID: "t1", Endpoint: "/x", Method: "GET",
			StatusCode: 200, CreatedAt: at.Add(-time.Duration(i) * time.Minute),
		}))
		require.NoError(t, s.Rotations().Create(ctx, &rotation.Record{
			ID: id.NewRotationID(), KeyID: keyID, TenantID: "t1", OldKeyHash: "h", NewKeyHash: "n",
			Reason: rotation.ReasonManual, GraceEnds: at, CreatedAt: at.Add(-time.Duration(i) * time.Minute),
		}))
	}
	since := at.Add(-time.Hour)
	for _, tc := range []struct {
		name   string
		col    string
		filter bson.D
		index  string
	}{
		{"usage by tenant", colUsage, bson.D{{Key: "tenant_id", Value: "t1"}}, "tenant_id_1_created_at_-1__id_-1"},
		{"usage by tenant and range", colUsage, bson.D{{Key: "tenant_id", Value: "t1"}, {Key: "created_at", Value: bson.D{{Key: "$gte", Value: since}}}}, "tenant_id_1_created_at_-1__id_-1"},
		{"usage by key", colUsage, bson.D{{Key: "key_id", Value: keyID.String()}, {Key: "tenant_id", Value: "t1"}}, ""},
		{"rotations by tenant", colRotations, bson.D{{Key: "tenant_id", Value: "t1"}}, "tenant_id_1_created_at_-1__id_-1"},
		{"rotations by key", colRotations, bson.D{{Key: "key_id", Value: keyID.String()}, {Key: "tenant_id", Value: "t1"}}, ""},
	} {
		for _, page := range []struct{ limit, skip int64 }{{1, 0}, {26, 25}} {
			stages, indexes := planOf(t, s, tc.col, tc.filter, page.limit, page.skip)
			t.Logf("%s, limit %d skip %d: stages %v, indexes %v", tc.name, page.limit, page.skip, stages, indexes)
			assert.NotContains(t, stages, "SORT", "%s %v: %v", tc.name, page, stages)
			assert.Contains(t, stages, "IXSCAN", "%s %v: %v", tc.name, page, stages)
			if tc.index != "" {
				assert.Contains(t, indexes, tc.index, "%s %v", tc.name, page)
			} else {
				// Either stable-sort index serves a key and tenant filter;
				// the planner picks whichever is narrower.
				assert.Subset(t, []string{"tenant_id_1_created_at_-1__id_-1", "key_id_1_created_at_-1__id_-1"}, indexes, "%s %v", tc.name, page)
			}
		}
	}
}

func indexNames(t *testing.T, s *Store, col string) []string {
	t.Helper()
	specs, err := s.mdb.Collection(col).Indexes().ListSpecifications(context.Background())
	require.NoError(t, err)
	names := make([]string, 0, len(specs))
	for _, sp := range specs {
		names = append(names, sp.Name)
	}
	return names
}

// Deployments that applied the usage and rotation migrations before the
// stable sort get the indexes from this one.
func TestStableSortIndexMigrationAddsAndDropsItsIndexes(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	m := migrationByVersion(t, "20261005000001")
	exec := mongomigrate.New(s.mdb)
	want := []string{"tenant_id_1_created_at_-1__id_-1", "key_id_1_created_at_-1__id_-1"}

	require.NoError(t, m.Down(ctx, exec))
	require.NoError(t, m.Down(ctx, exec), "a second down finds nothing to drop and is fine")
	for _, col := range []string{colUsage, colRotations} {
		for _, name := range want {
			assert.NotContains(t, indexNames(t, s, col), name, col)
		}
	}

	require.NoError(t, m.Up(ctx, exec))
	require.NoError(t, m.Up(ctx, exec), "up again is a no-op")
	for _, col := range []string{colUsage, colRotations} {
		assert.Subset(t, indexNames(t, s, col), want, col)
	}
}

// Keys written before the version field existed have none. The store reads
// them as version 0 and its version filter matches them, so a database that
// only ever ran Store.Migrate works. The grove migration fills the field in.
func TestKeysWithoutAVersion(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	legacy := func() *key.Key {
		now := time.Now()
		k := &key.Key{
			ID: id.NewKeyID(), TenantID: "t1", AppID: "app", Name: "k", Prefix: "sk", Hint: "a3f8",
			KeyHash: "hash-" + id.NewKeyID().String(), Environment: key.EnvLive, State: key.StateActive,
			CreatedAt: now, UpdatedAt: now,
		}
		require.NoError(t, s.Keys().Create(ctx, k))
		_, err := s.mdb.Collection(colKeys).UpdateOne(ctx, bson.M{"_id": k.ID.String()}, bson.M{"$unset": bson.M{"version": ""}})
		require.NoError(t, err)
		return k
	}
	version := func(k *key.Key) bson.RawValue {
		var raw bson.Raw
		require.NoError(t, s.mdb.Collection(colKeys).FindOne(ctx, bson.M{"_id": k.ID.String()}).Decode(&raw))
		return raw.Lookup("version")
	}

	checked := legacy()
	got, err := s.Keys().Get(ctx, checked.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), got.Version)
	got.Name = "renamed"
	require.NoError(t, s.Keys().UpdateIfVersion(ctx, got, 0))
	assert.Equal(t, int64(1), version(checked).Int64())
	require.ErrorIs(t, s.Keys().UpdateIfVersion(ctx, got, 0), store.ErrKeyConflict)

	plain := legacy()
	require.NoError(t, s.Keys().Update(ctx, plain))
	assert.Equal(t, int64(1), version(plain).Int64(), "$inc starts a missing version from 0")

	untouched := legacy()
	assert.Equal(t, bson.Type(0), version(untouched).Type, "the field is gone before the migration")
	require.NoError(t, migrationByVersion(t, "20261007000001").Up(ctx, mongomigrate.New(s.mdb)))
	assert.Equal(t, bson.TypeInt64, version(untouched).Type)
	assert.Equal(t, int64(0), version(untouched).Int64())
	assert.Equal(t, int64(1), version(checked).Int64(), "the migration leaves a counted version alone")
}
