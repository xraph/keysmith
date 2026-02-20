// Package wardenhook bridges Keysmith key lifecycle events to Warden authorization.
// It defines a local WardenBridge interface so the package does not import Warden directly.
package wardenhook

import (
	"context"
	"log/slog"

	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/plugin"
)

// Compile-time interface checks.
var (
	_ plugin.Plugin     = (*Extension)(nil)
	_ plugin.KeyCreated = (*Extension)(nil)
	_ plugin.KeyRevoked = (*Extension)(nil)
)

// WardenBridge is the interface Warden must satisfy for Keysmith to sync
// scope-based role assignments. Defined locally to avoid importing Warden.
type WardenBridge interface {
	// AssignRoleToAPIKey assigns a Warden role to an API key subject.
	AssignRoleToAPIKey(ctx context.Context, tenantID, keyID, roleSlug string) error
	// UnassignRoleFromAPIKey removes a Warden role from an API key subject.
	UnassignRoleFromAPIKey(ctx context.Context, tenantID, keyID string) error
	// SyncScopesToPermissions ensures Warden permissions exist for the given scopes.
	SyncScopesToPermissions(ctx context.Context, tenantID string, scopes []string) error
}

// Extension bridges Keysmith key lifecycle events to Warden authorization.
// When a key is created with scopes, it syncs those scopes as Warden permissions
// and optionally assigns a Warden role to the API key subject.
type Extension struct {
	bridge      WardenBridge
	autoAssign  bool
	defaultRole string
	logger      *slog.Logger
}

// New creates a Warden bridge extension.
func New(bridge WardenBridge, opts ...Option) *Extension {
	e := &Extension{
		bridge:      bridge,
		autoAssign:  true,
		defaultRole: "api-key",
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Name implements plugin.Plugin.
func (e *Extension) Name() string { return "warden-hook" }

// OnKeyCreated implements plugin.KeyCreated.
// Syncs key scopes to Warden permissions and optionally assigns a role.
func (e *Extension) OnKeyCreated(ctx context.Context, k *key.Key) error {
	if len(k.Scopes) > 0 {
		if err := e.bridge.SyncScopesToPermissions(ctx, k.TenantID, k.Scopes); err != nil {
			e.logger.Warn("warden_hook: failed to sync scopes to permissions",
				"key_id", k.ID.String(),
				"error", err,
			)
		}
	}

	if e.autoAssign {
		if err := e.bridge.AssignRoleToAPIKey(ctx, k.TenantID, k.ID.String(), e.defaultRole); err != nil {
			e.logger.Warn("warden_hook: failed to assign role to API key",
				"key_id", k.ID.String(),
				"role", e.defaultRole,
				"error", err,
			)
		}
	}

	return nil
}

// OnKeyRevoked implements plugin.KeyRevoked.
// Removes the API key's Warden role assignment when the key is revoked.
func (e *Extension) OnKeyRevoked(ctx context.Context, k *key.Key, _ string) error {
	if err := e.bridge.UnassignRoleFromAPIKey(ctx, k.TenantID, k.ID.String()); err != nil {
		e.logger.Warn("warden_hook: failed to unassign role from revoked API key",
			"key_id", k.ID.String(),
			"error", err,
		)
	}
	return nil
}
