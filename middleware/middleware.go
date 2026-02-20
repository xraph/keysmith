// Package middleware provides Forge-compatible middleware for API key validation.
package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/xraph/keysmith"
)

type contextKey struct{}

// ResultFromContext extracts the ValidationResult from the context.
func ResultFromContext(ctx context.Context) (*keysmith.ValidationResult, bool) {
	v, ok := ctx.Value(contextKey{}).(*keysmith.ValidationResult)
	return v, ok
}

// APIKeyAuth returns middleware that validates API keys from the
// Authorization header (Bearer token) or X-API-Key header.
func APIKeyAuth(eng *keysmith.Engine) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey := extractKey(r)
			if rawKey == "" {
				http.Error(w, `{"error":"missing API key"}`, http.StatusUnauthorized)
				return
			}

			result, err := eng.ValidateKey(r.Context(), rawKey)
			if err != nil {
				code := http.StatusUnauthorized
				switch {
				case errors.Is(err, keysmith.ErrRateLimited):
					code = http.StatusTooManyRequests
				case errors.Is(err, keysmith.ErrKeyExpired),
					errors.Is(err, keysmith.ErrKeyRevoked),
					errors.Is(err, keysmith.ErrKeySuspended):
					code = http.StatusForbidden
				}
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), code)
				return
			}

			ctx := context.WithValue(r.Context(), contextKey{}, result)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScopes returns middleware that checks the validated key has all
// of the specified scopes.
func RequireScopes(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result, ok := ResultFromContext(r.Context())
			if !ok {
				http.Error(w, `{"error":"no API key context"}`, http.StatusUnauthorized)
				return
			}

			scopeSet := make(map[string]struct{}, len(result.Scopes))
			for _, s := range result.Scopes {
				scopeSet[s] = struct{}{}
			}

			for _, required := range scopes {
				if _, ok := scopeSet[required]; !ok {
					http.Error(w, `{"error":"insufficient scopes"}`, http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractKey extracts the API key from Authorization header or X-API-Key header.
func extractKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return r.Header.Get("X-API-Key")
}
