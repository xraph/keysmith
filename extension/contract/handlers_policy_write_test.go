package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

func pwStr(s string) *string            { return &s }
func pwInt(n int64) *int64              { return &n }
func pwList(s ...string) *[]string      { l := append([]string{}, s...); return &l }
func pwFields(name string) policyFields { return policyFields{Name: pwStr(name)} }

func pwCreate(t *testing.T, deps Deps, in policyFields) PolicyDetail {
	t.Helper()
	out, err := policiesCreateHandler(deps)(context.Background(), in, principal())
	require.NoError(t, err)
	return out.Policy
}

func pwUpdate(deps Deps, polID string, in policyFields) (policyResponse, error) {
	return policiesUpdateHandler(deps)(context.Background(), policiesUpdateRequest{ID: polID, policyFields: in}, principal())
}

func pwPolicyID(t *testing.T, s string) id.PolicyID {
	t.Helper()
	pid, err := id.ParsePolicyID(s)
	require.NoError(t, err)
	return pid
}

func pwRepeat(prefix string, n int) *[]string {
	l := make([]string, 0, n)
	for i := range n {
		l = append(l, prefix+strconv.Itoa(i))
	}
	return &l
}

// Every rule answers BAD_REQUEST with a fixed message. The fixture copies
// these, so they are pinned word for word.
func TestPoliciesCreateValidationMessages(t *testing.T) {
	deps, eng := setup(t, memory.New())
	mkScope(t, eng, "t1", "read", "")
	mkScope(t, eng, "t2", "theirs", "")

	cases := []struct {
		name   string
		mutate func(*policyFields)
		want   string
	}{
		{"no name", func(f *policyFields) { f.Name = nil }, "name is required"},
		{"blank name", func(f *policyFields) { f.Name = pwStr("   ") }, "name is required"},
		{"name over 200 runes", func(f *policyFields) { f.Name = pwStr(strings.Repeat("é", 201)) }, "name is too long"},
		{"description over 1000 runes", func(f *policyFields) {
			f.Description = pwStr(strings.Repeat("é", 1001))
		}, "description is too long"},

		{"negative lifetime", func(f *policyFields) { f.MaxKeyLifetimeSeconds = pwInt(-1) }, "maxKeyLifetimeSeconds cannot be negative"},
		{"negative grace", func(f *policyFields) { f.GraceSeconds = pwInt(-1) }, "graceSeconds cannot be negative"},
		{"negative rate limit", func(f *policyFields) { f.RateLimit = pwInt(-1) }, "rateLimit cannot be negative"},
		{"negative window", func(f *policyFields) { f.RateLimitWindowSeconds = pwInt(-1) }, "rateLimitWindowSeconds cannot be negative"},
		{"negative burst", func(f *policyFields) { f.BurstLimit = pwInt(-1) }, "burstLimit cannot be negative"},
		{"negative rotation", func(f *policyFields) { f.RotationPeriodSeconds = pwInt(-1) }, "rotationPeriodSeconds cannot be negative"},
		{"negative daily quota", func(f *policyFields) { f.DailyQuota = pwInt(-1) }, "dailyQuota cannot be negative"},
		{"negative monthly quota", func(f *policyFields) { f.MonthlyQuota = pwInt(-1) }, "monthlyQuota cannot be negative"},
		{"most negative lifetime", func(f *policyFields) {
			f.MaxKeyLifetimeSeconds = pwInt(math.MinInt64)
		}, "maxKeyLifetimeSeconds cannot be negative"},
		{"most negative rate limit", func(f *policyFields) { f.RateLimit = pwInt(math.MinInt64) }, "rateLimit cannot be negative"},

		{"lifetime over 10 years", func(f *policyFields) {
			f.MaxKeyLifetimeSeconds = pwInt(315360001)
		}, "maxKeyLifetimeSeconds is at most 10 years"},
		{"lifetime that would overflow a duration", func(f *policyFields) {
			f.MaxKeyLifetimeSeconds = pwInt(math.MaxInt64)
		}, "maxKeyLifetimeSeconds is at most 10 years"},
		{"grace over 90 days", func(f *policyFields) { f.GraceSeconds = pwInt(7776001) }, "graceSeconds is at most 90 days"},
		{"rate limit too large", func(f *policyFields) {
			f.RateLimit = pwInt(1000000001)
			f.RateLimitWindowSeconds = pwInt(60)
		}, "rateLimit is too large"},
		{"rate limit beyond int", func(f *policyFields) {
			f.RateLimit = pwInt(math.MaxInt64)
			f.RateLimitWindowSeconds = pwInt(60)
		}, "rateLimit is too large"},
		{"window over 31 days", func(f *policyFields) {
			f.RateLimitWindowSeconds = pwInt(2678401)
		}, "rateLimitWindowSeconds is at most 31 days"},
		{"burst too large", func(f *policyFields) {
			f.RateLimit = pwInt(10)
			f.RateLimitWindowSeconds = pwInt(60)
			f.BurstLimit = pwInt(1000000001)
		}, "burstLimit is too large"},
		{"rotation over 10 years", func(f *policyFields) {
			f.RotationPeriodSeconds = pwInt(315360001)
		}, "rotationPeriodSeconds is at most 10 years"},

		{"rate limit with no window", func(f *policyFields) { f.RateLimit = pwInt(10) }, "a rate limit needs a window"},
		{"rate limit with a zero window", func(f *policyFields) {
			f.RateLimit = pwInt(10)
			f.RateLimitWindowSeconds = pwInt(0)
		}, "a rate limit needs a window"},
		{"burst with no rate limit", func(f *policyFields) {
			f.BurstLimit = pwInt(5)
			f.RateLimitWindowSeconds = pwInt(60)
		}, "a burst limit needs a rate limit"},

		{"too many scopes", func(f *policyFields) { f.AllowedScopes = pwRepeat("s", 101) }, "allowedScopes has more than 100 entries"},
		{"too many ips", func(f *policyFields) { f.AllowedIPs = pwRepeat("10.0.0.", 101) }, "allowedIps has more than 100 entries"},
		{"too many origins", func(f *policyFields) {
			l := pwRepeat("https://a", 101)
			for i := range *l {
				(*l)[i] += ".example"
			}
			f.AllowedOrigins = l
		}, "allowedOrigins has more than 100 entries"},
		{"too many methods", func(f *policyFields) { f.AllowedMethods = pwRepeat("M", 101) }, "allowedMethods has more than 100 entries"},
		{"too many paths", func(f *policyFields) { f.AllowedPaths = pwRepeat("/p", 101) }, "allowedPaths has more than 100 entries"},

		{"unknown scope", func(f *policyFields) { f.AllowedScopes = pwList("read", "nope") }, `scope "nope" does not exist in this tenant`},
		{"another tenant's scope", func(f *policyFields) { f.AllowedScopes = pwList("theirs") }, `scope "theirs" does not exist in this tenant`},
		{"bad ip", func(f *policyFields) { f.AllowedIPs = pwList("10.0.0.300") }, `allowedIps: "10.0.0.300" is not an IP address or CIDR range`},
		{"bad cidr", func(f *policyFields) { f.AllowedIPs = pwList("10.0.0.0/33") }, `allowedIps: "10.0.0.0/33" is not an IP address or CIDR range`},
		{"origin with no scheme", func(f *policyFields) { f.AllowedOrigins = pwList("example.com") }, `allowedOrigins: "example.com" is not an origin like https://example.com`},
		{"origin with a path", func(f *policyFields) {
			f.AllowedOrigins = pwList("https://example.com/app")
		}, `allowedOrigins: "https://example.com/app" is not an origin like https://example.com`},
		{"origin with a trailing slash", func(f *policyFields) {
			f.AllowedOrigins = pwList("https://example.com/")
		}, `allowedOrigins: "https://example.com/" is not an origin like https://example.com`},
		{"origin with a query", func(f *policyFields) {
			f.AllowedOrigins = pwList("https://example.com?x=1")
		}, `allowedOrigins: "https://example.com?x=1" is not an origin like https://example.com`},
		{"origin with another scheme", func(f *policyFields) {
			f.AllowedOrigins = pwList("ftp://example.com")
		}, `allowedOrigins: "ftp://example.com" is not an origin like https://example.com`},
		{"origin with no host", func(f *policyFields) { f.AllowedOrigins = pwList("https://") }, `allowedOrigins: "https://" is not an origin like https://example.com`},
		{"unknown method, upper-cased", func(f *policyFields) { f.AllowedMethods = pwList("fetch") }, `allowedMethods: "FETCH" is not an HTTP method`},
		{"relative path", func(f *policyFields) { f.AllowedPaths = pwList("v1/keys") }, `allowedPaths: "v1/keys" must start with /`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := pwFields("Standard")
			c.mutate(&in)
			_, err := policiesCreateHandler(deps)(context.Background(), in, principal())
			assert.Equal(t, c.want, badRequestMessage(t, err))
		})
	}

	list, err := eng.ListPolicies(tctx("t1"), &policy.ListFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Empty(t, list, "a refused create stores nothing")
}

func TestPoliciesCreateValidationOrder(t *testing.T) {
	deps, _ := setup(t, memory.New())
	h := policiesCreateHandler(deps)
	try := func(in policyFields) string {
		t.Helper()
		_, err := h(context.Background(), in, principal())
		return badRequestMessage(t, err)
	}

	// Everything is wrong at once; each fix reveals the next rule.
	in := policyFields{
		Name:                   pwStr(" "),
		Description:            pwStr(strings.Repeat("d", 1001)),
		MaxKeyLifetimeSeconds:  pwInt(315360001),
		GraceSeconds:           pwInt(-1),
		RateLimit:              pwInt(5),
		BurstLimit:             pwInt(-2),
		AllowedScopes:          pwList("nope"),
		AllowedIPs:             pwList("not-an-ip"),
		AllowedOrigins:         pwList("nope"),
		AllowedMethods:         pwList("nope"),
		AllowedPaths:           pwList("nope"),
		RateLimitWindowSeconds: nil,
	}
	assert.Equal(t, "name is required", try(in))
	in.Name = pwStr("n")
	assert.Equal(t, "description is too long", try(in))
	in.Description = nil
	// Negatives run before caps, in field order.
	assert.Equal(t, "graceSeconds cannot be negative", try(in))
	in.GraceSeconds = nil
	assert.Equal(t, "burstLimit cannot be negative", try(in))
	in.BurstLimit = pwInt(1000000001)
	// Caps run in field order too: lifetime comes before burst.
	assert.Equal(t, "maxKeyLifetimeSeconds is at most 10 years", try(in))
	in.MaxKeyLifetimeSeconds = nil
	assert.Equal(t, "burstLimit is too large", try(in))
	in.BurstLimit = pwInt(3)
	assert.Equal(t, "a rate limit needs a window", try(in))
	in.RateLimitWindowSeconds = pwInt(60)
	assert.Equal(t, `scope "nope" does not exist in this tenant`, try(in))
	in.AllowedScopes = nil
	assert.Equal(t, `allowedIps: "not-an-ip" is not an IP address or CIDR range`, try(in))
	in.AllowedIPs = nil
	assert.Equal(t, `allowedOrigins: "nope" is not an origin like https://example.com`, try(in))
	in.AllowedOrigins = nil
	assert.Equal(t, `allowedMethods: "NOPE" is not an HTTP method`, try(in))
	in.AllowedMethods = nil
	assert.Equal(t, `allowedPaths: "nope" must start with /`, try(in))

	// Within a list, the entries are checked in sorted order.
	in.AllowedPaths = pwList("zz", "aa")
	assert.Equal(t, `allowedPaths: "aa" must start with /`, try(in))
	// The entry count is checked before any entry.
	in.AllowedPaths = pwRepeat("x", 101)
	assert.Equal(t, "allowedPaths has more than 100 entries", try(in))
}

func TestPoliciesCreateAcceptsEveryBoundary(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		got := pwCreate(t, deps, policyFields{
			Name:                   pwStr("  " + strings.Repeat("é", 200) + " "),
			Description:            pwStr(" " + strings.Repeat("é", 1000) + "\n"),
			MaxKeyLifetimeSeconds:  pwInt(315360000),
			GraceSeconds:           pwInt(7776000),
			AllowedScopes:          pwList("read"),
			RateLimit:              pwInt(1000000000),
			RateLimitWindowSeconds: pwInt(2678400),
			BurstLimit:             pwInt(1000000000),
			AllowedIPs:             pwList("192.168.1.1", "10.0.0.0/8", "::1", "2001:db8::/32"),
			AllowedOrigins:         pwList("*", "http://localhost:3000", "https://example.com"),
			AllowedMethods:         pwList("GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"),
			AllowedPaths:           pwRepeat("/p", 100),
			RotationPeriodSeconds:  pwInt(315360000),
			DailyQuota:             pwInt(math.MaxInt64),
			MonthlyQuota:           pwInt(1),
		})
		assert.Equal(t, strings.Repeat("é", 200), got.Name, "the name is trimmed")
		assert.Equal(t, strings.Repeat("é", 1000), got.Description, "the description is trimmed")
		assert.Equal(t, int64(315360000), *got.MaxKeyLifetimeSeconds)
		assert.Equal(t, int64(7776000), *got.GraceSeconds)
		assert.Equal(t, int64(1000000000), *got.RateLimit)
		assert.Equal(t, int64(2678400), *got.RateLimitWindowSeconds)
		assert.Equal(t, int64(1000000000), *got.BurstLimit)
		assert.Equal(t, int64(315360000), *got.RotationPeriodSeconds)
		assert.Equal(t, int64(math.MaxInt64), *got.DailyQuota)
		assert.Equal(t, int64(1), *got.MonthlyQuota)
		assert.Len(t, got.AllowedPaths, 100)
		assert.Equal(t, []string{"DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"}, got.AllowedMethods)

		// What came back is what was stored.
		assert.Equal(t, got, policyDetail(t, deps, pwPolicyID(t, got.ID)).Policy)
	})
}

// A bare create stores nothing but the name, and every unset field answers
// null or [].
func TestPoliciesCreateWithOnlyAName(t *testing.T) {
	deps, _ := setup(t, memory.New())
	got := pwCreate(t, deps, pwFields("Bare"))
	b, err := json.Marshal(policyResponse{Policy: got})
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
}

// The engine reads the tenant from the context and prefers a forge Scope over
// keysmith.WithTenant. A host that sets one on every request must not decide
// where a policy lands.
func TestPoliciesCreateStoresUnderTheContractTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		p := user(map[string]any{"tenant_id": "t1", "app_id": "app9"})
		ctx := forge.WithScope(context.Background(), forge.NewOrgScope("app_x", "org_other"))

		in := pwFields("Standard")
		in.AllowedScopes = pwList("read")
		out, err := policiesCreateHandler(deps)(ctx, in, p)
		require.NoError(t, err)

		stored, err := s.Policies().Get(context.Background(), pwPolicyID(t, out.Policy.ID))
		require.NoError(t, err)
		assert.Equal(t, "t1", stored.TenantID)
		assert.Equal(t, "app9", stored.AppID)
		assert.Equal(t, []string{"read"}, out.Policy.AllowedScopes)

		detail, err := policiesDetailHandler(deps)(ctx, policyIDRequest{ID: out.Policy.ID}, p)
		require.NoError(t, err)
		assert.Equal(t, out.Policy.ID, detail.Policy.ID)

		list, err := policiesListHandler(deps)(ctx, pickerRequest{}, p)
		require.NoError(t, err)
		require.Len(t, list.Policies, 1)
		assert.Equal(t, out.Policy.ID, list.Policies[0].ID)
	})
}

func TestPoliciesCreateNormalisesLists(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		mkScope(t, eng, "t1", "write", "")
		in := pwFields("Standard")
		in.AllowedMethods = pwList("get", "GET", " post ")
		in.AllowedScopes = pwList("write", " read", "", "write")
		in.AllowedIPs = pwList(" 10.0.0.0/8 ", "  ", "10.0.0.0/8")
		in.AllowedOrigins = pwList("https://b.example", "https://a.example", "https://b.example")
		in.AllowedPaths = pwList("/v2", "/v1", "\t/v1")
		out, err := policiesCreateHandler(deps)(context.Background(), in, principal())
		require.NoError(t, err)

		stored, err := s.Policies().Get(context.Background(), pwPolicyID(t, out.Policy.ID))
		require.NoError(t, err)
		for _, got := range [][]string{stored.AllowedMethods, out.Policy.AllowedMethods} {
			assert.Equal(t, []string{"GET", "POST"}, got)
		}
		for _, got := range [][]string{stored.AllowedScopes, out.Policy.AllowedScopes} {
			assert.Equal(t, []string{"read", "write"}, got)
		}
		assert.Equal(t, []string{"10.0.0.0/8"}, stored.AllowedIPs)
		assert.Equal(t, []string{"https://a.example", "https://b.example"}, stored.AllowedOrigins)
		assert.Equal(t, []string{"/v1", "/v2"}, stored.AllowedPaths)
	})
}

// 101 entries that collapse to fewer than 100 are fine: the count is taken
// after blanks and duplicates go.
func TestPoliciesCreateCountsEntriesAfterNormalising(t *testing.T) {
	deps, _ := setup(t, memory.New())
	in := pwFields("Standard")
	l := append(*pwRepeat("/p", 100), "/p0", " ", "")
	in.AllowedPaths = &l
	got := pwCreate(t, deps, in)
	assert.Len(t, got.AllowedPaths, 100)
}

func pwFull() policyFields {
	return policyFields{
		Name:                   pwStr("Standard"),
		Description:            pwStr("the usual limits"),
		MaxKeyLifetimeSeconds:  pwInt(90 * 24 * 3600),
		GraceSeconds:           pwInt(3600),
		AllowedScopes:          pwList("read"),
		RateLimit:              pwInt(100),
		RateLimitWindowSeconds: pwInt(60),
		BurstLimit:             pwInt(20),
		AllowedIPs:             pwList("10.0.0.0/8"),
		AllowedOrigins:         pwList("https://example.com"),
		AllowedMethods:         pwList("GET"),
		AllowedPaths:           pwList("/v1"),
		RotationPeriodSeconds:  pwInt(30 * 24 * 3600),
		DailyQuota:             pwInt(1000),
		MonthlyQuota:           pwInt(25000),
	}
}

func TestPoliciesUpdateLeavesOmittedFieldsAlone(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		before := pwCreate(t, deps, pwFull())

		out, err := pwUpdate(deps, before.ID, policyFields{Description: pwStr("  new words ")})
		require.NoError(t, err)
		after := out.Policy
		assert.Equal(t, "new words", after.Description)
		assert.Equal(t, int64(100), *after.RateLimit)
		assert.Equal(t, []string{"read"}, after.AllowedScopes)

		// Every other field is as it was, and the next read agrees.
		want := before
		want.Description = "new words"
		want.UpdatedAt = after.UpdatedAt
		assert.Equal(t, want, after)
		assert.Equal(t, after, policyDetail(t, deps, pwPolicyID(t, before.ID)).Policy)
	})
}

func TestPoliciesUpdateClearsWithZeroAndEmpty(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		before := pwCreate(t, deps, pwFull())

		out, err := pwUpdate(deps, before.ID, policyFields{
			RateLimit:              pwInt(0),
			RateLimitWindowSeconds: pwInt(0),
			BurstLimit:             pwInt(0),
			AllowedScopes:          pwList(),
			Description:            pwStr(""),
		})
		require.NoError(t, err)
		for _, got := range []PolicyDetail{out.Policy, policyDetail(t, deps, pwPolicyID(t, before.ID)).Policy} {
			assert.Nil(t, got.RateLimit)
			assert.Nil(t, got.RateLimitWindowSeconds)
			assert.Nil(t, got.BurstLimit)
			assert.Equal(t, []string{}, got.AllowedScopes)
			assert.Empty(t, got.Description)
			assert.Equal(t, []string{"GET"}, got.AllowedMethods, "an omitted list is left alone")
		}

		b, err := json.Marshal(out)
		require.NoError(t, err)
		body := string(b)
		assert.Contains(t, body, `"rateLimit":null`)
		assert.Contains(t, body, `"rateLimitWindowSeconds":null`)
		assert.Contains(t, body, `"allowedScopes":[]`)
	})
}

// Validation runs on the merged policy, so an edit that leaves the stored
// rate limit without its window is refused, and a refused edit changes
// nothing.
func TestPoliciesUpdateValidatesTheMergedPolicy(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		in := pwFull()
		in.AllowedMethods = pwList("POST", "GET")
		before := pwCreate(t, deps, in)
		storedBefore, err := s.Policies().Get(context.Background(), pwPolicyID(t, before.ID))
		require.NoError(t, err)

		_, err = pwUpdate(deps, before.ID, policyFields{RateLimitWindowSeconds: pwInt(0)})
		assert.Equal(t, "a rate limit needs a window", badRequestMessage(t, err))
		_, err = pwUpdate(deps, before.ID, policyFields{Name: pwStr(" ")})
		assert.Equal(t, "name is required", badRequestMessage(t, err))
		_, err = pwUpdate(deps, before.ID, policyFields{
			AllowedMethods: pwList("put", "fetch"),
			Description:    pwStr("changed"),
		})
		assert.Equal(t, `allowedMethods: "FETCH" is not an HTTP method`, badRequestMessage(t, err))

		storedAfter, err := s.Policies().Get(context.Background(), pwPolicyID(t, before.ID))
		require.NoError(t, err)
		assert.Equal(t, projectPolicyDetail(storedBefore), projectPolicyDetail(storedAfter))
		assert.Equal(t, "the usual limits", storedAfter.Description)
	})
}

func TestPoliciesUpdateIsTenantScoped(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := mkPolicy(t, eng, "t2", &policy.Policy{Name: "theirs", Description: "keep"})

		notFound := &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "policy not found"}
		_, foreign := pwUpdate(deps, theirs.ID.String(), policyFields{Description: pwStr("mine now")})
		_, missing := pwUpdate(deps, id.NewPolicyID().String(), policyFields{Description: pwStr("x")})
		assert.Equal(t, notFound, foreign)
		assert.Equal(t, notFound, missing)

		stored, err := s.Policies().Get(context.Background(), theirs.ID)
		require.NoError(t, err)
		assert.Equal(t, "keep", stored.Description)
		assert.Equal(t, "t2", stored.TenantID)

		_, err = pwUpdate(deps, "", policyFields{})
		assert.Equal(t, "id is required", badRequestMessage(t, err))
		_, err = pwUpdate(deps, id.NewKeyID().String(), policyFields{})
		assert.Equal(t, "id is not a policy id", badRequestMessage(t, err))

		mine := pwCreate(t, deps, pwFields("mine"))
		out, err := pwUpdate(deps, " "+mine.ID+"  ", policyFields{Description: pwStr("trimmed id")})
		require.NoError(t, err)
		assert.Equal(t, "trimmed id", out.Policy.Description)
	})
}

func TestPoliciesWriteRefusesADuplicateName(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		taken := &dashcontract.Error{Code: dashcontract.CodeConflict, Message: "a policy with this name already exists"}
		// Another tenant's policy of the same name does not count.
		mkPolicy(t, eng, "t2", &policy.Policy{Name: "Standard"})

		a := pwCreate(t, deps, pwFields("Standard"))
		b := pwCreate(t, deps, pwFields("Strict"))

		_, err := policiesCreateHandler(deps)(context.Background(), pwFields(" Standard "), principal())
		assert.Equal(t, taken, err)

		_, err = pwUpdate(deps, b.ID, policyFields{Name: pwStr("Standard")})
		assert.Equal(t, taken, err)
		stored, err := s.Policies().Get(context.Background(), pwPolicyID(t, b.ID))
		require.NoError(t, err)
		assert.Equal(t, "Strict", stored.Name)

		// Keeping its own name is not a clash.
		out, err := pwUpdate(deps, a.ID, policyFields{Name: pwStr("Standard"), Description: pwStr("same name")})
		require.NoError(t, err)
		assert.Equal(t, "Standard", out.Policy.Name)
	})
}

func TestPoliciesWriteRefusesWithoutAUserOrTenant(t *testing.T) {
	deps, eng := setup(t, memory.New())
	pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "p"})
	ctx := context.Background()

	_, err := policiesCreateHandler(deps)(ctx, pwFields("x"), dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))
	_, err = policiesUpdateHandler(deps)(ctx, policiesUpdateRequest{ID: pol.ID.String()}, dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))

	deps.DefaultTenantID = ""
	_, err = policiesCreateHandler(deps)(ctx, pwFields("x"), principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
	_, err = policiesUpdateHandler(deps)(ctx, policiesUpdateRequest{ID: pol.ID.String()}, principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))

	deps.DefaultTenantID = "t1"
	_, err = policiesCreateHandler(deps)(ctx, pwFields("x"), user(map[string]any{"app_id": 7}))
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err), "a broken app claim refuses")

	list, err := eng.ListPolicies(tctx("t1"), &policy.ListFilter{TenantID: "t1"})
	require.NoError(t, err)
	assert.Len(t, list, 1, "no refused create stored anything")
}

// Through the dispatcher, JSON null and a missing field both mean "leave it
// alone", and 0 or [] clear.
func TestPoliciesWritesAreDispatchedAsWriteCommands(t *testing.T) {
	deps, eng := setup(t, memory.New())
	mkScope(t, eng, "t1", "read", "")
	d := createTestDispatcher(t, deps)

	dispatch := func(intent, payload string) map[string]any {
		t.Helper()
		data, _, err := d.Dispatch(context.Background(), dashcontract.Request{
			Envelope: "v1", Kind: dashcontract.KindCommand, Contributor: ContributorName,
			Intent: intent, IntentVersion: 1, Payload: json.RawMessage(payload),
		}, principal())
		require.NoError(t, err)
		var out struct {
			Policy map[string]any `json:"policy"`
		}
		require.NoError(t, json.Unmarshal(data, &out))
		return out.Policy
	}

	created := dispatch("policies.create",
		`{"name":"Standard","rateLimit":100,"rateLimitWindowSeconds":60,"allowedScopes":["read"],"allowedIps":["10.0.0.1"]}`)
	assert.Equal(t, "Standard", created["name"])
	assert.InDelta(t, 100, created["rateLimit"], 0)
	polID := created["id"].(string)

	updated := dispatch("policies.update",
		`{"id":"`+polID+`","rateLimit":null,"allowedScopes":[],"description":"edited"}`)
	assert.InDelta(t, 100, updated["rateLimit"], 0, "null leaves a field alone")
	assert.InDelta(t, 60, updated["rateLimitWindowSeconds"], 0, "a missing field is left alone")
	assert.Equal(t, []any{}, updated["allowedScopes"], "[] clears a list")
	assert.Equal(t, []any{"10.0.0.1"}, updated["allowedIps"])
	assert.Equal(t, "edited", updated["description"])

	cleared := dispatch("policies.update", `{"id":"`+polID+`","rateLimit":0,"rateLimitWindowSeconds":0}`)
	assert.Nil(t, cleared["rateLimit"])
	assert.Nil(t, cleared["rateLimitWindowSeconds"])
}

func TestPolicyWriteIntentsAreWriteCommands(t *testing.T) {
	m, err := loader.Load(bytes.NewReader(manifestYAML), "keysmith/contract/manifest.yaml")
	require.NoError(t, err)
	want := map[string][]string{
		"policies.create": {"policies.list", "overview"},
		"policies.update": {"policies.list", "policies.detail", "keys.detail"},
	}
	found := 0
	for _, in := range m.Intents {
		inv, ok := want[in.Name]
		if !ok {
			continue
		}
		found++
		assert.Equal(t, dashcontract.IntentKindCommand, in.Kind, in.Name)
		assert.EqualValues(t, "write", in.Capability, in.Name)
		assert.Equal(t, 1, in.Version, in.Name)
		assert.Equal(t, inv, in.Invalidates, in.Name)
	}
	assert.Equal(t, len(want), found)
}

func pwDelete(deps Deps, polID string) (policiesDeleteResponse, error) {
	return policiesDeleteHandler(deps)(context.Background(), policyIDRequest{ID: polID}, principal())
}

func pwKeyWithPolicy(polID id.PolicyID) *keysmith.CreateKeyInput {
	return &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &polID}
}

// pwForeignKey writes a key in tenant t2 that names polID straight to the
// store. The contract (keys.create) would refuse it, and the engine's
// CreateKey does not check the policy's tenant.
func pwForeignKey(t *testing.T, s store.Store, polID id.PolicyID, state key.State) {
	t.Helper()
	now := time.Now()
	require.NoError(t, s.Keys().Create(context.Background(), &key.Key{
		ID: id.NewKeyID(), TenantID: "t2", AppID: "app", Name: "theirs",
		Prefix: "sk", Hint: "zzzz", KeyHash: "hash-" + id.NewKeyID().String(),
		Environment: key.EnvLive, State: state, PolicyID: &polID,
		CreatedAt: now, UpdatedAt: now,
	}))
}

func TestPoliciesDeleteRemovesAnUnusedPolicy(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "Standard"})
		other := mkPolicy(t, eng, "t1", &policy.Policy{Name: "Other"})

		out, err := pwDelete(deps, " "+pol.ID.String()+" ")
		require.NoError(t, err)
		assert.Equal(t, policiesDeleteResponse{ID: pol.ID.String()}, out)

		_, err = policiesDetailHandler(deps)(context.Background(), policyIDRequest{ID: pol.ID.String()}, principal())
		assert.Equal(t, &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "policy not found"}, err)
		_, err = pwDelete(deps, pol.ID.String())
		assert.Equal(t, &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "policy not found"}, err,
			"a second delete finds nothing")

		// Only that policy went.
		assert.Equal(t, "Other", policyDetail(t, deps, other.ID).Policy.Name)
	})
}

// Rex's ruling: revoked keys never block a delete. Active, suspended and
// expired ones do, and the refusal says how many.
func TestPoliciesDeleteRefusesWhileAKeyThatIsNotRevokedUsesIt(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "Standard"})
		live := create(t, eng, "t1", pwKeyWithPolicy(pol.ID))
		revoked := create(t, eng, "t1", pwKeyWithPolicy(pol.ID))
		require.NoError(t, eng.RevokeKey(tctx("t1"), revoked.Key.ID, "done"))

		_, err := pwDelete(deps, pol.ID.String())
		assert.Equal(t, &dashcontract.Error{
			Code: dashcontract.CodeConflict, Message: "1 key that is not revoked uses this policy",
		}, err)

		suspended := create(t, eng, "t1", pwKeyWithPolicy(pol.ID))
		require.NoError(t, eng.SuspendKey(tctx("t1"), suspended.Key.ID))
		_, err = pwDelete(deps, pol.ID.String())
		assert.Equal(t, &dashcontract.Error{
			Code: dashcontract.CodeConflict, Message: "2 keys that are not revoked use this policy",
		}, err)

		// A refused delete leaves the policy where it was.
		assert.Equal(t, "Standard", policyDetail(t, deps, pol.ID).Policy.Name)

		require.NoError(t, eng.RevokeKey(tctx("t1"), live.Key.ID, "done"))
		require.NoError(t, eng.RevokeKey(tctx("t1"), suspended.Key.ID, "done"))
		out, err := pwDelete(deps, pol.ID.String())
		require.NoError(t, err)
		assert.Equal(t, pol.ID.String(), out.ID)

		// The revoked key still names the policy, and its page reads it as
		// gone.
		got := stateDetail(t, deps, revoked.Key.ID)
		assert.Nil(t, got.Policy)
		assert.Equal(t, pol.ID.String(), got.Key.PolicyID)
		b, err := json.Marshal(got)
		require.NoError(t, err)
		assert.Contains(t, string(b), `"policy":null`)
	})
}

// The engine counts keys in every tenant; the message counts only this
// one's. When only another tenant's key blocks, the refusal names no count
// rather than saying 0.
func TestPoliciesDeleteBlockedOnlyByAnotherTenantsKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "Standard"})
		revoked := create(t, eng, "t1", pwKeyWithPolicy(pol.ID))
		require.NoError(t, eng.RevokeKey(tctx("t1"), revoked.Key.ID, "done"))
		pwForeignKey(t, s, pol.ID, key.StateActive)

		_, err := pwDelete(deps, pol.ID.String())
		assert.Equal(t, &dashcontract.Error{
			Code: dashcontract.CodeConflict, Message: "keys that are not revoked use this policy",
		}, err)
		assert.Equal(t, "Standard", policyDetail(t, deps, pol.ID).Policy.Name)

		// With a key of this tenant blocking too, the count is this
		// tenant's alone.
		create(t, eng, "t1", pwKeyWithPolicy(pol.ID))
		_, err = pwDelete(deps, pol.ID.String())
		assert.Equal(t, &dashcontract.Error{
			Code: dashcontract.CodeConflict, Message: "1 key that is not revoked uses this policy",
		}, err)
	})
}

// pwFlakyKeys fails ListByPolicy from its failFrom-th call on.
type pwFlakyKeys struct {
	key.Store
	calls    int
	failFrom int
}

func (w *pwFlakyKeys) ListByPolicy(ctx context.Context, polID id.PolicyID) ([]*key.Key, error) {
	w.calls++
	if w.calls >= w.failFrom {
		return nil, createPlainErr("dial tcp 10.0.0.1: secret-connection-detail")
	}
	return w.Store.ListByPolicy(ctx, polID)
}

// When the engine refuses and the count read then fails, the answer is
// still the CONFLICT, without a number, and the failure is logged.
func TestPoliciesDeleteRefusesWithoutACountWhenTheCountFails(t *testing.T) {
	s := memory.New()
	_, eng := setup(t, s)
	pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "Standard"})
	create(t, eng, "t1", pwKeyWithPolicy(pol.ID))

	keys := &pwFlakyKeys{Store: s.Keys(), failFrom: 2}
	flaky, err := keysmith.NewEngine(keysmith.WithStore(stateRaceStore{Store: s, keys: keys}))
	require.NoError(t, err)
	var lines []rotateLogLine
	deps := Deps{Engine: flaky, DefaultTenantID: "t1", Logger: rotateCapturingLogger{lines: &lines}}

	_, err = pwDelete(deps, pol.ID.String())
	assert.Equal(t, &dashcontract.Error{
		Code: dashcontract.CodeConflict, Message: "keys that are not revoked use this policy",
	}, err)
	assert.Equal(t, 2, keys.calls, "the engine's check, then the count")
	require.Len(t, lines, 1)
	assert.Equal(t, "policies.delete", lines[0].fields["intent"])
	assert.Contains(t, lines[0].fields["error"], "secret-connection-detail")

	// The engine's own read failing is INTERNAL and says nothing more.
	keys.calls, keys.failFrom = 0, 1
	_, err = pwDelete(deps, pol.ID.String())
	assert.Equal(t, &dashcontract.Error{Code: dashcontract.CodeInternal, Message: "an internal error occurred"}, err)
	_, err = s.Policies().Get(context.Background(), pol.ID)
	require.NoError(t, err, "no failed delete removed the policy")
}

func TestPoliciesDeleteIsTenantScoped(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := mkPolicy(t, eng, "t2", &policy.Policy{Name: "theirs"})

		notFound := &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "policy not found"}
		_, foreign := pwDelete(deps, theirs.ID.String())
		_, missing := pwDelete(deps, id.NewPolicyID().String())
		assert.Equal(t, notFound, foreign)
		assert.Equal(t, notFound, missing)

		stored, err := s.Policies().Get(context.Background(), theirs.ID)
		require.NoError(t, err, "another tenant's policy is still there")
		assert.Equal(t, "t2", stored.TenantID)

		_, err = pwDelete(deps, "")
		assert.Equal(t, "id is required", badRequestMessage(t, err))
		_, err = pwDelete(deps, id.NewKeyID().String())
		assert.Equal(t, "id is not a policy id", badRequestMessage(t, err))
	})
}

func TestPoliciesDeleteRefusesWithoutAUserOrTenant(t *testing.T) {
	deps, eng := setup(t, memory.New())
	pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "p"})
	ctx := context.Background()
	h := policiesDeleteHandler

	_, err := h(deps)(ctx, policyIDRequest{ID: pol.ID.String()}, dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))
	deps.DefaultTenantID = ""
	_, err = h(deps)(ctx, policyIDRequest{ID: pol.ID.String()}, principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))

	_, err = eng.GetPolicy(ctx, pol.ID)
	require.NoError(t, err, "a refused delete removed nothing")
}

func TestPoliciesDeleteIsDispatchedAsAWriteCommand(t *testing.T) {
	deps, eng := setup(t, memory.New())
	pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "p"})
	d := createTestDispatcher(t, deps)

	data, _, err := d.Dispatch(context.Background(), dashcontract.Request{
		Envelope: "v1", Kind: dashcontract.KindCommand, Contributor: ContributorName,
		Intent: "policies.delete", IntentVersion: 1, Payload: json.RawMessage(`{"id":"` + pol.ID.String() + `"}`),
	}, principal())
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"`+pol.ID.String()+`"}`, string(data))

	m, err := loader.Load(bytes.NewReader(manifestYAML), "keysmith/contract/manifest.yaml")
	require.NoError(t, err)
	found := false
	for _, in := range m.Intents {
		if in.Name != "policies.delete" {
			continue
		}
		found = true
		assert.Equal(t, dashcontract.IntentKindCommand, in.Kind)
		assert.EqualValues(t, "write", in.Capability)
		assert.Equal(t, 1, in.Version)
		assert.Equal(t, []string{"policies.list", "policies.detail", "overview"}, in.Invalidates)
	}
	assert.True(t, found, "policies.delete is in the manifest")
}
