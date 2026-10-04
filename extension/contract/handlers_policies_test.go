package contract

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

func policyDetail(t *testing.T, deps Deps, polID id.PolicyID) policiesDetailResponse {
	t.Helper()
	out, err := policiesDetailHandler(deps)(context.Background(), policyIDRequest{ID: polID.String()}, principal())
	require.NoError(t, err)
	return out
}

func TestPoliciesDetailProjectsEveryField(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		full := mkPolicy(t, eng, "t1", &policy.Policy{
			Name:            "Standard",
			Description:     "the usual limits",
			MaxKeyLifetime:  90 * 24 * time.Hour,
			GracePeriod:     time.Hour,
			AllowedScopes:   []string{"write", "read", "write"},
			RateLimit:       100,
			RateLimitWindow: time.Minute,
			BurstLimit:      20,
			AllowedIPs:      []string{"192.168.1.1", "10.0.0.0/8", "192.168.1.1"},
			AllowedOrigins:  []string{"https://b.example", "https://a.example"},
			AllowedMethods:  []string{"POST", "GET", "POST"},
			AllowedPaths:    []string{"/v1/keys", "/v1/admin"},
			RotationPeriod:  30 * 24 * time.Hour,
			DailyQuota:      1000,
			MonthlyQuota:    25000,
		})

		got := policyDetail(t, deps, full.ID).Policy
		stored, err := s.Policies().Get(context.Background(), full.ID)
		require.NoError(t, err)

		assert.Equal(t, full.ID.String(), got.ID)
		assert.Equal(t, "Standard", got.Name)
		assert.Equal(t, "the usual limits", got.Description)
		for name, pair := range map[string]struct {
			got  *int64
			want int64
		}{
			"maxKeyLifetimeSeconds":  {got.MaxKeyLifetimeSeconds, 90 * 24 * 3600},
			"graceSeconds":           {got.GraceSeconds, 3600},
			"rateLimit":              {got.RateLimit, 100},
			"rateLimitWindowSeconds": {got.RateLimitWindowSeconds, 60},
			"burstLimit":             {got.BurstLimit, 20},
			"rotationPeriodSeconds":  {got.RotationPeriodSeconds, 30 * 24 * 3600},
			"dailyQuota":             {got.DailyQuota, 1000},
			"monthlyQuota":           {got.MonthlyQuota, 25000},
		} {
			if assert.NotNil(t, pair.got, name) {
				assert.Equal(t, pair.want, *pair.got, name)
			}
		}
		// Lists come back sorted and without duplicates.
		assert.Equal(t, []string{"read", "write"}, got.AllowedScopes)
		assert.Equal(t, []string{"10.0.0.0/8", "192.168.1.1"}, got.AllowedIPs)
		assert.Equal(t, []string{"https://a.example", "https://b.example"}, got.AllowedOrigins)
		assert.Equal(t, []string{"GET", "POST"}, got.AllowedMethods)
		assert.Equal(t, []string{"/v1/admin", "/v1/keys"}, got.AllowedPaths)
		assert.Equal(t, rfc3339(stored.CreatedAt), got.CreatedAt)
		assert.Equal(t, rfc3339(stored.UpdatedAt), got.UpdatedAt)

		bare := mkPolicy(t, eng, "t1", &policy.Policy{Name: "Bare"})
		b, err := json.Marshal(policyDetail(t, deps, bare.ID))
		require.NoError(t, err)
		body := string(b)
		for _, field := range []string{
			"maxKeyLifetimeSeconds", "graceSeconds", "rateLimit", "rateLimitWindowSeconds",
			"burstLimit", "rotationPeriodSeconds", "dailyQuota", "monthlyQuota",
		} {
			assert.Contains(t, body, `"`+field+`":null`)
		}
		for _, field := range []string{"allowedScopes", "allowedIps", "allowedOrigins", "allowedMethods", "allowedPaths"} {
			assert.Contains(t, body, `"`+field+`":[]`)
		}
		assert.NotContains(t, body, `"description"`, "an empty description is left out")
	})
}

func TestPoliciesDetailIsTenantScoped(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mine := mkPolicy(t, eng, "t1", &policy.Policy{Name: "mine"})
		theirs := mkPolicy(t, eng, "t2", &policy.Policy{Name: "theirs"})
		h := policiesDetailHandler(deps)

		out, err := h(context.Background(), policyIDRequest{ID: "  " + mine.ID.String() + " "}, principal())
		require.NoError(t, err)
		assert.Equal(t, mine.ID.String(), out.Policy.ID)

		notFound := &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "policy not found"}
		_, otherTenant := h(context.Background(), policyIDRequest{ID: theirs.ID.String()}, principal())
		_, missing := h(context.Background(), policyIDRequest{ID: id.NewPolicyID().String()}, principal())
		assert.Equal(t, notFound, otherTenant)
		assert.Equal(t, notFound, missing)

		_, err = h(context.Background(), policyIDRequest{ID: ""}, principal())
		assert.Equal(t, &dashcontract.Error{Code: dashcontract.CodeBadRequest, Message: "id is required"}, err)
		_, err = h(context.Background(), policyIDRequest{ID: id.NewKeyID().String()}, principal())
		assert.Equal(t, &dashcontract.Error{Code: dashcontract.CodeBadRequest, Message: "id is not a policy id"}, err)
	})
}

func TestPoliciesDetailRefusesWithoutAUserOrTenant(t *testing.T) {
	deps, eng := setup(t, memory.New())
	pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "p"})
	_, err := policiesDetailHandler(deps)(context.Background(), policyIDRequest{ID: pol.ID.String()}, dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))

	deps.DefaultTenantID = ""
	_, err = policiesDetailHandler(deps)(context.Background(), policyIDRequest{ID: pol.ID.String()}, principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
}

func TestPoliciesDetailCountsKeys(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "Standard"})
		other := mkPolicy(t, eng, "t1", &policy.Policy{Name: "Other"})
		withPolicy := func(pid id.PolicyID) *keysmith.CreateKeyInput {
			return &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pid}
		}

		create(t, eng, "t1", withPolicy(pol.ID))
		create(t, eng, "t1", withPolicy(pol.ID))
		suspended := create(t, eng, "t1", withPolicy(pol.ID))
		require.NoError(t, eng.SuspendKey(tctx("t1"), suspended.Key.ID))
		revoked := create(t, eng, "t1", withPolicy(pol.ID))
		require.NoError(t, eng.RevokeKey(tctx("t1"), revoked.Key.ID, "done"))
		// Keys under another policy and keys with none are not counted.
		create(t, eng, "t1", withPolicy(other.ID))
		create(t, eng, "t1", nil)

		// Another tenant's key naming this policy, written straight to the
		// store because the contract (keys.create) would not let it through.
		// The engine's CreateKey does not check the policy's tenant.
		now := time.Now()
		polID := pol.ID
		require.NoError(t, s.Keys().Create(context.Background(), &key.Key{
			ID: id.NewKeyID(), TenantID: "t2", AppID: "app", Name: "theirs",
			Prefix: "sk", Hint: "zzzz", KeyHash: "hash-" + id.NewKeyID().String(),
			Environment: key.EnvLive, State: key.StateActive, PolicyID: &polID,
			CreatedAt: now, UpdatedAt: now,
		}))

		got := policyDetail(t, deps, pol.ID)
		assert.Equal(t, 4, got.KeysUsing)
		assert.Equal(t, 3, got.KeysBlockingDelete)

		// A revoked key whose stored state was put back to active by a stale
		// write still counts as revoked, as it does in engine.DeletePolicy.
		reset := create(t, eng, "t1", withPolicy(pol.ID))
		require.NoError(t, eng.RevokeKey(tctx("t1"), reset.Key.ID, "leaked"))
		require.NoError(t, s.Keys().UpdateState(context.Background(), reset.Key.ID, key.StateActive))
		got = policyDetail(t, deps, pol.ID)
		assert.Equal(t, 5, got.KeysUsing)
		assert.Equal(t, 3, got.KeysBlockingDelete)

		// The count agrees with what the engine does on delete.
		empty := mkPolicy(t, eng, "t1", &policy.Policy{Name: "Empty"})
		got = policyDetail(t, deps, empty.ID)
		assert.Zero(t, got.KeysUsing)
		assert.Zero(t, got.KeysBlockingDelete)
	})
}

// allowAll is a RateLimiter that lets everything through. The tests only
// need the engine to have one.
type allowAll struct{}

func (allowAll) Allow(context.Context, string, int, time.Duration) (bool, error) { return true, nil }

func (allowAll) Remaining(context.Context, string, int, time.Duration) (int, error) {
	return 1, nil
}

func TestPoliciesListAndDetailReportTheRateLimiter(t *testing.T) {
	s := memory.New()
	deps, eng := setup(t, s)
	pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "p"})

	list, err := policiesListHandler(deps)(context.Background(), pickerRequest{}, principal())
	require.NoError(t, err)
	assert.False(t, list.RateLimiterConfigured)
	assert.False(t, policyDetail(t, deps, pol.ID).RateLimiterConfigured)

	limited, err := keysmith.NewEngine(keysmith.WithStore(s), keysmith.WithRateLimiter(allowAll{}))
	require.NoError(t, err)
	deps.Engine = limited
	list, err = policiesListHandler(deps)(context.Background(), pickerRequest{}, principal())
	require.NoError(t, err)
	assert.True(t, list.RateLimiterConfigured)
	assert.True(t, policyDetail(t, deps, pol.ID).RateLimiterConfigured)

	b, err := json.Marshal(list)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"rateLimiterConfigured":true`)
}
