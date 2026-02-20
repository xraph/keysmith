// Package observability provides a metrics extension for Keysmith lifecycle events.
package observability

import (
	"context"

	gu "github.com/xraph/go-utils/metrics"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/plugin"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
)

// Compile-time interface checks.
var (
	_ plugin.Plugin              = (*MetricsExtension)(nil)
	_ plugin.KeyCreated          = (*MetricsExtension)(nil)
	_ plugin.KeyCreateFailed     = (*MetricsExtension)(nil)
	_ plugin.KeyValidated        = (*MetricsExtension)(nil)
	_ plugin.KeyValidationFailed = (*MetricsExtension)(nil)
	_ plugin.KeyRotated          = (*MetricsExtension)(nil)
	_ plugin.KeyRevoked          = (*MetricsExtension)(nil)
	_ plugin.KeySuspended        = (*MetricsExtension)(nil)
	_ plugin.KeyReactivated      = (*MetricsExtension)(nil)
	_ plugin.KeyExpired          = (*MetricsExtension)(nil)
	_ plugin.KeyRateLimited      = (*MetricsExtension)(nil)
	_ plugin.PolicyCreated       = (*MetricsExtension)(nil)
	_ plugin.PolicyUpdated       = (*MetricsExtension)(nil)
	_ plugin.PolicyDeleted       = (*MetricsExtension)(nil)
)

// MetricsExtension records Keysmith lifecycle metrics via go-utils MetricFactory.
type MetricsExtension struct {
	keyCreated          gu.Counter
	keyCreateFailed     gu.Counter
	keyValidated        gu.Counter
	keyValidationFailed gu.Counter
	keyRotated          gu.Counter
	keyRevoked          gu.Counter
	keySuspended        gu.Counter
	keyReactivated      gu.Counter
	keyExpired          gu.Counter
	keyRateLimited      gu.Counter
	policyCreated       gu.Counter
	policyUpdated       gu.Counter
	policyDeleted       gu.Counter
}

// NewMetricsExtension creates a MetricsExtension using a default collector.
func NewMetricsExtension() *MetricsExtension {
	return NewMetricsExtensionWithFactory(gu.NewMetricsCollector("keysmith/observability"))
}

// NewMetricsExtensionWithFactory creates a MetricsExtension with the provided factory.
func NewMetricsExtensionWithFactory(factory gu.MetricFactory) *MetricsExtension {
	return &MetricsExtension{
		keyCreated:          factory.Counter("keysmith.key.created"),
		keyCreateFailed:     factory.Counter("keysmith.key.create_failed"),
		keyValidated:        factory.Counter("keysmith.key.validated"),
		keyValidationFailed: factory.Counter("keysmith.key.validation_failed"),
		keyRotated:          factory.Counter("keysmith.key.rotated"),
		keyRevoked:          factory.Counter("keysmith.key.revoked"),
		keySuspended:        factory.Counter("keysmith.key.suspended"),
		keyReactivated:      factory.Counter("keysmith.key.reactivated"),
		keyExpired:          factory.Counter("keysmith.key.expired"),
		keyRateLimited:      factory.Counter("keysmith.key.rate_limited"),
		policyCreated:       factory.Counter("keysmith.policy.created"),
		policyUpdated:       factory.Counter("keysmith.policy.updated"),
		policyDeleted:       factory.Counter("keysmith.policy.deleted"),
	}
}

// Name implements plugin.Plugin.
func (m *MetricsExtension) Name() string { return "observability-metrics" }

// OnKeyCreated implements plugin.KeyCreated.
func (m *MetricsExtension) OnKeyCreated(_ context.Context, _ *key.Key) error {
	m.keyCreated.Inc()
	return nil
}

// OnKeyCreateFailed implements plugin.KeyCreateFailed.
func (m *MetricsExtension) OnKeyCreateFailed(_ context.Context, _ *key.Key, _ error) error {
	m.keyCreateFailed.Inc()
	return nil
}

// OnKeyValidated implements plugin.KeyValidated.
func (m *MetricsExtension) OnKeyValidated(_ context.Context, _ *key.Key) error {
	m.keyValidated.Inc()
	return nil
}

// OnKeyValidationFailed implements plugin.KeyValidationFailed.
func (m *MetricsExtension) OnKeyValidationFailed(_ context.Context, _ string, _ error) error {
	m.keyValidationFailed.Inc()
	return nil
}

// OnKeyRotated implements plugin.KeyRotated.
func (m *MetricsExtension) OnKeyRotated(_ context.Context, _ *key.Key, _ *rotation.Record) error {
	m.keyRotated.Inc()
	return nil
}

// OnKeyRevoked implements plugin.KeyRevoked.
func (m *MetricsExtension) OnKeyRevoked(_ context.Context, _ *key.Key, _ string) error {
	m.keyRevoked.Inc()
	return nil
}

// OnKeySuspended implements plugin.KeySuspended.
func (m *MetricsExtension) OnKeySuspended(_ context.Context, _ *key.Key) error {
	m.keySuspended.Inc()
	return nil
}

// OnKeyReactivated implements plugin.KeyReactivated.
func (m *MetricsExtension) OnKeyReactivated(_ context.Context, _ *key.Key) error {
	m.keyReactivated.Inc()
	return nil
}

// OnKeyExpired implements plugin.KeyExpired.
func (m *MetricsExtension) OnKeyExpired(_ context.Context, _ *key.Key) error {
	m.keyExpired.Inc()
	return nil
}

// OnKeyRateLimited implements plugin.KeyRateLimited.
func (m *MetricsExtension) OnKeyRateLimited(_ context.Context, _ *key.Key) error {
	m.keyRateLimited.Inc()
	return nil
}

// OnPolicyCreated implements plugin.PolicyCreated.
func (m *MetricsExtension) OnPolicyCreated(_ context.Context, _ *policy.Policy) error {
	m.policyCreated.Inc()
	return nil
}

// OnPolicyUpdated implements plugin.PolicyUpdated.
func (m *MetricsExtension) OnPolicyUpdated(_ context.Context, _ *policy.Policy) error {
	m.policyUpdated.Inc()
	return nil
}

// OnPolicyDeleted implements plugin.PolicyDeleted.
func (m *MetricsExtension) OnPolicyDeleted(_ context.Context, _ id.PolicyID) error {
	m.policyDeleted.Inc()
	return nil
}
