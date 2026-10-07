package contract

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

func stGet(deps Deps) (settingsResponse, error) {
	return settingsHandler(deps)(context.Background(), settingsRequest{}, principal())
}

// stPingStore answers Ping with err, counts the calls, and keeps the
// deadline of the last context it was handed.
type stPingStore struct {
	store.Store
	err         error
	pings       int
	deadline    time.Time
	hasDeadline bool
	ctxErr      error
}

func (s *stPingStore) Ping(ctx context.Context) error {
	s.pings++
	s.deadline, s.hasDeadline = ctx.Deadline()
	s.ctxErr = ctx.Err()
	if s.err != nil {
		return s.err
	}
	return s.Store.Ping(ctx)
}

func TestSettingsReportsAHealthyStore(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, _ := setup(t, s)
		out, err := stGet(deps)
		require.NoError(t, err)
		assert.True(t, out.StoreHealthy)
		assert.Equal(t, "The store answered.", out.StoreMessage)
	})
}

// A store that is down: settings says so without echoing the driver's
// error, which goes to the server log with the intent that hit it.
func TestSettingsSaysTheStoreDidNotAnswerWithoutEchoingTheError(t *testing.T) {
	down := &stPingStore{Store: memory.New(), err: errors.New("dial tcp 10.0.0.1:5432: secret-connection-detail")}
	_, eng := setup(t, down)
	var lines []rotateLogLine
	deps := Deps{
		Engine: eng, DefaultTenantID: "t1",
		Logger: rotateCapturingLogger{Logger: forge.NewNoopLogger(), lines: &lines},
	}

	out, err := stGet(deps)
	require.NoError(t, err, "a store that is down is something settings reports, not a failed request")
	assert.False(t, out.StoreHealthy)
	assert.Equal(t, "The store did not answer. The error is in the server log.", out.StoreMessage)

	b, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "secret-connection-detail")
	assert.NotContains(t, string(b), "10.0.0.1")

	require.Len(t, lines, 1)
	assert.Equal(t, "settings", lines[0].fields["intent"])
	assert.Contains(t, lines[0].fields["error"], "secret-connection-detail")

	// Everything else still answers.
	assert.Equal(t, "t1", out.Tenant)
	assert.Len(t, out.Enforcement, policyFieldCount)

	// With no logger the answer is the same and nothing panics.
	deps.Logger = nil
	out, err = stGet(deps)
	require.NoError(t, err)
	assert.False(t, out.StoreHealthy)
}

// The health check gets at most two seconds, so a store that hangs cannot
// hang the page.
func TestSettingsGivesTheHealthCheckTwoSeconds(t *testing.T) {
	spy := &stPingStore{Store: memory.New()}
	deps, _ := setup(t, spy)
	start := time.Now()
	_, err := stGet(deps)
	end := time.Now()
	require.NoError(t, err)
	require.Equal(t, 1, spy.pings)
	require.True(t, spy.hasDeadline, "the health check runs under a deadline")
	// The deadline was set between start and end, two seconds ahead.
	assert.False(t, spy.deadline.Before(start.Add(2*time.Second)), "deadline %v", spy.deadline.Sub(start))
	assert.False(t, spy.deadline.After(end.Add(2*time.Second)), "deadline %v", spy.deadline.Sub(end))
	assert.NoError(t, spy.ctxErr)

	// The two seconds come off the request's own context, so a request that
	// is already gone, or has less time left, is never given more.
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = settingsHandler(deps)(gone, settingsRequest{}, principal())
	require.NoError(t, err)
	require.Equal(t, 2, spy.pings)
	assert.ErrorIs(t, spy.ctxErr, context.Canceled, "the ping runs under the request's context")

	short, cancelShort := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelShort()
	want, _ := short.Deadline()
	_, err = settingsHandler(deps)(short, settingsRequest{}, principal())
	require.NoError(t, err)
	require.Equal(t, 3, spy.pings)
	assert.Equal(t, want, spy.deadline, "the request's earlier deadline wins")
}

func TestSettingsListsPluginsSorted(t *testing.T) {
	deps, _ := setup(t, memory.New())

	out, err := stGet(deps)
	require.NoError(t, err)
	assert.NotNil(t, out.Plugins)
	assert.Empty(t, out.Plugins)
	b, err := json.Marshal(out)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"plugins":[]`)

	given := []string{"warden", "audit", "metrics"}
	deps.Plugins = given
	out, err = stGet(deps)
	require.NoError(t, err)
	assert.Equal(t, []string{"audit", "metrics", "warden"}, out.Plugins)
	assert.Equal(t, []string{"warden", "audit", "metrics"}, given, "Deps.Plugins is shared; it is never sorted in place")
}

func TestSettingsSaysWhetherARateLimiterIsConfigured(t *testing.T) {
	s := memory.New()
	deps, _ := setup(t, s)
	out, err := stGet(deps)
	require.NoError(t, err)
	assert.False(t, out.RateLimiterConfigured)
	assert.Equal(t, enforcementTable(false), out.Enforcement)
	assert.Equal(t, enforcedPolicyFieldCount(false), out.EnforcedFields)
	assert.Equal(t, 3, out.EnforcedFields)

	limited, err := keysmith.NewEngine(keysmith.WithStore(s), keysmith.WithRateLimiter(allowAll{}))
	require.NoError(t, err)
	deps.Engine = limited
	out, err = stGet(deps)
	require.NoError(t, err)
	assert.True(t, out.RateLimiterConfigured)
	assert.Equal(t, enforcementTable(true), out.Enforcement)
	assert.Equal(t, enforcedPolicyFieldCount(true), out.EnforcedFields)
	assert.Equal(t, 5, out.EnforcedFields)
}

func TestSettingsSaysWhereTheTenantCameFrom(t *testing.T) {
	deps, _ := setup(t, memory.New())

	out, err := stGet(deps)
	require.NoError(t, err)
	assert.Equal(t, "config", out.TenantSource)
	assert.Equal(t, "t1", out.Tenant)

	out, err = settingsHandler(deps)(context.Background(), settingsRequest{}, user(map[string]any{tenantClaim: "t9"}))
	require.NoError(t, err)
	assert.Equal(t, "claim", out.TenantSource)
	assert.Equal(t, "t9", out.Tenant)

	// The claim wins even with no configured default.
	deps.DefaultTenantID = ""
	out, err = settingsHandler(deps)(context.Background(), settingsRequest{}, user(map[string]any{tenantClaim: "t9"}))
	require.NoError(t, err)
	assert.Equal(t, "claim", out.TenantSource)
	assert.Equal(t, "t9", out.Tenant)

	// Other claims say nothing about the tenant.
	deps.DefaultTenantID = "t1"
	out, err = settingsHandler(deps)(context.Background(), settingsRequest{}, user(map[string]any{appClaim: "a1"}))
	require.NoError(t, err)
	assert.Equal(t, "config", out.TenantSource)
	assert.Equal(t, "t1", out.Tenant)

	// The session's org beats the configured tenant.
	out, err = settingsHandler(deps)(orgCtx("org_1"), settingsRequest{}, principal())
	require.NoError(t, err)
	assert.Equal(t, "scope", out.TenantSource)
	assert.Equal(t, "org_1", out.Tenant)

	// An app-only session has no org to follow.
	out, err = settingsHandler(deps)(appOnlyCtx(), settingsRequest{}, principal())
	require.NoError(t, err)
	assert.Equal(t, "config", out.TenantSource)
	assert.Equal(t, "t1", out.Tenant)
}

func TestTenantSourceOfFollowsTenantFrom(t *testing.T) {
	deps := Deps{DefaultTenantID: "t1"}
	bare := context.Background()
	assert.Equal(t, "config", tenantSourceOf(bare, user(nil), deps))
	assert.Equal(t, "claim", tenantSourceOf(bare, user(map[string]any{tenantClaim: "t2"}), deps))
	assert.Equal(t, "config", tenantSourceOf(bare, user(map[string]any{appClaim: "a"}), deps))
	assert.Equal(t, "scope", tenantSourceOf(orgCtx("org_1"), user(nil), deps))
	assert.Equal(t, "claim", tenantSourceOf(orgCtx("org_1"), user(map[string]any{tenantClaim: "t2"}), deps))
	assert.Equal(t, "config", tenantSourceOf(appOnlyCtx(), user(nil), deps))
}

// defaultGraceSeconds is the engine's own fallback, pinned against a real
// rotation of a key with no policy and no explicit grace.
func TestSettingsDefaultGraceIsTheEnginesFallback(t *testing.T) {
	deps, eng := setup(t, memory.New())
	out, err := stGet(deps)
	require.NoError(t, err)
	assert.EqualValues(t, 86400, out.DefaultGraceSeconds)

	k := create(t, eng, "t1", nil)
	_, err = eng.RotateKey(tctx("t1"), k.Key.ID, rotation.ReasonManual)
	require.NoError(t, err)
	recs, err := eng.ListRotations(tctx("t1"), &rotation.ListFilter{TenantID: "t1"})
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, out.DefaultGraceSeconds, int64(recs[0].GraceTTL/time.Second))
}

func TestSettingsWireShape(t *testing.T) {
	deps, _ := setup(t, memory.New())
	out, err := stGet(deps)
	require.NoError(t, err)
	b, err := json.Marshal(out)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	assert.ElementsMatch(t, []string{
		"plugins", "storeHealthy", "storeMessage", "rateLimiterConfigured",
		"tenantSource", "tenant", "enforcement", "enforcedFields", "defaultGraceSeconds",
	}, keys)
	body := string(b)
	assert.Contains(t, body, `"defaultGraceSeconds":86400`)
	assert.Contains(t, body, `"tenantSource":"config"`)
	assert.True(t, strings.Contains(body, `"enforcement":[{"field":"maxKeyLifetimeSeconds"`), body)
}

// Refusals come first: the store is never asked anything for a request that
// is refused.
func TestSettingsRefusesWithoutAUserOrTenant(t *testing.T) {
	spy := &stPingStore{Store: memory.New()}
	deps, _ := setup(t, spy)

	_, err := settingsHandler(deps)(context.Background(), settingsRequest{}, dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))

	_, err = settingsHandler(deps)(context.Background(), settingsRequest{}, user(map[string]any{tenantClaim: ""}))
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))

	deps.DefaultTenantID = ""
	_, err = stGet(deps)
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))

	assert.Zero(t, spy.pings)
}

func TestSettingsIsDispatchedAsAReadQuery(t *testing.T) {
	deps, _ := setup(t, memory.New())
	deps.Plugins = []string{"warden"}
	d := createTestDispatcher(t, deps)
	data, _, err := d.Dispatch(context.Background(), dashcontract.Request{
		Envelope: "v1", Kind: dashcontract.KindQuery, Contributor: ContributorName,
		Intent: "settings", IntentVersion: 1, Params: map[string]any{},
	}, principal())
	require.NoError(t, err)
	var out settingsResponse
	require.NoError(t, json.Unmarshal(data, &out))
	assert.True(t, out.StoreHealthy)
	assert.Equal(t, []string{"warden"}, out.Plugins)
	assert.Equal(t, enforcementTable(false), out.Enforcement)
}
