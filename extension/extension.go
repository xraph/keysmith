// Package extension adapts Keysmith as a Forge extension.
package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/xraph/forge"
	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/pgdriver"
	"github.com/xraph/vessel"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/api"
	"github.com/xraph/keysmith/plugin"
	"github.com/xraph/keysmith/store"
	mongostore "github.com/xraph/keysmith/store/mongo"
	pgstore "github.com/xraph/keysmith/store/postgres"
	sqlitestore "github.com/xraph/keysmith/store/sqlite"
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
	*forge.BaseExtension

	config       Config
	eng          *keysmith.Engine
	apiHandler   *api.API
	logger       *slog.Logger
	keysmithOpts []keysmith.Option
	exts         []plugin.Plugin
	useGrove     bool
}

// New creates a Keysmith Forge extension with the given options.
func New(opts ...ExtOption) *Extension {
	e := &Extension{
		BaseExtension: forge.NewBaseExtension(ExtensionName, ExtensionVersion, ExtensionDescription),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Engine returns the underlying keysmith engine (nil until Register is called).
func (e *Extension) Engine() *keysmith.Engine { return e.eng }

// API returns the API handler.
func (e *Extension) API() *api.API { return e.apiHandler }

// Register implements [forge.Extension].
func (e *Extension) Register(fapp forge.App) error {
	if err := e.BaseExtension.Register(fapp); err != nil {
		return err
	}

	if err := e.loadConfiguration(); err != nil {
		return err
	}

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

	// Resolve store from grove DI if configured.
	if e.useGrove {
		groveDB, err := e.resolveGroveDB(fapp)
		if err != nil {
			return fmt.Errorf("keysmith: %w", err)
		}
		s, err := e.buildStoreFromGroveDB(groveDB)
		if err != nil {
			return err
		}
		e.keysmithOpts = append(e.keysmithOpts, keysmith.WithStore(s))
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
	if err := e.eng.Start(ctx); err != nil {
		return err
	}
	e.MarkStarted()
	return nil
}

// Stop gracefully shuts down the keysmith engine.
func (e *Extension) Stop(ctx context.Context) error {
	if e.eng == nil {
		e.MarkStopped()
		return nil
	}
	err := e.eng.Stop(ctx)
	e.MarkStopped()
	return err
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

// --- Config Loading (mirrors grove extension pattern) ---

// loadConfiguration loads config from YAML files or programmatic sources.
func (e *Extension) loadConfiguration() error {
	programmaticConfig := e.config

	// Try loading from config file.
	fileConfig, configLoaded := e.tryLoadFromConfigFile()

	if !configLoaded {
		if programmaticConfig.RequireConfig {
			return errors.New("keysmith: configuration is required but not found in config files; " +
				"ensure 'extensions.keysmith' or 'keysmith' key exists in your config")
		}

		// Use programmatic config merged with defaults.
		e.config = e.mergeWithDefaults(programmaticConfig)
	} else {
		// Config loaded from YAML -- merge with programmatic options.
		e.config = e.mergeConfigurations(fileConfig, programmaticConfig)
	}

	// Enable grove resolution if YAML config specifies a grove database.
	if e.config.GroveDatabase != "" {
		e.useGrove = true
	}

	e.Logger().Debug("keysmith: configuration loaded",
		forge.F("disable_routes", e.config.DisableRoutes),
		forge.F("disable_migrate", e.config.DisableMigrate),
		forge.F("base_path", e.config.BasePath),
		forge.F("grove_database", e.config.GroveDatabase),
	)

	return nil
}

// tryLoadFromConfigFile attempts to load config from YAML files.
func (e *Extension) tryLoadFromConfigFile() (Config, bool) {
	cm := e.App().Config()
	var cfg Config

	// Try "extensions.keysmith" first (namespaced pattern).
	if cm.IsSet("extensions.keysmith") {
		if err := cm.Bind("extensions.keysmith", &cfg); err == nil {
			e.Logger().Debug("keysmith: loaded config from file",
				forge.F("key", "extensions.keysmith"),
			)
			return cfg, true
		}
		e.Logger().Warn("keysmith: failed to bind extensions.keysmith config",
			forge.F("error", "bind failed"),
		)
	}

	// Try legacy "keysmith" key.
	if cm.IsSet("keysmith") {
		if err := cm.Bind("keysmith", &cfg); err == nil {
			e.Logger().Debug("keysmith: loaded config from file",
				forge.F("key", "keysmith"),
			)
			return cfg, true
		}
		e.Logger().Warn("keysmith: failed to bind keysmith config",
			forge.F("error", "bind failed"),
		)
	}

	return Config{}, false
}

// mergeWithDefaults fills zero-valued fields with defaults.
func (e *Extension) mergeWithDefaults(cfg Config) Config {
	// Currently no duration/int defaults to fill; return as-is.
	return cfg
}

// mergeConfigurations merges YAML config with programmatic options.
// YAML config takes precedence for most fields; programmatic bool flags fill gaps.
func (e *Extension) mergeConfigurations(yamlConfig, programmaticConfig Config) Config {
	// Programmatic bool flags override when true.
	if programmaticConfig.DisableRoutes {
		yamlConfig.DisableRoutes = true
	}
	if programmaticConfig.DisableMigrate {
		yamlConfig.DisableMigrate = true
	}

	// String fields: YAML takes precedence.
	if yamlConfig.BasePath == "" && programmaticConfig.BasePath != "" {
		yamlConfig.BasePath = programmaticConfig.BasePath
	}
	if yamlConfig.GroveDatabase == "" && programmaticConfig.GroveDatabase != "" {
		yamlConfig.GroveDatabase = programmaticConfig.GroveDatabase
	}

	// Fill remaining zeros with defaults.
	return e.mergeWithDefaults(yamlConfig)
}

// resolveGroveDB resolves a *grove.DB from the DI container.
// If GroveDatabase is set, it looks up the named DB; otherwise it uses the default.
func (e *Extension) resolveGroveDB(fapp forge.App) (*grove.DB, error) {
	if e.config.GroveDatabase != "" {
		db, err := vessel.InjectNamed[*grove.DB](fapp.Container(), e.config.GroveDatabase)
		if err != nil {
			return nil, fmt.Errorf("grove database %q not found in container: %w", e.config.GroveDatabase, err)
		}
		return db, nil
	}
	db, err := vessel.Inject[*grove.DB](fapp.Container())
	if err != nil {
		return nil, fmt.Errorf("default grove database not found in container: %w", err)
	}
	return db, nil
}

// buildStoreFromGroveDB constructs the appropriate store backend
// based on the grove driver type (pg, sqlite, mongo).
func (e *Extension) buildStoreFromGroveDB(db *grove.DB) (store.Store, error) {
	driverName := db.Driver().Name()
	switch driverName {
	case "pg":
		return pgstore.New(pgdriver.Unwrap(db)), nil
	case "sqlite":
		return sqlitestore.New(db), nil
	case "mongo":
		return mongostore.New(db), nil
	default:
		return nil, fmt.Errorf("keysmith: unsupported grove driver %q", driverName)
	}
}
