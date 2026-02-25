package extension

import (
	"log/slog"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/plugin"
)

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

// WithDisableRoutes prevents HTTP route registration.
func WithDisableRoutes() ExtOption {
	return func(e *Extension) { e.config.DisableRoutes = true }
}

// WithDisableMigrate prevents auto-migration on start.
func WithDisableMigrate() ExtOption {
	return func(e *Extension) { e.config.DisableMigrate = true }
}

// WithBasePath sets the URL prefix for keysmith routes.
func WithBasePath(path string) ExtOption {
	return func(e *Extension) { e.config.BasePath = path }
}

// WithRequireConfig requires config to be present in YAML files.
// If true and no config is found, Register returns an error.
func WithRequireConfig(require bool) ExtOption {
	return func(e *Extension) { e.config.RequireConfig = require }
}

// WithGroveDatabase sets the name of the grove.DB to resolve from the DI container.
// The extension will auto-construct the appropriate store backend (postgres/sqlite/mongo)
// based on the grove driver type. Pass an empty string to use the default (unnamed) grove.DB.
func WithGroveDatabase(name string) ExtOption {
	return func(e *Extension) {
		e.config.GroveDatabase = name
		e.useGrove = true
	}
}
