package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/internal/storetest"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

var createTenantFilter = key.ListFilter{TenantID: "t1"}

func createMemoryStore() store.Store { return memory.New() }

func createMustKeyID(t *testing.T, s string) id.ID {
	t.Helper()
	kid, err := id.ParseKeyID(s)
	require.NoError(t, err)
	return kid
}

func policyIDString() string { return id.NewPolicyID().String() }

type createPlainError string

func (e createPlainError) Error() string { return string(e) }

func createPlainErr(msg string) error { return createPlainError(msg) }

func createTestDispatcher(t *testing.T, deps Deps) *dispatcher.Dispatcher {
	t.Helper()
	d := dispatcher.New(nil)
	require.NoError(t, Register(d, dashcontract.NewRegistry(), dashcontract.NewWardenRegistry(), deps))
	return d
}

func validCreate() keysCreateRequest {
	return keysCreateRequest{Name: "ci deploy", Environment: "live", Prefix: "sk"}
}

func badRequestMessage(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, dashcontract.CodeBadRequest, codeOf(t, err))
	var ce *dashcontract.Error
	require.ErrorAs(t, err, &ce)
	return ce.Message
}

func TestKeysCreateStoresTheResolvedTenantAndSubject(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		deps.DefaultTenantID = "other-default"
		// The claim wins over the different default.
		p := user(map[string]any{"tenant_id": "t1", "app_id": "app9"})
		out, err := keysCreateHandler(deps)(context.Background(), validCreate(), p)
		require.NoError(t, err)

		stored, err := s.Keys().Get(context.Background(), createMustKeyID(t, out.Key.ID))
		require.NoError(t, err)
		assert.Equal(t, "t1", stored.TenantID)
		assert.Equal(t, "user_1", stored.CreatedBy)
		assert.Equal(t, "user_1", out.Key.CreatedBy)
		assert.Equal(t, "ci deploy", out.Key.Name)
		assert.Equal(t, "sk", out.Key.Prefix)
		assert.Equal(t, "live", out.Key.Environment)
		assert.Equal(t, "active", out.Key.State)
		assert.Equal(t, []string{}, out.Key.Scopes)

		// And it is visible to that tenant, not to the default one.
		_, err = eng.GetKey(tctx("t1"), createMustKeyID(t, out.Key.ID))
		require.NoError(t, err)
	})
}

func TestKeysCreateDefaultTenantAndSubject(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, _ := setup(t, s)
		out, err := keysCreateHandler(deps)(context.Background(), validCreate(), principal())
		require.NoError(t, err)
		stored, err := s.Keys().Get(context.Background(), createMustKeyID(t, out.Key.ID))
		require.NoError(t, err)
		assert.Equal(t, "t1", stored.TenantID)
		assert.Equal(t, "user_1", stored.CreatedBy)
	})
}

func TestKeysCreateRawKeyValidatesAndEndsInTheHint(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		out, err := keysCreateHandler(deps)(context.Background(), validCreate(), principal())
		require.NoError(t, err)

		require.NotEmpty(t, out.RawKey)
		assert.True(t, strings.HasSuffix(out.RawKey, out.Key.Hint),
			"raw key (length %d) does not end in the hint %q", len(out.RawKey), out.Key.Hint)
		assert.True(t, strings.HasPrefix(out.RawKey, "sk_live_"), "raw key (length %d) lacks the prefix and environment", len(out.RawKey))

		res, err := eng.ValidateKey(tctx("t1"), out.RawKey)
		require.NoError(t, err, "raw key of length %d did not validate", len(out.RawKey))
		assert.Equal(t, out.Key.ID, res.Key.ID.String())
	})
}

func TestKeysCreateRefusesAnotherTenantsPolicyAndARandomOneIdentically(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := mkPolicy(t, eng, "t2", &policy.Policy{Name: "theirs"})

		foreign := validCreate()
		foreign.PolicyID = theirs.ID.String()
		_, errForeign := keysCreateHandler(deps)(context.Background(), foreign, principal())

		random := validCreate()
		random.PolicyID = policyIDString()
		_, errRandom := keysCreateHandler(deps)(context.Background(), random, principal())

		malformed := validCreate()
		malformed.PolicyID = "not-an-id"
		_, errMalformed := keysCreateHandler(deps)(context.Background(), malformed, principal())

		for _, e := range []error{errForeign, errRandom, errMalformed} {
			assert.Equal(t, "policy not found", badRequestMessage(t, e))
		}
		assert.Equal(t, errForeign, errRandom, "a foreign ID must look exactly like a missing one")

		// Nothing was written.
		total, err := s.Keys().Count(context.Background(), &createTenantFilter)
		require.NoError(t, err)
		assert.Zero(t, total)
	})
}

func TestKeysCreateWithAnOwnPolicy(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "mine", MaxKeyLifetime: 30 * 24 * time.Hour})
		in := validCreate()
		in.PolicyID = pol.ID.String()
		out, err := keysCreateHandler(deps)(context.Background(), in, principal())
		require.NoError(t, err)
		assert.Equal(t, pol.ID.String(), out.Key.PolicyID)
		assert.NotEmpty(t, out.Key.ExpiresAt, "the policy cap becomes the default expiry")
	})
}

func TestKeysCreateValidationMessages(t *testing.T) {
	deps, _ := setup(t, createMemoryStore())
	cases := []struct {
		name   string
		mutate func(*keysCreateRequest)
		want   string
	}{
		{"blank name", func(r *keysCreateRequest) { r.Name = "   " }, "name is required"},
		{"name over 200", func(r *keysCreateRequest) { r.Name = strings.Repeat("a", 201) }, "name is too long"},
		{"description over 1000", func(r *keysCreateRequest) { r.Description = strings.Repeat("a", 1001) }, "description is too long"},
		{"description over 1000 runes", func(r *keysCreateRequest) { r.Description = strings.Repeat("é", 1001) }, "description is too long"},
		{"environment empty", func(r *keysCreateRequest) { r.Environment = "" }, "environment must be one of live, test, staging"},
		{"environment unknown", func(r *keysCreateRequest) { r.Environment = "prod" }, "environment must be one of live, test, staging"},
		{"prefix empty", func(r *keysCreateRequest) { r.Prefix = "" }, prefixMsg},
		{"prefix one char", func(r *keysCreateRequest) { r.Prefix = "s" }, prefixMsg},
		{"prefix 17 chars", func(r *keysCreateRequest) { r.Prefix = "abcdefghijklmnopq" }, prefixMsg},
		{"prefix uppercase", func(r *keysCreateRequest) { r.Prefix = "SK" }, prefixMsg},
		{"prefix underscore", func(r *keysCreateRequest) { r.Prefix = "sk_x" }, prefixMsg},
		{"prefix leading digit", func(r *keysCreateRequest) { r.Prefix = "1sk" }, prefixMsg},
		{"expiresAt not rfc3339", func(r *keysCreateRequest) { r.ExpiresAt = "tomorrow" }, expiresMsg},
		{"expiresAt in the past", func(r *keysCreateRequest) {
			r.ExpiresAt = time.Now().Add(-time.Hour).Format(time.RFC3339)
		}, expiresMsg},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := validCreate()
			c.mutate(&in)
			_, err := keysCreateHandler(deps)(context.Background(), in, principal())
			assert.Equal(t, c.want, badRequestMessage(t, err))
		})
	}
}

const (
	prefixMsg  = "prefix must be 2 to 16 lowercase letters or digits, starting with a letter"
	expiresMsg = "expiresAt must be an RFC3339 timestamp in the future"
)

func TestKeysCreateValidationOrder(t *testing.T) {
	deps, _ := setup(t, createMemoryStore())
	// Everything is wrong at once: the first rule in the list answers.
	in := keysCreateRequest{Name: "", Environment: "x", Prefix: "X", ExpiresAt: "x", PolicyID: "x"}
	_, err := keysCreateHandler(deps)(context.Background(), in, principal())
	assert.Equal(t, "name is required", badRequestMessage(t, err))
	in.Name = "n"
	_, err = keysCreateHandler(deps)(context.Background(), in, principal())
	assert.Contains(t, badRequestMessage(t, err), "environment")
	in.Environment = "live"
	_, err = keysCreateHandler(deps)(context.Background(), in, principal())
	assert.Contains(t, badRequestMessage(t, err), "prefix")
	in.Prefix = "sk"
	_, err = keysCreateHandler(deps)(context.Background(), in, principal())
	assert.Contains(t, badRequestMessage(t, err), "expiresAt")
	in.ExpiresAt = ""
	_, err = keysCreateHandler(deps)(context.Background(), in, principal())
	assert.Equal(t, "policy not found", badRequestMessage(t, err))
}

func TestKeysCreateNameBoundaryAndTrimming(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, _ := setup(t, s)
		in := validCreate()
		in.Name = "  " + strings.Repeat("é", 200) + "  "
		out, err := keysCreateHandler(deps)(context.Background(), in, principal())
		require.NoError(t, err)
		assert.Equal(t, strings.Repeat("é", 200), out.Key.Name)
	})
}

// The cap counts runes, not bytes, and the surrounding space is trimmed
// before it is counted and before anything is stored.
func TestKeysCreateDescriptionBoundaryAndTrimming(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, _ := setup(t, s)
		in := validCreate()
		in.Description = "  " + strings.Repeat("é", 1000) + "\n "
		out, err := keysCreateHandler(deps)(context.Background(), in, principal())
		require.NoError(t, err)
		assert.Equal(t, strings.Repeat("é", 1000), out.Key.Description)
		stored, err := s.Keys().Get(context.Background(), createMustKeyID(t, out.Key.ID))
		require.NoError(t, err)
		assert.Equal(t, strings.Repeat("é", 1000), stored.Description)
	})
}

func TestKeysCreateRefusesWithoutAUserOrTenant(t *testing.T) {
	deps, _ := setup(t, createMemoryStore())
	_, err := keysCreateHandler(deps)(context.Background(), validCreate(), dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))

	deps.DefaultTenantID = ""
	_, err = keysCreateHandler(deps)(context.Background(), validCreate(), principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))

	_, err = keysCreateHandler(deps)(context.Background(), validCreate(), user(map[string]any{"tenant_id": "t1", "app_id": 7}))
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
}

func TestKeysCreateScopeErrors(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		mkScope(t, eng, "t1", "write", "")
		mkScope(t, eng, "t2", "theirs", "")
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "readonly", AllowedScopes: []string{"read"}})

		in := validCreate()
		in.Scopes = []string{"read", "nope"}
		_, err := keysCreateHandler(deps)(context.Background(), in, principal())
		assert.Equal(t, `scope "nope" does not exist in this tenant`, badRequestMessage(t, err))

		in.Scopes = []string{"theirs"}
		_, err = keysCreateHandler(deps)(context.Background(), in, principal())
		assert.Equal(t, `scope "theirs" does not exist in this tenant`, badRequestMessage(t, err))

		in.PolicyID = pol.ID.String()
		in.Scopes = []string{"read", "write"}
		_, err = keysCreateHandler(deps)(context.Background(), in, principal())
		assert.Equal(t, "a scope is outside this policy's allowed scopes", badRequestMessage(t, err))

		// A refused create leaves nothing behind.
		total, err := s.Keys().Count(context.Background(), &createTenantFilter)
		require.NoError(t, err)
		assert.Zero(t, total)

		in.Scopes = []string{"read"}
		out, err := keysCreateHandler(deps)(context.Background(), in, principal())
		require.NoError(t, err)
		assert.Equal(t, []string{"read"}, out.Key.Scopes)
	})
}

func TestScopeNameFromError(t *testing.T) {
	name, ok := scopeNameFromError(createPlainErr(`scope "a \"b\"": keysmith: scope not found`))
	assert.True(t, ok)
	assert.Equal(t, `a "b"`, name)
	_, ok = scopeNameFromError(createPlainErr("keysmith: scope not found"))
	assert.False(t, ok)
	_, ok = scopeNameFromError(createPlainErr(`scope unquoted: x`))
	assert.False(t, ok)
}

func TestKeysCreateLifetimeExceeded(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		pol := mkPolicy(t, eng, "t1", &policy.Policy{Name: "short", MaxKeyLifetime: 24 * time.Hour})
		in := validCreate()
		in.PolicyID = pol.ID.String()
		in.ExpiresAt = time.Now().Add(72 * time.Hour).Format(time.RFC3339)
		_, err := keysCreateHandler(deps)(context.Background(), in, principal())
		assert.Equal(t, "expiresAt is beyond this policy's maximum key lifetime", badRequestMessage(t, err))

		in.ExpiresAt = time.Now().Add(12 * time.Hour).Format(time.RFC3339)
		out, err := keysCreateHandler(deps)(context.Background(), in, principal())
		require.NoError(t, err)
		assert.NotEmpty(t, out.Key.ExpiresAt)
	})
}

func TestKeysCreateUnexpectedEngineErrorStaysGeneric(t *testing.T) {
	deps, _ := setup(t, createMemoryStore())
	err := deps.mapCreateError("keys.create", createPlainErr("dial tcp 10.0.0.1: password=hunter2"))
	var ce *dashcontract.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, dashcontract.CodeInternal, ce.Code)
	assert.NotContains(t, ce.Message, "hunter2")
}

func TestKeysCreateThenListAndDetailNeverCarryTheRawKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		in := validCreate()
		in.Scopes = []string{"read"}
		in.Description = "deploys"
		out, err := keysCreateHandler(deps)(context.Background(), in, principal())
		require.NoError(t, err)

		list, err := keysListHandler(deps)(context.Background(), keysListRequest{}, principal())
		require.NoError(t, err)
		require.Len(t, list.Keys, 1)
		assert.Equal(t, out.Key, list.Keys[0])
		assert.Equal(t, []string{"read"}, list.Keys[0].Scopes)

		detail, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: out.Key.ID}, principal())
		require.NoError(t, err)
		assert.Equal(t, out.Key, detail.Key)

		raw := out.RawKey
		for name, v := range map[string]any{"list": list, "detail": detail} {
			b, merr := json.Marshal(v)
			require.NoError(t, merr)
			body := string(b)
			// assert.NotContains would print the body and the key on failure,
			// so only the lengths go in the message.
			assert.False(t, strings.Contains(body, raw), "%s response of %d bytes carries the raw key", name, len(body))
			assert.NotContains(t, body, "rawKey", name)
		}

		// The create response itself is the one place it appears.
		b, err := json.Marshal(out)
		require.NoError(t, err)
		assert.Contains(t, string(b), `"rawKey"`)
	})
}

func TestKeysCreateIsDispatched(t *testing.T) {
	deps, _ := setup(t, createMemoryStore())
	d := createTestDispatcher(t, deps)
	req := dashcontract.Request{
		Envelope: "v1", Kind: dashcontract.KindCommand, Contributor: ContributorName,
		Intent: "keys.create", IntentVersion: 1,
		Payload: json.RawMessage(`{"name":"via dispatch","environment":"test","prefix":"ci"}`),
	}
	data, _, err := d.Dispatch(context.Background(), req, principal())
	require.NoError(t, err)
	var out keyWithSecretResponse
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, "via dispatch", out.Key.Name)
	assert.True(t, strings.HasPrefix(out.RawKey, "ci_test_"))
}

func TestKeysCreateDuplicateScopesAreAnsweredOnce(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "read", "")
		in := validCreate()
		in.Scopes = []string{"read", "read"}
		out, err := keysCreateHandler(deps)(context.Background(), in, principal())
		require.NoError(t, err)
		assert.Equal(t, []string{"read"}, out.Key.Scopes)

		list, err := keysListHandler(deps)(context.Background(), keysListRequest{}, principal())
		require.NoError(t, err)
		require.Len(t, list.Keys, 1)
		assert.Equal(t, out.Key, list.Keys[0])
	})
}

func TestKeysCreateMapsAPolicyDeletedMidFlightLikeAMissingOne(t *testing.T) {
	deps, _ := setup(t, createMemoryStore())
	err := deps.mapCreateError("keys.create", fmt.Errorf("get policy: %w", keysmith.ErrPolicyNotFound))
	assert.Equal(t, "policy not found", badRequestMessage(t, err))
}

// The engine reads the tenant from the context, and it prefers a forge Scope
// over keysmith.WithTenant. A tenant_id claim beats the Scope in tenantFrom,
// so when the two disagree the Scope must not decide where a key lands: the
// contract resolved the tenant, so the key goes there and the contract's own
// reads find it.
func TestKeysCreateUsesTheContractTenantWhateverForgeScopeTheRequestCarries(t *testing.T) {
	scopes := map[string]forge.Scope{
		"org scope": forge.NewOrgScope("app_x", "org_other"),
		"app scope": forge.NewAppScope("app_x"),
	}
	for name, sc := range scopes {
		t.Run(name, func(t *testing.T) {
			storetest.Each(t, func(t *testing.T, s store.Store) {
				deps, eng := setup(t, s)
				mkScope(t, eng, "t1", "read", "")
				p := user(map[string]any{"tenant_id": "t1", "app_id": "app9"})
				ctx := forge.WithScope(context.Background(), sc)

				out, err := keysCreateHandler(deps)(ctx, validCreate(), p)
				require.NoError(t, err)

				stored, err := s.Keys().Get(context.Background(), createMustKeyID(t, out.Key.ID))
				require.NoError(t, err)
				assert.Equal(t, "t1", stored.TenantID)
				assert.Equal(t, "app9", stored.AppID)

				list, err := keysListHandler(deps)(ctx, keysListRequest{}, p)
				require.NoError(t, err)
				require.Len(t, list.Keys, 1)
				assert.Equal(t, out.Key.ID, list.Keys[0].ID)

				detail, err := keysDetailHandler(deps)(ctx, keysDetailRequest{ID: out.Key.ID}, p)
				require.NoError(t, err)
				assert.Equal(t, out.Key.ID, detail.Key.ID)

				// The engine checks scope names in the tenant it resolved, so
				// a scope of the contract's tenant must be accepted.
				in := validCreate()
				in.Scopes = []string{"read"}
				scoped, err := keysCreateHandler(deps)(ctx, in, p)
				require.NoError(t, err)
				assert.Equal(t, []string{"read"}, scoped.Key.Scopes)
			})
		})
	}
}
