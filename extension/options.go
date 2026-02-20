package extension

import (
	"log/slog"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/plugin"
)

// Config holds Forge extension configuration.
type Config struct {
	DisableRoutes  bool
	DisableMigrate bool
}

// ExtOption is a functional option for the Forge extension.
type ExtOption func(*Extension)

// WithConfig sets the extension config.
func WithConfig(c Config) ExtOption { return func(e *Extension) { e.config = c } }

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) ExtOption { return func(e *Extension) { e.logger = l } }

// WithEngineOptions passes options to the underlying keysmith engine.
func WithEngineOptions(opts ...keysmith.Option) ExtOption {
	return func(e *Extension) { e.keysmithOpts = append(e.keysmithOpts, opts...) }
}

// WithHookExtension registers a lifecycle plugin with the engine.
func WithHookExtension(x plugin.Plugin) ExtOption {
	return func(e *Extension) { e.exts = append(e.exts, x) }
}
