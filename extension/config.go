package extension

// Config holds the Keysmith extension configuration.
// Fields can be set programmatically via Option functions or loaded from
// YAML configuration files (under "extensions.keysmith" or "keysmith" keys).
type Config struct {
	// DisableRoutes prevents HTTP route registration.
	DisableRoutes bool `json:"disable_routes" mapstructure:"disable_routes" yaml:"disable_routes"`

	// DisableMigrate prevents auto-migration on start.
	DisableMigrate bool `json:"disable_migrate" mapstructure:"disable_migrate" yaml:"disable_migrate"`

	// BasePath is the URL prefix for keysmith routes (default: "/keysmith").
	BasePath string `json:"base_path" mapstructure:"base_path" yaml:"base_path"`

	// GroveDatabase is the name of a grove.DB registered in the DI container.
	// When set, the extension resolves this named database and auto-constructs
	// the appropriate store based on the driver type (pg/sqlite/mongo).
	// When empty and WithGroveDatabase was called, the default (unnamed) DB is used.
	GroveDatabase string `json:"grove_database" mapstructure:"grove_database" yaml:"grove_database"`

	// Dashboard scopes the React dashboard's contract handlers.
	Dashboard DashboardConfig `json:"dashboard" mapstructure:"dashboard" yaml:"dashboard"`

	// RequireConfig requires config to be present in YAML files.
	// If true and no config is found, Register returns an error.
	RequireConfig bool `json:"-" yaml:"-"`
}

// DashboardConfig scopes the dashboard contract handlers to a tenant.
type DashboardConfig struct {
	// TenantID is the tenant every dashboard request is scoped to when the
	// principal carries no tenant claim, which today is every request. Leave
	// it empty on a multi-tenant deployment and the dashboard refuses rather
	// than guessing.
	TenantID string `json:"tenant_id" mapstructure:"tenant_id" yaml:"tenant_id"`

	// AppID labels rows the dashboard creates when the principal carries no
	// app claim. It may be empty. Keysmith never filters reads by app.
	AppID string `json:"app_id" mapstructure:"app_id" yaml:"app_id"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{}
}
