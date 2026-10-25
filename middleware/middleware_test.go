package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/middleware"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

// downKeys fails the lookup by hash, as a key store does when the database
// is unreachable.
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

func newEngine(t *testing.T, s store.Store) (*keysmith.Engine, context.Context) {
	t.Helper()
	eng, err := keysmith.NewEngine(keysmith.WithStore(s))
	require.NoError(t, err)
	return eng, keysmith.WithTenant(context.Background(), "app", "t1")
}

func call(t *testing.T, eng *keysmith.Engine, raw string) *httptest.ResponseRecorder {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	middleware.APIKeyAuth(eng)(next).ServeHTTP(rec, req)
	return rec
}

func TestAPIKeyAuthStatuses(t *testing.T) {
	s := memory.New()
	eng, ctx := newEngine(t, s)
	good, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive})
	require.NoError(t, err)

	t.Run("valid key passes", func(t *testing.T) {
		assert.Equal(t, http.StatusNoContent, call(t, eng, good.RawKey).Code)
	})

	t.Run("unknown key is 401", func(t *testing.T) {
		rec := call(t, eng, "sk_live_unknownunknownunknown")
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Contains(t, rec.Body.String(), keysmith.ErrInvalidKey.Error())
	})

	t.Run("revoked key is 401", func(t *testing.T) {
		revoked, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "r", Prefix: "sk", Environment: key.EnvLive})
		require.NoError(t, err)
		require.NoError(t, eng.RevokeKey(ctx, revoked.Key.ID, "gone"))
		assert.Equal(t, http.StatusUnauthorized, call(t, eng, revoked.RawKey).Code,
			"a revoked key reports as inactive, which this middleware has always answered with 401")
	})
}

// A key store that is down is the server's problem, not the caller's. A 401
// tells a client its credentials are wrong, and many of them stop retrying.
func TestAPIKeyAuthStoreOutageIs5xx(t *testing.T) {
	s := memory.New()
	eng, ctx := newEngine(t, s)
	good, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive})
	require.NoError(t, err)

	broken, _ := newEngine(t, downStore{Store: s, keys: downKeys{Store: s.Keys(), err: errors.New("keysmith/postgres: get key by hash: dial tcp 10.0.0.7:5432: connection refused")}})
	rec := call(t, broken, good.RawKey)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, strings.Contains(rec.Body.String(), "10.0.0.7"), "the body must not echo the store's error text: %s", rec.Body.String())
}

func TestAPIKeyAuthMissingKeyIs401(t *testing.T) {
	eng, _ := newEngine(t, memory.New())
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	middleware.APIKeyAuth(eng)(http.NotFoundHandler()).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
