package plugin

import (
	"context"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
)

// Manager holds registered plugins and dispatches lifecycle events.
type Manager struct {
	plugins []Plugin
}

// NewManager creates a new plugin manager.
func NewManager() *Manager {
	return &Manager{}
}

// Register adds a plugin.
func (m *Manager) Register(p Plugin) { m.plugins = append(m.plugins, p) }

// ── Key lifecycle dispatch ────────────────────────

// FireKeyCreated dispatches to all plugins that implement KeyCreated.
func (m *Manager) FireKeyCreated(ctx context.Context, k *key.Key) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeyCreated); ok {
			if err := h.OnKeyCreated(ctx, k); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireKeyCreateFailed dispatches to all plugins that implement KeyCreateFailed.
func (m *Manager) FireKeyCreateFailed(ctx context.Context, k *key.Key, createErr error) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeyCreateFailed); ok {
			if err := h.OnKeyCreateFailed(ctx, k, createErr); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireKeyValidated dispatches to all plugins that implement KeyValidated.
func (m *Manager) FireKeyValidated(ctx context.Context, k *key.Key) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeyValidated); ok {
			if err := h.OnKeyValidated(ctx, k); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireKeyValidationFailed dispatches to all plugins that implement KeyValidationFailed.
func (m *Manager) FireKeyValidationFailed(ctx context.Context, rawKey string, validationErr error) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeyValidationFailed); ok {
			if err := h.OnKeyValidationFailed(ctx, rawKey, validationErr); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireKeyRotated dispatches to all plugins that implement KeyRotated.
func (m *Manager) FireKeyRotated(ctx context.Context, k *key.Key, rec *rotation.Record) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeyRotated); ok {
			if err := h.OnKeyRotated(ctx, k, rec); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireKeyRevoked dispatches to all plugins that implement KeyRevoked.
func (m *Manager) FireKeyRevoked(ctx context.Context, k *key.Key, reason string) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeyRevoked); ok {
			if err := h.OnKeyRevoked(ctx, k, reason); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireKeySuspended dispatches to all plugins that implement KeySuspended.
func (m *Manager) FireKeySuspended(ctx context.Context, k *key.Key) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeySuspended); ok {
			if err := h.OnKeySuspended(ctx, k); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireKeyReactivated dispatches to all plugins that implement KeyReactivated.
func (m *Manager) FireKeyReactivated(ctx context.Context, k *key.Key) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeyReactivated); ok {
			if err := h.OnKeyReactivated(ctx, k); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireKeyExpired dispatches to all plugins that implement KeyExpired.
func (m *Manager) FireKeyExpired(ctx context.Context, k *key.Key) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeyExpired); ok {
			if err := h.OnKeyExpired(ctx, k); err != nil {
				return err
			}
		}
	}
	return nil
}

// FireKeyRateLimited dispatches to all plugins that implement KeyRateLimited.
func (m *Manager) FireKeyRateLimited(ctx context.Context, k *key.Key) error {
	for _, p := range m.plugins {
		if h, ok := p.(KeyRateLimited); ok {
			if err := h.OnKeyRateLimited(ctx, k); err != nil {
				return err
			}
		}
	}
	return nil
}

// ── Policy lifecycle dispatch ─────────────────────

// FirePolicyCreated dispatches to all plugins that implement PolicyCreated.
func (m *Manager) FirePolicyCreated(ctx context.Context, pol *policy.Policy) error {
	for _, p := range m.plugins {
		if h, ok := p.(PolicyCreated); ok {
			if err := h.OnPolicyCreated(ctx, pol); err != nil {
				return err
			}
		}
	}
	return nil
}

// FirePolicyUpdated dispatches to all plugins that implement PolicyUpdated.
func (m *Manager) FirePolicyUpdated(ctx context.Context, pol *policy.Policy) error {
	for _, p := range m.plugins {
		if h, ok := p.(PolicyUpdated); ok {
			if err := h.OnPolicyUpdated(ctx, pol); err != nil {
				return err
			}
		}
	}
	return nil
}

// FirePolicyDeleted dispatches to all plugins that implement PolicyDeleted.
func (m *Manager) FirePolicyDeleted(ctx context.Context, polID id.PolicyID) error {
	for _, p := range m.plugins {
		if h, ok := p.(PolicyDeleted); ok {
			if err := h.OnPolicyDeleted(ctx, polID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ── Shutdown dispatch ─────────────────────────────

// FireShutdown dispatches to all plugins that implement Shutdown.
func (m *Manager) FireShutdown(ctx context.Context) error {
	for _, p := range m.plugins {
		if h, ok := p.(Shutdown); ok {
			if err := h.OnShutdown(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
