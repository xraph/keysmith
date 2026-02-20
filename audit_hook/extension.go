// Package audithook bridges Keysmith lifecycle events to an audit trail backend.
// It defines a local Recorder interface so the package does not import Chronicle directly.
package audithook

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/plugin"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
)

// Compile-time interface checks.
var (
	_ plugin.Plugin              = (*Extension)(nil)
	_ plugin.KeyCreated          = (*Extension)(nil)
	_ plugin.KeyCreateFailed     = (*Extension)(nil)
	_ plugin.KeyValidated        = (*Extension)(nil)
	_ plugin.KeyValidationFailed = (*Extension)(nil)
	_ plugin.KeyRotated          = (*Extension)(nil)
	_ plugin.KeyRevoked          = (*Extension)(nil)
	_ plugin.KeySuspended        = (*Extension)(nil)
	_ plugin.KeyReactivated      = (*Extension)(nil)
	_ plugin.KeyExpired          = (*Extension)(nil)
	_ plugin.KeyRateLimited      = (*Extension)(nil)
	_ plugin.PolicyCreated       = (*Extension)(nil)
	_ plugin.PolicyUpdated       = (*Extension)(nil)
	_ plugin.PolicyDeleted       = (*Extension)(nil)
)

// Recorder is the interface that audit backends must implement.
type Recorder interface {
	Record(ctx context.Context, event *AuditEvent) error
}

// AuditEvent is a local representation of an audit event.
type AuditEvent struct {
	Action     string         `json:"action"`
	Resource   string         `json:"resource"`
	Category   string         `json:"category"`
	ResourceID string         `json:"resource_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Outcome    string         `json:"outcome"`
	Severity   string         `json:"severity"`
	Reason     string         `json:"reason,omitempty"`
}

// RecorderFunc is an adapter to use a plain function as a Recorder.
type RecorderFunc func(ctx context.Context, event *AuditEvent) error

// Record implements Recorder.
func (f RecorderFunc) Record(ctx context.Context, event *AuditEvent) error {
	return f(ctx, event)
}

// Severity constants.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Outcome constants.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
)

// Action constants.
const (
	ActionKeyCreated          = "keysmith.key.created"
	ActionKeyCreateFailed     = "keysmith.key.create_failed"
	ActionKeyValidated        = "keysmith.key.validated"
	ActionKeyValidationFailed = "keysmith.key.validation_failed"
	ActionKeyRotated          = "keysmith.key.rotated"
	ActionKeyRevoked          = "keysmith.key.revoked"
	ActionKeySuspended        = "keysmith.key.suspended"
	ActionKeyReactivated      = "keysmith.key.reactivated"
	ActionKeyExpired          = "keysmith.key.expired"
	ActionKeyRateLimited      = "keysmith.key.rate_limited"
	ActionPolicyCreated       = "keysmith.policy.created"
	ActionPolicyUpdated       = "keysmith.policy.updated"
	ActionPolicyDeleted       = "keysmith.policy.deleted"
)

// Resource constants.
const (
	ResourceKey    = "key"
	ResourcePolicy = "policy"
)

// Category constants.
const (
	CategoryKeyLifecycle    = "key_lifecycle"
	CategoryKeyValidation   = "key_validation"
	CategoryKeySecurity     = "key_security"
	CategoryPolicyLifecycle = "policy_lifecycle"
)

// Extension bridges Keysmith lifecycle events to an audit trail backend.
type Extension struct {
	recorder Recorder
	enabled  map[string]bool
	logger   *slog.Logger
}

// New creates an Extension that emits audit events.
func New(r Recorder, opts ...Option) *Extension {
	e := &Extension{
		recorder: r,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Name implements plugin.Plugin.
func (e *Extension) Name() string { return "audit-hook" }

// OnKeyCreated implements plugin.KeyCreated.
func (e *Extension) OnKeyCreated(ctx context.Context, k *key.Key) error {
	return e.record(ctx, ActionKeyCreated, SeverityInfo, OutcomeSuccess,
		ResourceKey, k.ID.String(), CategoryKeyLifecycle, nil,
		"key_name", k.Name, "environment", string(k.Environment),
	)
}

// OnKeyCreateFailed implements plugin.KeyCreateFailed.
func (e *Extension) OnKeyCreateFailed(ctx context.Context, k *key.Key, createErr error) error {
	return e.record(ctx, ActionKeyCreateFailed, SeverityWarning, OutcomeFailure,
		ResourceKey, k.ID.String(), CategoryKeyLifecycle, createErr,
	)
}

// OnKeyValidated implements plugin.KeyValidated.
func (e *Extension) OnKeyValidated(ctx context.Context, k *key.Key) error {
	return e.record(ctx, ActionKeyValidated, SeverityInfo, OutcomeSuccess,
		ResourceKey, k.ID.String(), CategoryKeyValidation, nil,
	)
}

// OnKeyValidationFailed implements plugin.KeyValidationFailed.
func (e *Extension) OnKeyValidationFailed(ctx context.Context, _ string, validationErr error) error {
	return e.record(ctx, ActionKeyValidationFailed, SeverityWarning, OutcomeFailure,
		ResourceKey, "", CategoryKeyValidation, validationErr,
	)
}

// OnKeyRotated implements plugin.KeyRotated.
func (e *Extension) OnKeyRotated(ctx context.Context, k *key.Key, rec *rotation.Record) error {
	return e.record(ctx, ActionKeyRotated, SeverityCritical, OutcomeSuccess,
		ResourceKey, k.ID.String(), CategoryKeySecurity, nil,
		"reason", string(rec.Reason), "grace_ttl", rec.GraceTTL.String(),
	)
}

// OnKeyRevoked implements plugin.KeyRevoked.
func (e *Extension) OnKeyRevoked(ctx context.Context, k *key.Key, reason string) error {
	return e.record(ctx, ActionKeyRevoked, SeverityCritical, OutcomeSuccess,
		ResourceKey, k.ID.String(), CategoryKeySecurity, nil,
		"reason", reason,
	)
}

// OnKeySuspended implements plugin.KeySuspended.
func (e *Extension) OnKeySuspended(ctx context.Context, k *key.Key) error {
	return e.record(ctx, ActionKeySuspended, SeverityWarning, OutcomeSuccess,
		ResourceKey, k.ID.String(), CategoryKeySecurity, nil,
	)
}

// OnKeyReactivated implements plugin.KeyReactivated.
func (e *Extension) OnKeyReactivated(ctx context.Context, k *key.Key) error {
	return e.record(ctx, ActionKeyReactivated, SeverityInfo, OutcomeSuccess,
		ResourceKey, k.ID.String(), CategoryKeySecurity, nil,
	)
}

// OnKeyExpired implements plugin.KeyExpired.
func (e *Extension) OnKeyExpired(ctx context.Context, k *key.Key) error {
	return e.record(ctx, ActionKeyExpired, SeverityWarning, OutcomeSuccess,
		ResourceKey, k.ID.String(), CategoryKeyLifecycle, nil,
	)
}

// OnKeyRateLimited implements plugin.KeyRateLimited.
func (e *Extension) OnKeyRateLimited(ctx context.Context, k *key.Key) error {
	return e.record(ctx, ActionKeyRateLimited, SeverityWarning, OutcomeFailure,
		ResourceKey, k.ID.String(), CategoryKeySecurity, nil,
	)
}

// OnPolicyCreated implements plugin.PolicyCreated.
func (e *Extension) OnPolicyCreated(ctx context.Context, pol *policy.Policy) error {
	return e.record(ctx, ActionPolicyCreated, SeverityInfo, OutcomeSuccess,
		ResourcePolicy, pol.ID.String(), CategoryPolicyLifecycle, nil,
		"policy_name", pol.Name,
	)
}

// OnPolicyUpdated implements plugin.PolicyUpdated.
func (e *Extension) OnPolicyUpdated(ctx context.Context, pol *policy.Policy) error {
	return e.record(ctx, ActionPolicyUpdated, SeverityInfo, OutcomeSuccess,
		ResourcePolicy, pol.ID.String(), CategoryPolicyLifecycle, nil,
		"policy_name", pol.Name,
	)
}

// OnPolicyDeleted implements plugin.PolicyDeleted.
func (e *Extension) OnPolicyDeleted(ctx context.Context, polID id.PolicyID) error {
	return e.record(ctx, ActionPolicyDeleted, SeverityInfo, OutcomeSuccess,
		ResourcePolicy, polID.String(), CategoryPolicyLifecycle, nil,
	)
}

// record builds and sends an audit event if the action is enabled.
func (e *Extension) record(
	ctx context.Context,
	action, severity, outcome string,
	resource, resourceID, category string,
	err error,
	kvPairs ...any,
) error {
	if e.enabled != nil && !e.enabled[action] {
		return nil
	}

	meta := make(map[string]any, len(kvPairs)/2+1)
	for i := 0; i+1 < len(kvPairs); i += 2 {
		k, ok := kvPairs[i].(string)
		if !ok {
			k = fmt.Sprintf("%v", kvPairs[i])
		}
		meta[k] = kvPairs[i+1]
	}

	var reason string
	if err != nil {
		reason = err.Error()
		meta["error"] = err.Error()
	}

	evt := &AuditEvent{
		Action:     action,
		Resource:   resource,
		Category:   category,
		ResourceID: resourceID,
		Metadata:   meta,
		Outcome:    outcome,
		Severity:   severity,
		Reason:     reason,
	}

	if recErr := e.recorder.Record(ctx, evt); recErr != nil {
		e.logger.Warn("audit_hook: failed to record audit event",
			"action", action,
			"resource_id", resourceID,
			"error", recErr,
		)
	}
	return nil
}
