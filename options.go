package keysmith

import (
	"log/slog"

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
func WithLogger(l *slog.Logger) Option { return func(e *Engine) { e.logger = l } }
