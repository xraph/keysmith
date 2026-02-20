package plugin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/plugin"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
)

// testPlugin implements all lifecycle hooks for testing.
type testPlugin struct {
	name   string
	called map[string]int
	err    error
}

func newTestPlugin(name string) *testPlugin {
	return &testPlugin{name: name, called: make(map[string]int)}
}

func (p *testPlugin) Name() string { return p.name }

func (p *testPlugin) OnKeyCreated(_ context.Context, _ *key.Key) error {
	p.called["KeyCreated"]++
	return p.err
}

func (p *testPlugin) OnKeyCreateFailed(_ context.Context, _ *key.Key, _ error) error {
	p.called["KeyCreateFailed"]++
	return p.err
}

func (p *testPlugin) OnKeyValidated(_ context.Context, _ *key.Key) error {
	p.called["KeyValidated"]++
	return p.err
}

func (p *testPlugin) OnKeyValidationFailed(_ context.Context, _ string, _ error) error {
	p.called["KeyValidationFailed"]++
	return p.err
}

func (p *testPlugin) OnKeyRotated(_ context.Context, _ *key.Key, _ *rotation.Record) error {
	p.called["KeyRotated"]++
	return p.err
}

func (p *testPlugin) OnKeyRevoked(_ context.Context, _ *key.Key, _ string) error {
	p.called["KeyRevoked"]++
	return p.err
}

func (p *testPlugin) OnKeySuspended(_ context.Context, _ *key.Key) error {
	p.called["KeySuspended"]++
	return p.err
}

func (p *testPlugin) OnKeyReactivated(_ context.Context, _ *key.Key) error {
	p.called["KeyReactivated"]++
	return p.err
}

func (p *testPlugin) OnKeyExpired(_ context.Context, _ *key.Key) error {
	p.called["KeyExpired"]++
	return p.err
}

func (p *testPlugin) OnKeyRateLimited(_ context.Context, _ *key.Key) error {
	p.called["KeyRateLimited"]++
	return p.err
}

func (p *testPlugin) OnPolicyCreated(_ context.Context, _ *policy.Policy) error {
	p.called["PolicyCreated"]++
	return p.err
}

func (p *testPlugin) OnPolicyUpdated(_ context.Context, _ *policy.Policy) error {
	p.called["PolicyUpdated"]++
	return p.err
}

func (p *testPlugin) OnPolicyDeleted(_ context.Context, _ id.PolicyID) error {
	p.called["PolicyDeleted"]++
	return p.err
}

func (p *testPlugin) OnShutdown(_ context.Context) error {
	p.called["Shutdown"]++
	return p.err
}

func TestManager_FireKeyCreated(t *testing.T) {
	m := plugin.NewManager()
	p := newTestPlugin("test")
	m.Register(p)

	err := m.FireKeyCreated(context.Background(), &key.Key{})
	require.NoError(t, err)
	assert.Equal(t, 1, p.called["KeyCreated"])
}

func TestManager_FireKeyCreated_Error(t *testing.T) {
	m := plugin.NewManager()
	p := newTestPlugin("test")
	p.err = errors.New("hook error")
	m.Register(p)

	err := m.FireKeyCreated(context.Background(), &key.Key{})
	assert.Error(t, err)
	assert.Equal(t, "hook error", err.Error())
}

func TestManager_MultiplePlugins(t *testing.T) {
	m := plugin.NewManager()
	p1 := newTestPlugin("p1")
	p2 := newTestPlugin("p2")
	m.Register(p1)
	m.Register(p2)

	err := m.FireKeyCreated(context.Background(), &key.Key{})
	require.NoError(t, err)
	assert.Equal(t, 1, p1.called["KeyCreated"])
	assert.Equal(t, 1, p2.called["KeyCreated"])
}

func TestManager_ErrorStopsDispatch(t *testing.T) {
	m := plugin.NewManager()
	p1 := newTestPlugin("p1")
	p1.err = errors.New("first fails")
	p2 := newTestPlugin("p2")
	m.Register(p1)
	m.Register(p2)

	err := m.FireKeyCreated(context.Background(), &key.Key{})
	assert.Error(t, err)
	assert.Equal(t, 1, p1.called["KeyCreated"])
	assert.Equal(t, 0, p2.called["KeyCreated"])
}

func TestManager_FireAllHooks(t *testing.T) {
	m := plugin.NewManager()
	p := newTestPlugin("test")
	m.Register(p)
	ctx := context.Background()
	k := &key.Key{}
	pol := &policy.Policy{}

	require.NoError(t, m.FireKeyCreated(ctx, k))
	require.NoError(t, m.FireKeyCreateFailed(ctx, k, errors.New("fail")))
	require.NoError(t, m.FireKeyValidated(ctx, k))
	require.NoError(t, m.FireKeyValidationFailed(ctx, "raw", errors.New("fail")))
	require.NoError(t, m.FireKeyRotated(ctx, k, &rotation.Record{}))
	require.NoError(t, m.FireKeyRevoked(ctx, k, "reason"))
	require.NoError(t, m.FireKeySuspended(ctx, k))
	require.NoError(t, m.FireKeyReactivated(ctx, k))
	require.NoError(t, m.FireKeyExpired(ctx, k))
	require.NoError(t, m.FireKeyRateLimited(ctx, k))
	require.NoError(t, m.FirePolicyCreated(ctx, pol))
	require.NoError(t, m.FirePolicyUpdated(ctx, pol))
	require.NoError(t, m.FirePolicyDeleted(ctx, id.NewPolicyID()))
	require.NoError(t, m.FireShutdown(ctx))

	assert.Equal(t, 1, p.called["KeyCreated"])
	assert.Equal(t, 1, p.called["KeyCreateFailed"])
	assert.Equal(t, 1, p.called["KeyValidated"])
	assert.Equal(t, 1, p.called["KeyValidationFailed"])
	assert.Equal(t, 1, p.called["KeyRotated"])
	assert.Equal(t, 1, p.called["KeyRevoked"])
	assert.Equal(t, 1, p.called["KeySuspended"])
	assert.Equal(t, 1, p.called["KeyReactivated"])
	assert.Equal(t, 1, p.called["KeyExpired"])
	assert.Equal(t, 1, p.called["KeyRateLimited"])
	assert.Equal(t, 1, p.called["PolicyCreated"])
	assert.Equal(t, 1, p.called["PolicyUpdated"])
	assert.Equal(t, 1, p.called["PolicyDeleted"])
	assert.Equal(t, 1, p.called["Shutdown"])
}

// partialPlugin implements only KeyCreated — other Fire* should skip it.
type partialPlugin struct {
	called int
}

func (p *partialPlugin) Name() string { return "partial" }
func (p *partialPlugin) OnKeyCreated(_ context.Context, _ *key.Key) error {
	p.called++
	return nil
}

func TestManager_SkipUnimplementedHooks(t *testing.T) {
	m := plugin.NewManager()
	pp := &partialPlugin{}
	m.Register(pp)
	ctx := context.Background()

	// This hook is implemented.
	require.NoError(t, m.FireKeyCreated(ctx, &key.Key{}))
	assert.Equal(t, 1, pp.called)

	// These hooks are not implemented — should not error.
	require.NoError(t, m.FireKeyRevoked(ctx, &key.Key{}, "reason"))
	require.NoError(t, m.FirePolicyCreated(ctx, &policy.Policy{}))
	require.NoError(t, m.FireShutdown(ctx))
}
