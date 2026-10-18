package keysmith

import (
	"time"

	log "github.com/xraph/go-utils/log"
	gometrics "github.com/xraph/go-utils/metrics"

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

// WithMetrics records keysmith's metrics on m: validations and their
// duration, lifecycle operations, rate limiter checks, failed hooks and
// failed last-used writes. In a Forge app pass app.Metrics() and the series
// land on /_/metrics with forge's namespace in front
// (forge_keysmith_key_validations_total). Keysmith's Forge extension does
// this for you. A nil m, the default, records nothing.
//
// Spans need no option: the engine starts them on the global tracer
// provider, which forge.New installs.
func WithMetrics(m gometrics.Metrics) Option { return func(e *Engine) { e.metrics = m } }

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
