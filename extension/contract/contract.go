// Package contract wires keysmith into the Forge dashboard's contract path.
// It registers the `keysmith` contributor with the dashboard's contract
// registry and answers the queries and commands the React plugin sends.
package contract

import (
	"github.com/xraph/forge"

	"github.com/xraph/keysmith"
)

// ContributorName is the name keysmith registers under with the dashboard's
// contract registry. It is also the namespace of every intent in the manifest.
const ContributorName = "keysmith"

// Deps is everything the contract handlers need from the running extension.
type Deps struct {
	// Engine is the keysmith engine every handler reads and writes through.
	Engine *keysmith.Engine

	// DefaultTenantID is the tenant a request belongs to when the principal
	// carries no tenant_id claim, which is every request today. Set it for a
	// single-tenant deployment. Leaving it empty makes every read refuse,
	// which is correct for a multi-tenant deployment with no tenant claim:
	// the empty string would match every tenant's rows.
	DefaultTenantID string

	// DefaultAppID labels new rows when the principal carries no app_id
	// claim. It may be empty. Keysmith never filters by app, so a wrong or
	// missing app only mislabels a new row and never widens a read.
	DefaultAppID string

	// Plugins names the keysmith plugins the host registered, for the
	// overview's plugin listing.
	Plugins []string

	// Logger receives the server-side detail of internal errors. It may be
	// nil, in which case nothing is logged.
	Logger forge.Logger
}
