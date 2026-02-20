package audithook_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	audithook "github.com/xraph/keysmith/audit_hook"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
)

type mockRecorder struct {
	events []*audithook.AuditEvent
}

func (r *mockRecorder) Record(_ context.Context, event *audithook.AuditEvent) error {
	r.events = append(r.events, event)
	return nil
}

func TestExtension_Name(t *testing.T) {
	rec := &mockRecorder{}
	ext := audithook.New(rec)
	assert.Equal(t, "audit-hook", ext.Name())
}

func TestExtension_OnKeyCreated(t *testing.T) {
	rec := &mockRecorder{}
	ext := audithook.New(rec)

	k := &key.Key{
		ID:          id.NewKeyID(),
		Name:        "Test Key",
		Environment: key.EnvLive,
	}

	err := ext.OnKeyCreated(context.Background(), k)
	require.NoError(t, err)
	require.Len(t, rec.events, 1)

	evt := rec.events[0]
	assert.Equal(t, audithook.ActionKeyCreated, evt.Action)
	assert.Equal(t, audithook.SeverityInfo, evt.Severity)
	assert.Equal(t, audithook.OutcomeSuccess, evt.Outcome)
	assert.Equal(t, audithook.ResourceKey, evt.Resource)
	assert.Equal(t, audithook.CategoryKeyLifecycle, evt.Category)
	assert.Equal(t, k.ID.String(), evt.ResourceID)
	assert.Equal(t, "Test Key", evt.Metadata["key_name"])
}

func TestExtension_OnKeyCreateFailed(t *testing.T) {
	rec := &mockRecorder{}
	ext := audithook.New(rec)

	k := &key.Key{ID: id.NewKeyID()}
	createErr := errors.New("db connection failed")

	err := ext.OnKeyCreateFailed(context.Background(), k, createErr)
	require.NoError(t, err)
	require.Len(t, rec.events, 1)

	evt := rec.events[0]
	assert.Equal(t, audithook.ActionKeyCreateFailed, evt.Action)
	assert.Equal(t, audithook.SeverityWarning, evt.Severity)
	assert.Equal(t, audithook.OutcomeFailure, evt.Outcome)
	assert.Equal(t, "db connection failed", evt.Reason)
	assert.Equal(t, "db connection failed", evt.Metadata["error"])
}

func TestExtension_OnKeyRevoked(t *testing.T) {
	rec := &mockRecorder{}
	ext := audithook.New(rec)

	k := &key.Key{ID: id.NewKeyID()}

	err := ext.OnKeyRevoked(context.Background(), k, "compromised")
	require.NoError(t, err)
	require.Len(t, rec.events, 1)

	evt := rec.events[0]
	assert.Equal(t, audithook.ActionKeyRevoked, evt.Action)
	assert.Equal(t, audithook.SeverityCritical, evt.Severity)
	assert.Equal(t, audithook.CategoryKeySecurity, evt.Category)
	assert.Equal(t, "compromised", evt.Metadata["reason"])
}

func TestExtension_OnKeyRotated(t *testing.T) {
	rec := &mockRecorder{}
	ext := audithook.New(rec)

	k := &key.Key{ID: id.NewKeyID()}
	rec2 := &rotation.Record{
		Reason:   rotation.ReasonManual,
		GraceTTL: 24 * time.Hour,
	}

	err := ext.OnKeyRotated(context.Background(), k, rec2)
	require.NoError(t, err)
	require.Len(t, rec.events, 1)

	evt := rec.events[0]
	assert.Equal(t, audithook.ActionKeyRotated, evt.Action)
	assert.Equal(t, audithook.SeverityCritical, evt.Severity)
	assert.Equal(t, "manual", evt.Metadata["reason"])
}

func TestExtension_OnPolicyCreated(t *testing.T) {
	rec := &mockRecorder{}
	ext := audithook.New(rec)

	pol := &policy.Policy{ID: id.NewPolicyID(), Name: "Standard"}

	err := ext.OnPolicyCreated(context.Background(), pol)
	require.NoError(t, err)
	require.Len(t, rec.events, 1)

	evt := rec.events[0]
	assert.Equal(t, audithook.ActionPolicyCreated, evt.Action)
	assert.Equal(t, audithook.ResourcePolicy, evt.Resource)
	assert.Equal(t, audithook.CategoryPolicyLifecycle, evt.Category)
	assert.Equal(t, "Standard", evt.Metadata["policy_name"])
}

func TestExtension_OnPolicyDeleted(t *testing.T) {
	rec := &mockRecorder{}
	ext := audithook.New(rec)

	polID := id.NewPolicyID()

	err := ext.OnPolicyDeleted(context.Background(), polID)
	require.NoError(t, err)
	require.Len(t, rec.events, 1)

	evt := rec.events[0]
	assert.Equal(t, audithook.ActionPolicyDeleted, evt.Action)
	assert.Equal(t, polID.String(), evt.ResourceID)
}

func TestExtension_WithEnabled_FiltersActions(t *testing.T) {
	rec := &mockRecorder{}
	ext := audithook.New(rec, audithook.WithEnabled(audithook.ActionKeyCreated))

	k := &key.Key{ID: id.NewKeyID()}

	// Enabled action — should record.
	err := ext.OnKeyCreated(context.Background(), k)
	require.NoError(t, err)
	assert.Len(t, rec.events, 1)

	// Disabled action — should NOT record.
	err = ext.OnKeyRevoked(context.Background(), k, "test")
	require.NoError(t, err)
	assert.Len(t, rec.events, 1) // still 1
}

func TestExtension_RecorderFunc(t *testing.T) {
	var captured *audithook.AuditEvent
	fn := audithook.RecorderFunc(func(_ context.Context, event *audithook.AuditEvent) error {
		captured = event
		return nil
	})

	ext := audithook.New(fn)
	k := &key.Key{ID: id.NewKeyID(), Name: "FnKey"}

	err := ext.OnKeyCreated(context.Background(), k)
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, audithook.ActionKeyCreated, captured.Action)
}

func TestExtension_AllHooks(t *testing.T) {
	rec := &mockRecorder{}
	ext := audithook.New(rec)
	ctx := context.Background()
	k := &key.Key{ID: id.NewKeyID(), Name: "Test", Environment: key.EnvTest}
	pol := &policy.Policy{ID: id.NewPolicyID(), Name: "P"}
	rot := &rotation.Record{Reason: rotation.ReasonManual, GraceTTL: time.Hour}

	require.NoError(t, ext.OnKeyCreated(ctx, k))
	require.NoError(t, ext.OnKeyCreateFailed(ctx, k, errors.New("fail")))
	require.NoError(t, ext.OnKeyValidated(ctx, k))
	require.NoError(t, ext.OnKeyValidationFailed(ctx, "raw", errors.New("invalid")))
	require.NoError(t, ext.OnKeyRotated(ctx, k, rot))
	require.NoError(t, ext.OnKeyRevoked(ctx, k, "compromised"))
	require.NoError(t, ext.OnKeySuspended(ctx, k))
	require.NoError(t, ext.OnKeyReactivated(ctx, k))
	require.NoError(t, ext.OnKeyExpired(ctx, k))
	require.NoError(t, ext.OnKeyRateLimited(ctx, k))
	require.NoError(t, ext.OnPolicyCreated(ctx, pol))
	require.NoError(t, ext.OnPolicyUpdated(ctx, pol))
	require.NoError(t, ext.OnPolicyDeleted(ctx, pol.ID))

	assert.Len(t, rec.events, 13)
}
