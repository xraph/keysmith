package keysmith

import (
	"time"

	log "github.com/xraph/go-utils/log"

	"github.com/xraph/keysmith/plugin"
	"github.com/xraph/keysmith/store"
)

// Option is a functional option for Engine.
type Option func(*Engine)

// WithStore sets the composite store.
func WithStore(s store.Store) Option { return func(e *Engine) { e.store = s } }

// WithHasher sets the key hasher.
func WithHasher(h Hasher) Option { return func(e *Engine) { e.hasher = h } }

// WithKeyGenerator sets the key generator.
func WithKeyGenerator(g KeyGenerator) Option { return func(e *Engine) { e.generator = g } }

// WithRateLimiter sets the rate limiter.
func WithRateLimiter(r RateLimiter) Option { return func(e *Engine) { e.ratelimiter = r } }

// WithExtension registers a lifecycle plugin with the engine.
func WithExtension(x plugin.Plugin) Option { return func(e *Engine) { e.hooks.Register(x) } }

// WithLogger sets the logger.
func WithLogger(l log.Logger) Option { return func(e *Engine) { e.logger = l } }

type rotateConfig struct {
	grace     *time.Duration
	rotatedBy string
}

// RotateOption customises one call to RotateKey.
type RotateOption func(*rotateConfig)

// WithGrace sets how long the previous key keeps validating. Zero means it
// stops the moment the rotation is recorded, which is what a compromise
// rotation wants. A negative value is treated as zero.
func WithGrace(d time.Duration) RotateOption {
	return func(c *rotateConfig) {
		if d < 0 {
			d = 0
		}
		c.grace = &d
	}
}

// WithRotatedBy records who rotated the key.
func WithRotatedBy(subject string) RotateOption {
	return func(c *rotateConfig) { c.rotatedBy = subject }
}
