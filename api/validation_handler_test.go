package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// A request whose context is cancelled or past its deadline gets a 5xx, even
// when the key is unknown. The store's miss must not leak out as a 404 or a
// 401: the engine never judged the key.
func TestValidateKeyHandlerCancelledContextIs5xx(t *testing.T) {
	eng, err := keysmith.NewEngine(keysmith.WithStore(memory.New()))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	h := New(eng, nil).Handler()

	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for name, ctx := range map[string]context.Context{"cancelled": cancelled, "deadline exceeded": expired} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/keys/validate",
				strings.NewReader(`{"raw_key":"sk_live_unknownunknownunknown"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code < 500 || rec.Code > 599 {
				t.Fatalf("status %d, want 5xx, body %s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "not found") {
				t.Fatalf("body carries a not-found verdict: %s", rec.Body.String())
			}
		})
	}
}
