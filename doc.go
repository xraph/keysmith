// Package keysmith is a composable API key management engine for the Forge ecosystem.
//
// Keysmith handles the full lifecycle of API keys: generation, hashing, storage,
// validation, rotation, revocation, and usage analytics. Raw API keys are never
// persisted — only their SHA-256 hashes are stored, and the raw key is returned
// exactly once at creation time.
//
// # Quick Start
//
// Create an engine with an in-memory store (replace with postgres in production):
//
//	eng, err := keysmith.NewEngine(
//	    keysmith.WithStore(memory.New()),
//	)
//
// Set a tenant context and create a key:
//
//	ctx := keysmith.WithTenant(ctx, "my-app", "tenant-1")
//
//	result, err := eng.CreateKey(ctx, &keysmith.CreateKeyInput{
//	    Name:        "Production Key",
//	    Prefix:      "sk",
//	    Environment: key.EnvLive,
//	})
//	fmt.Println(result.RawKey) // shown once — save it
//
// Validate the key on incoming requests:
//
//	vr, err := eng.ValidateKey(ctx, rawKey)
//	fmt.Println(vr.Key.TenantID, vr.Scopes)
//
// # Architecture
//
// The engine coordinates five subsystems, each backed by a pluggable store:
//   - key — core key entity, lifecycle states (active/rotated/expired/revoked/suspended)
//   - policy — rate limits, allowed scopes/IPs/origins, key lifetime constraints
//   - scope — hierarchical permission scopes assignable to keys
//   - usage — per-request usage records and aggregated analytics
//   - rotation — rotation history with grace-period tracking
//
// # Plugins
//
// The [plugin] package defines opt-in lifecycle hook interfaces. A plugin implements
// [plugin.Plugin] (Name) and any combination of hooks like [plugin.KeyCreated] or
// [plugin.KeyRevoked]. Built-in plugins:
//   - audit_hook — emits structured audit events to a [audithook.Recorder] backend
//   - observability — increments go-utils metric counters for each lifecycle event
//   - warden_hook — syncs scopes as Warden permissions and assigns roles to API keys
//
// # Forge Integration
//
// The [extension] package adapts Keysmith as a [forge.Extension], registering the
// engine in the DI container, mounting REST API routes, and running migrations on start.
// The [middleware] package provides HTTP middleware for API key validation.
package keysmith
