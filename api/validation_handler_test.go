package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

type downKeys struct {
	key.Store
	err error
}

func (d downKeys) GetByHash(context.Context, string) (*key.Key, error) { return nil, d.err }

type downStore struct {
	store.Store
	keys key.Store
}

func (s downStore) Keys() key.Store { return s.keys }

// An unknown key is the caller's mistake (401). A key store that cannot
// answer is not: it is a 500 that keeps the store's error text to itself.
func TestValidateKeyHandlerSeparatesUnknownKeyFromOutage(t *testing.T) {
	s := memory.New()

	healthy, err := keysmith.NewEngine(keysmith.WithStore(s))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	body := map[string]any{"raw_key": "sk_live_unknownunknownunknown"}
	send(t, New(healthy, nil).Handler(), http.MethodPost, "/v1/keys/validate", body, http.StatusUnauthorized, "unknown key")

	down := errors.New("keysmith/postgres: get key by hash: dial tcp 10.0.0.7:5432: connection refused")
	broken, err := keysmith.NewEngine(keysmith.WithStore(downStore{Store: s, keys: downKeys{Store: s.Keys(), err: down}}))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	rec := send(t, New(broken, nil).Handler(), http.MethodPost, "/v1/keys/validate", body, http.StatusInternalServerError, "store outage")
	if strings.Contains(rec.Body.String(), "10.0.0.7") {
		t.Fatalf("500 body echoes the store error: %s", rec.Body.String())
	}
}
