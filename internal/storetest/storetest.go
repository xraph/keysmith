// Package storetest runs a test against every keysmith store backend that is
// available. Memory and sqlite always run. Postgres runs when
// KEYSMITH_TEST_PG_DSN is set and mongo when KEYSMITH_TEST_MONGO_URI is set;
// `make test-backends` sets both.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/mongodriver"
	"github.com/xraph/grove/drivers/pgdriver"
	"github.com/xraph/grove/drivers/sqlitedriver"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
	mongostore "github.com/xraph/keysmith/store/mongo"
	pgstore "github.com/xraph/keysmith/store/postgres"
	sqlitestore "github.com/xraph/keysmith/store/sqlite"
)

type backend struct {
	name string
	open func(t *testing.T) store.Store
}

func backends() []backend {
	bs := []backend{
		{"memory", func(*testing.T) store.Store { return memory.New() }},
		{"sqlite", OpenSQLite},
	}
	if dsn := os.Getenv("KEYSMITH_TEST_PG_DSN"); dsn != "" {
		bs = append(bs, backend{"postgres", func(t *testing.T) store.Store { return openPostgres(t, dsn) }})
	}
	if uri := os.Getenv("KEYSMITH_TEST_MONGO_URI"); uri != "" {
		bs = append(bs, backend{"mongo", func(t *testing.T) store.Store { return openMongo(t, uri) }})
	}
	return bs
}

// Each runs fn once per available backend, each with a fresh migrated store.
func Each(t *testing.T, fn func(t *testing.T, s store.Store)) {
	t.Helper()
	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			s := b.open(t)
			if err := s.Migrate(context.Background()); err != nil {
				t.Fatalf("%s migrate: %v", b.name, err)
			}
			fn(t, s)
		})
	}
}

// Name returns the backend name of the subtest t is running in: the last
// path element of the test name, such as "sqlite".
func Name(t *testing.T) string {
	t.Helper()
	n := t.Name()
	for i := len(n) - 1; i >= 0; i-- {
		if n[i] == '/' {
			return n[i+1:]
		}
	}
	return n
}

func suffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// replaceDB swaps the database name in a postgres:// DSN.
func replaceDB(t *testing.T, dsn, name string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse postgres DSN: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// OpenSQLite opens an unmigrated sqlite store in a fresh temporary
// directory, with the DSN the store documents.
//
// The engine's ValidateKey writes last_used_at from a goroutine nobody
// waits for, and on sqlite that write can still be waiting for the write
// lock when the test ends. If it outlives the cleanup it reopens the WAL
// files after the directory is emptied, and the temporary directory fails to
// remove. So the store this returns counts those writes, and the cleanup
// refuses new ones and waits for the rest before it closes the database.
func OpenSQLite(t *testing.T) store.Store {
	t.Helper()
	sdb := sqlitedriver.New()
	if err := sdb.Open(context.Background(), sqlitestore.DSN(filepath.Join(t.TempDir(), "keysmith.db"))); err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	db, err := grove.Open(sdb)
	if err != nil {
		t.Fatalf("grove open: %v", err)
	}
	g := &lastUsedGate{}
	t.Cleanup(func() {
		g.close()
		_ = db.Close()
	})
	return gatedStore{Store: sqlitestore.New(db), gate: g}
}

var errStoreClosed = errors.New("storetest: store closed")

// lastUsedGate tracks UpdateLastUsed calls in flight. Once closed it turns
// new calls away, so none can start after close has waited for the rest.
type lastUsedGate struct {
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func (g *lastUsedGate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.wg.Add(1)
	return true
}

func (g *lastUsedGate) close() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
	g.wg.Wait()
}

type gatedStore struct {
	store.Store
	gate *lastUsedGate
}

func (s gatedStore) Keys() key.Store { return gatedKeys{Store: s.Store.Keys(), gate: s.gate} }

type gatedKeys struct {
	key.Store
	gate *lastUsedGate
}

func (k gatedKeys) UpdateLastUsed(ctx context.Context, keyID id.KeyID, at time.Time) error {
	if !k.gate.enter() {
		return errStoreClosed
	}
	defer k.gate.wg.Done()
	return k.Store.UpdateLastUsed(ctx, keyID, at)
}

// openPostgres creates a throwaway database per test, so tests never share
// rows, and drops it afterwards.
func openPostgres(t *testing.T, dsn string) store.Store {
	t.Helper()
	ctx := context.Background()
	admin := pgdriver.New()
	if err := admin.Open(ctx, dsn); err != nil {
		t.Fatalf("postgres admin open: %v", err)
	}
	name := "ks_" + suffix()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	db := pgdriver.New()
	if err := db.Open(ctx, replaceDB(t, dsn, name)); err != nil {
		t.Fatalf("postgres open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close()
	})
	return pgstore.New(db)
}

func openMongo(t *testing.T, uri string) store.Store {
	t.Helper()
	ctx := context.Background()
	md := mongodriver.New()
	name := "ks_" + suffix()
	if err := md.Open(ctx, uri, mongodriver.WithDatabase(name), mongodriver.WithTimeout(10*time.Second)); err != nil {
		t.Fatalf("mongo open: %v", err)
	}
	db, err := grove.Open(md)
	if err != nil {
		t.Fatalf("grove open: %v", err)
	}
	t.Cleanup(func() {
		_ = md.Collection("keysmith_keys").Database().Drop(context.Background())
		_ = db.Close()
	})
	return mongostore.New(db)
}
