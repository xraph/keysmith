// Package extension adapts Keysmith as a Forge extension.
package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/api"
	"github.com/xraph/keysmith/plugin"
)

// ExtensionName is the name registered with Forge.
const ExtensionName = "keysmith"

// ExtensionDescription is the human-readable description.
const ExtensionDescription = "Composable API key management engine for key lifecycle, validation, and usage analytics"

// ExtensionVersion is the semantic version.
const ExtensionVersion = "0.1.0"

// Ensure Extension implements forge.Extension at compile time.
var _ forge.Extension = (*Extension)(nil)

// Extension adapts Keysmith as a Forge extension.
type Extension struct {
	config       Config
	eng          *keysmith.Engine
	apiHandler   *api.API
	logger       *slog.Logger
	keysmithOpts []keysmith.Option
	exts         []plugin.Plugin
}

// New creates a Keysmith Forge extension with the given options.
func New(opts ...ExtOption) *Extension {
	e := &Extension{}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Name returns the extension name.
func (e *Extension) Name() string { return ExtensionName }

// Description returns the extension description.
func (e *Extension) Description() string { return ExtensionDescription }

// Version returns the extension version.
func (e *Extension) Version() string { return ExtensionVersion }

// Dependencies returns the list of extension names this extension depends on.
func (e *Extension) Dependencies() []string { return []string{} }

// Engine returns the underlying keysmith engine (nil until Register is called).
func (e *Extension) Engine() *keysmith.Engine { return e.eng }

// API returns the API handler.
func (e *Extension) API() *api.API { return e.apiHandler }

// Register implements [forge.Extension].
func (e *Extension) Register(fapp forge.App) error {
	if err := e.init(fapp); err != nil {
		return err
	}

	if err := vessel.Provide(fapp.Container(), func() (*keysmith.Engine, error) {
		return e.eng, nil
	}); err != nil {
		return fmt.Errorf("keysmith: register engine in container: %w", err)
	}

	return nil
}

// init builds the engine and API handler.
func (e *Extension) init(fapp forge.App) error {
	logger := e.logger
	if logger == nil {
		logger = slog.Default()
	}

	opts := make([]keysmith.Option, 0, len(e.keysmithOpts)+1)
	opts = append(opts, e.keysmithOpts...)
	opts = append(opts, keysmith.WithLogger(logger))

	for _, hookExt := range e.exts {
		opts = append(opts, keysmith.WithExtension(hookExt))
	}

	eng, err := keysmith.NewEngine(opts...)
	if err != nil {
		return fmt.Errorf("keysmith: create engine: %w", err)
	}
	e.eng = eng

	e.apiHandler = api.New(e.eng, fapp.Router())

	if !e.config.DisableRoutes {
		e.apiHandler.RegisterRoutes(fapp.Router())
	}

	return nil
}

// Start begins the keysmith engine and runs auto-migration if enabled.
func (e *Extension) Start(ctx context.Context) error {
	if e.eng == nil {
		return errors.New("keysmith: extension not initialized")
	}
	if !e.config.DisableMigrate {
		if err := e.eng.Store().Migrate(ctx); err != nil {
			return fmt.Errorf("keysmith: migration failed: %w", err)
		}
	}
	return e.eng.Start(ctx)
}

// Stop gracefully shuts down the keysmith engine.
func (e *Extension) Stop(ctx context.Context) error {
	if e.eng == nil {
		return nil
	}
	return e.eng.Stop(ctx)
}

// Health implements [forge.Extension].
func (e *Extension) Health(ctx context.Context) error {
	if e.eng == nil {
		return errors.New("keysmith: extension not initialized")
	}
	return e.eng.Store().Ping(ctx)
}

// Handler returns the HTTP handler for standalone use outside Forge.
func (e *Extension) Handler() http.Handler {
	if e.apiHandler == nil {
		return http.NotFoundHandler()
	}
	return e.apiHandler.Handler()
}
