// Package contract wires keysmith into the Forge dashboard's contract path.
// It registers the `keysmith` contributor with the dashboard's contract
// registry and answers the queries and commands the React plugin sends.
package contract

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"

	"github.com/xraph/keysmith"
)

//go:embed manifest.yaml
var manifestYAML []byte

// ContributorName is the name keysmith registers under with the dashboard's
// contract registry. It is also the namespace of every intent in the manifest.
const ContributorName = "keysmith"

// Deps is everything the contract handlers need from the running extension.
type Deps struct {
	// Engine is the keysmith engine every handler reads and writes through.
	Engine *keysmith.Engine

	// DefaultTenantID is the tenant a request belongs to when the principal
	// carries no tenant_id claim and the context carries no forge Scope with
	// an org. Under an auth extension that sets the session's org, it only
	// answers for sessions without one. Leaving it empty makes those requests
	// refuse, which is correct: the empty string would match every tenant's
	// rows.
	DefaultTenantID string

	// DefaultAppID labels new rows when the principal carries no app_id
	// claim. It may be empty. Keysmith never filters by app, so a wrong or
	// missing app only mislabels a new row and never widens a read.
	DefaultAppID string

	// Plugins names the hook plugins the host registered. The settings
	// intent lists them, and the key detail uses it to decide whether to
	// link to Warden.
	Plugins []string

	// Logger receives the server-side detail of internal errors. It may be
	// nil, in which case nothing is logged.
	Logger forge.Logger
}

// Register loads the embedded manifest, validates it, registers the keysmith
// contributor with reg, and binds the handlers against deps.
func Register(
	d *dispatcher.Dispatcher,
	reg dashcontract.Registry,
	wreg dashcontract.WardenRegistry,
	deps Deps,
) error {
	if deps.Engine == nil {
		return fmt.Errorf("keysmith/contract: Engine is required")
	}

	m, err := loader.Load(bytes.NewReader(manifestYAML), "keysmith/contract/manifest.yaml")
	if err != nil {
		return fmt.Errorf("keysmith/contract: load manifest: %w", err)
	}
	if err := loader.Validate(m, wreg); err != nil {
		return fmt.Errorf("keysmith/contract: validate manifest: %w", err)
	}
	if err := reg.Register(m); err != nil {
		return fmt.Errorf("keysmith/contract: register manifest: %w", err)
	}

	c := ContributorName
	for _, bind := range []struct {
		intent string
		fn     func() error
	}{
		{"keys.list", func() error {
			return dispatcher.RegisterQuery(d, c, "keys.list", 1, keysListHandler(deps))
		}},
		{"keys.detail", func() error {
			return dispatcher.RegisterQuery(d, c, "keys.detail", 1, keysDetailHandler(deps))
		}},
		{"policies.list", func() error {
			return dispatcher.RegisterQuery(d, c, "policies.list", 1, policiesListHandler(deps))
		}},
		{"policies.detail", func() error {
			return dispatcher.RegisterQuery(d, c, "policies.detail", 1, policiesDetailHandler(deps))
		}},
		{"scopes.list", func() error {
			return dispatcher.RegisterQuery(d, c, "scopes.list", 1, scopesListHandler(deps))
		}},
		{"rotations.list", func() error {
			return dispatcher.RegisterQuery(d, c, "rotations.list", 1, rotationsListHandler(deps))
		}},
		{"usage.series", func() error {
			return dispatcher.RegisterQuery(d, c, "usage.series", 1, usageSeriesHandler(deps))
		}},
		{"usage.records", func() error {
			return dispatcher.RegisterQuery(d, c, "usage.records", 1, usageRecordsHandler(deps))
		}},
		{"overview", func() error {
			return dispatcher.RegisterQuery(d, c, "overview", 1, overviewHandler(deps))
		}},
		{"settings", func() error {
			return dispatcher.RegisterQuery(d, c, "settings", 1, settingsHandler(deps))
		}},
		{"keys.create", func() error {
			return dispatcher.RegisterCommand(d, c, "keys.create", 1, keysCreateHandler(deps), dispatcher.SecretResponse())
		}},
		{"keys.rotate", func() error {
			return dispatcher.RegisterCommand(d, c, "keys.rotate", 1, keysRotateHandler(deps), dispatcher.SecretResponse())
		}},
		{"keys.endGrace", func() error {
			return dispatcher.RegisterCommand(d, c, "keys.endGrace", 1, keysEndGraceHandler(deps))
		}},
		{"keys.revoke", func() error {
			return dispatcher.RegisterCommand(d, c, "keys.revoke", 1, keysRevokeHandler(deps))
		}},
		{"keys.suspend", func() error {
			return dispatcher.RegisterCommand(d, c, "keys.suspend", 1, keysSuspendHandler(deps))
		}},
		{"keys.reactivate", func() error {
			return dispatcher.RegisterCommand(d, c, "keys.reactivate", 1, keysReactivateHandler(deps))
		}},
		{"keys.scopes.assign", func() error {
			return dispatcher.RegisterCommand(d, c, "keys.scopes.assign", 1, keysScopesAssignHandler(deps))
		}},
		{"keys.scopes.remove", func() error {
			return dispatcher.RegisterCommand(d, c, "keys.scopes.remove", 1, keysScopesRemoveHandler(deps))
		}},
		{"policies.create", func() error {
			return dispatcher.RegisterCommand(d, c, "policies.create", 1, policiesCreateHandler(deps))
		}},
		{"policies.update", func() error {
			return dispatcher.RegisterCommand(d, c, "policies.update", 1, policiesUpdateHandler(deps))
		}},
		{"policies.delete", func() error {
			return dispatcher.RegisterCommand(d, c, "policies.delete", 1, policiesDeleteHandler(deps))
		}},
		{"scopes.create", func() error {
			return dispatcher.RegisterCommand(d, c, "scopes.create", 1, scopesCreateHandler(deps))
		}},
		{"scopes.delete", func() error {
			return dispatcher.RegisterCommand(d, c, "scopes.delete", 1, scopesDeleteHandler(deps))
		}},
	} {
		if err := bind.fn(); err != nil {
			return fmt.Errorf("keysmith/contract: bind %s: %w", bind.intent, err)
		}
	}
	return nil
}
