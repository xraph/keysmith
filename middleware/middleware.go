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
				writeValidationError(w, err)
				return
			}

			ctx := context.WithValue(r.Context(), contextKey{}, result)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeValidationError answers a failed ValidateKey. The caller's credentials
// are at fault only for the errors keysmith names: an unknown or inactive key
// is a 401, an expired, revoked or suspended one a 403, a rate limit a 429.
// Anything else (a key store that is down, a cancelled request) is not the
// caller's doing, so it is a 503 with a fixed body. The store's error text
// can carry hostnames and addresses, so it stays out of the response.
func writeValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, keysmith.ErrRateLimited):
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusTooManyRequests)
	case errors.Is(err, keysmith.ErrKeyExpired),
		errors.Is(err, keysmith.ErrKeyRevoked),
		errors.Is(err, keysmith.ErrKeySuspended):
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusForbidden)
	case errors.Is(err, keysmith.ErrInvalidKey),
		errors.Is(err, keysmith.ErrKeyInactive):
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusUnauthorized)
	default:
		http.Error(w, `{"error":"key validation unavailable"}`, http.StatusServiceUnavailable)
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
