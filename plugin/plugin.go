// Package plugin defines lifecycle hook interfaces for Keysmith plugins.
//
// Plugins extend Keysmith by implementing opt-in lifecycle hook interfaces.
// A plugin must implement the base [Plugin] interface (which requires only a Name),
// and may additionally implement any combination of the hook interfaces below.
//
// Available key lifecycle hooks:
//   - [KeyCreated] — fired after a key is successfully created
//   - [KeyCreateFailed] — fired when key creation fails
//   - [KeyValidated] — fired after a key passes validation
//   - [KeyValidationFailed] — fired when key validation fails
//   - [KeyRotated] — fired after a key is rotated
//   - [KeyRevoked] — fired when a key is permanently revoked
//   - [KeySuspended] — fired when a key is temporarily suspended
//   - [KeyReactivated] — fired when a suspended key is reactivated
//   - [KeyExpired] — fired when a key is found expired during validation
//   - [KeyRateLimited] — fired when a key exceeds its rate limit
//
// Available policy lifecycle hooks:
//   - [PolicyCreated] — fired after a policy is created
//   - [PolicyUpdated] — fired after a policy is updated
//   - [PolicyDeleted] — fired after a policy is deleted
//
// Shutdown hook:
//   - [Shutdown] — fired during graceful engine shutdown
//
// Example plugin:
//
//	type myPlugin struct{}
//
//	func (p *myPlugin) Name() string { return "my-plugin" }
//
//	func (p *myPlugin) OnKeyCreated(ctx context.Context, k *key.Key) error {
//	    log.Println("key created:", k.ID)
//	    return nil
//	}
package plugin

import (
	"context"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
)

// ──────────────────────────────────────────────────
// Base plugin interface
// ──────────────────────────────────────────────────

// Plugin is the base interface all Keysmith plugins must implement.
type Plugin interface {
	Name() string
}

// ──────────────────────────────────────────────────
// Key lifecycle hooks
// ──────────────────────────────────────────────────

// KeyCreated is called when a key is created.
type KeyCreated interface {
	OnKeyCreated(ctx context.Context, k *key.Key) error
}

// KeyCreateFailed is called when key creation fails.
type KeyCreateFailed interface {
	OnKeyCreateFailed(ctx context.Context, k *key.Key, err error) error
}

// KeyValidated is called when a key passes validation.
type KeyValidated interface {
	OnKeyValidated(ctx context.Context, k *key.Key) error
}

// KeyValidationFailed is called when key validation fails.
type KeyValidationFailed interface {
	OnKeyValidationFailed(ctx context.Context, rawKey string, err error) error
}

// KeyRotated is called when a key is rotated.
type KeyRotated interface {
	OnKeyRotated(ctx context.Context, k *key.Key, rec *rotation.Record) error
}

// KeyRevoked is called when a key is revoked.
type KeyRevoked interface {
	OnKeyRevoked(ctx context.Context, k *key.Key, reason string) error
}

// KeySuspended is called when a key is suspended.
type KeySuspended interface {
	OnKeySuspended(ctx context.Context, k *key.Key) error
}

// KeyReactivated is called when a suspended key is reactivated.
type KeyReactivated interface {
	OnKeyReactivated(ctx context.Context, k *key.Key) error
}

// KeyExpired is called when a key is found to be expired during validation.
type KeyExpired interface {
	OnKeyExpired(ctx context.Context, k *key.Key) error
}

// KeyRateLimited is called when a key exceeds its rate limit.
type KeyRateLimited interface {
	OnKeyRateLimited(ctx context.Context, k *key.Key) error
}

// ──────────────────────────────────────────────────
// Policy lifecycle hooks
// ──────────────────────────────────────────────────

// PolicyCreated is called when a policy is created.
type PolicyCreated interface {
	OnPolicyCreated(ctx context.Context, pol *policy.Policy) error
}

// PolicyUpdated is called when a policy is updated.
type PolicyUpdated interface {
	OnPolicyUpdated(ctx context.Context, pol *policy.Policy) error
}

// PolicyDeleted is called when a policy is deleted.
type PolicyDeleted interface {
	OnPolicyDeleted(ctx context.Context, polID id.PolicyID) error
}

// ──────────────────────────────────────────────────
// Shutdown hook
// ──────────────────────────────────────────────────

// Shutdown is called during graceful shutdown.
type Shutdown interface {
	OnShutdown(ctx context.Context) error
}
