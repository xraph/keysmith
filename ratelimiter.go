package keysmith

import (
	"context"
	"time"
)

// RateLimiter checks whether a request is allowed under rate limits.
type RateLimiter interface {
	// Allow returns true if the request is within rate limits.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)

	// Remaining returns the number of remaining requests in the current window.
	Remaining(ctx context.Context, key string, limit int, window time.Duration) (int, error)
}
