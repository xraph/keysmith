package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

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
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/store"
	"github.com/xraph/keysmith/store/memory"
)

func swCreate(deps Deps, in scopesCreateRequest) (scopesCreateResponse, error) {
	return scopesCreateHandler(deps)(context.Background(), in, principal())
}

func swDelete(deps Deps, scopeID string) (scopesDeleteResponse, error) {
	return scopesDeleteHandler(deps)(context.Background(), scopesDeleteRequest{ID: scopeID}, principal())
}

func swScopeID(t *testing.T, s string) id.ScopeID {
	t.Helper()
	sid, err := id.ParseScopeID(s)
	require.NoError(t, err)
	return sid
}

func swConflict(msg string) error {
	return &dashcontract.Error{Code: dashcontract.CodeConflict, Message: msg}
}

func swNames(t *testing.T, s store.Store, tenant string) []string {
	t.Helper()
	rows, err := s.Scopes().List(context.Background(), &scope.ListFilter{TenantID: tenant})
	require.NoError(t, err)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

// Every rule answers BAD_REQUEST with a fixed message, first failure wins.
// The fixture copies these, so they are pinned word for word.
func TestScopesCreateValidationMessages(t *testing.T) {
	s := memory.New()
	deps, eng := setup(t, s)
	mkScope(t, eng, "t1", "billing", "")
	mkScope(t, eng, "t2", "theirs", "")
	longDesc := strings.Repeat("é", 1001)

	cases := []struct {
		name string
		in   scopesCreateRequest
		want string
	}{
		{"no name", scopesCreateRequest{}, "name is required"},
		{"blank name", scopesCreateRequest{Name: " \t "}, "name is required"},
		{"name over 100 runes", scopesCreateRequest{Name: strings.Repeat("é", 101)}, "name is too long"},
		{"name with a space", scopesCreateRequest{Name: "billing read"}, "name cannot contain spaces"},
		{"name with a tab", scopesCreateRequest{Name: "billing\tread"}, "name cannot contain spaces"},
		{"name with a no-break space", scopesCreateRequest{Name: "billing read"}, "name cannot contain spaces"},
		{"description over 1000 runes", scopesCreateRequest{Name: "read", Description: longDesc}, "description is too long"},
		{"parent over 100 runes", scopesCreateRequest{Name: "read", Parent: strings.Repeat("é", 101)}, "parent is too long"},
		{"own parent", scopesCreateRequest{Name: "read", Parent: " read "}, "a scope cannot be its own parent"},
		{"missing parent", scopesCreateRequest{Name: "read", Parent: "nope"}, `parent scope "nope" does not exist in this tenant`},
		{"another tenant's parent", scopesCreateRequest{Name: "read", Parent: " theirs "}, `parent scope "theirs" does not exist in this tenant`},

		// Order: the first failing rule answers.
		{"no name beats a long description", scopesCreateRequest{Description: longDesc}, "name is required"},
		{"too long beats spaces", scopesCreateRequest{Name: strings.Repeat("a b", 40)}, "name is too long"},
		{"spaces beat a long description", scopesCreateRequest{Name: "a b", Description: longDesc}, "name cannot contain spaces"},
		{"a long description beats a bad parent", scopesCreateRequest{Name: "read", Parent: "nope", Description: longDesc}, "description is too long"},
		{"own parent beats a missing parent", scopesCreateRequest{Name: "nope", Parent: "nope"}, "a scope cannot be its own parent"},
		{"a long description beats a long parent", scopesCreateRequest{Name: "read", Parent: strings.Repeat("p", 101), Description: longDesc}, "description is too long"},
		{"a long parent beats a missing parent", scopesCreateRequest{Name: "read", Parent: strings.Repeat("p", 101)}, "parent is too long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := swCreate(deps, tc.in)
			assert.Equal(t, tc.want, badRequestMessage(t, err))
		})
	}
	assert.ElementsMatch(t, []string{"billing"}, swNames(t, s, "t1"), "no refused create stored anything")

	// The limits themselves are allowed.
	_, err := swCreate(deps, scopesCreateRequest{
		Name: strings.Repeat("é", 100), Parent: "billing", Description: strings.Repeat("é", 1000),
	})
	require.NoError(t, err)
}

func TestScopesCreateTrimsAndStores(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "billing", "")

		out, err := swCreate(deps, scopesCreateRequest{Name: " billing:read ", Parent: " billing ", Description: "  Read invoices "})
		require.NoError(t, err)
		assert.Equal(t, "billing:read", out.Scope.Name)
		assert.Equal(t, "billing", out.Scope.Parent)
		assert.Equal(t, "Read invoices", out.Scope.Description)

		stored, err := s.Scopes().Get(context.Background(), swScopeID(t, out.Scope.ID))
		require.NoError(t, err)
		assert.Equal(t, "t1", stored.TenantID)
		assert.Equal(t, "billing:read", stored.Name)
		assert.Equal(t, "billing", stored.Parent)
		assert.Equal(t, "Read invoices", stored.Description)

		// A blank parent is no parent, and the response leaves both empty
		// fields out exactly as scopes.list does.
		bare, err := swCreate(deps, scopesCreateRequest{Name: "audit", Parent: "  "})
		require.NoError(t, err)
		assert.Empty(t, bare.Scope.Parent)
		list, err := scopesListHandler(deps)(context.Background(), pickerRequest{}, principal())
		require.NoError(t, err)
		assert.Contains(t, list.Scopes, out.Scope)
		assert.Contains(t, list.Scopes, bare.Scope)
	})
}

// The engine reads the tenant from the context and prefers a forge Scope over
// keysmith.WithTenant. When a tenant_id claim names another tenant than the
// Scope's org, the claim wins in tenantFrom, and the Scope must not decide
// where a scope lands.
func TestScopesCreateStoresUnderTheContractTenant(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		mkScope(t, eng, "t1", "billing", "")
		p := user(map[string]any{"tenant_id": "t1", "app_id": "app9"})
		ctx := forge.WithScope(context.Background(), forge.NewOrgScope("app_x", "org_other"))

		out, err := scopesCreateHandler(deps)(ctx, scopesCreateRequest{Name: "billing:read", Parent: "billing"}, p)
		require.NoError(t, err)

		stored, err := s.Scopes().Get(context.Background(), swScopeID(t, out.Scope.ID))
		require.NoError(t, err)
		assert.Equal(t, "t1", stored.TenantID)
		assert.Equal(t, "app9", stored.AppID)
		assert.Empty(t, swNames(t, s, "org_other"))

		list, err := scopesListHandler(deps)(ctx, pickerRequest{}, p)
		require.NoError(t, err)
		assert.Contains(t, list.Scopes, out.Scope)
	})
}

func TestScopesCreateRefusesADuplicateName(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		// Another tenant's scope of the same name does not count.
		mkScope(t, eng, "t2", "read", "")
		_, err := swCreate(deps, scopesCreateRequest{Name: "read"})
		require.NoError(t, err)

		_, err = swCreate(deps, scopesCreateRequest{Name: " read ", Description: "again"})
		assert.Equal(t, swConflict("a scope with this name already exists"), err)
		assert.Equal(t, []string{"read"}, swNames(t, s, "t1"))
	})
}

func TestScopesDeleteTakesTheScopeOffEveryKey(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		read := mkScope(t, eng, "t1", "read", "")
		mkScope(t, eng, "t1", "write", "")
		k := create(t, eng, "t1", &keysmith.CreateKeyInput{
			Name: "k", Prefix: "sk", Environment: key.EnvLive, Scopes: []string{"read", "write"},
		})

		out, err := swDelete(deps, " "+read.ID.String()+" ")
		require.NoError(t, err)
		assert.Equal(t, scopesDeleteResponse{ID: read.ID.String()}, out)

		detail, err := keysDetailHandler(deps)(context.Background(), keysDetailRequest{ID: k.Key.ID.String()}, principal())
		require.NoError(t, err)
		assert.Equal(t, []string{"write"}, detail.Key.Scopes)
		list, err := keysListHandler(deps)(context.Background(), keysListRequest{}, principal())
		require.NoError(t, err)
		require.Len(t, list.Keys, 1)
		assert.Equal(t, []string{"write"}, list.Keys[0].Scopes)
		assert.Equal(t, []string{"write"}, swNames(t, s, "t1"))

		_, err = swDelete(deps, read.ID.String())
		assert.Equal(t, &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "scope not found"}, err,
			"a second delete finds nothing")
	})
}

func TestScopesDeleteRefusesWhileChildrenNameIt(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		parent := mkScope(t, eng, "t1", "billing", "")
		r := mkScope(t, eng, "t1", "billing:read", "billing")
		w := mkScope(t, eng, "t1", "billing:write", "billing")
		// Another tenant's child of a same-name parent does not count.
		mkScope(t, eng, "t2", "x", "billing")
		// Children are checked before policies.
		mkPolicy(t, eng, "t1", &policy.Policy{Name: "p", AllowedScopes: []string{"billing"}})

		_, err := swDelete(deps, parent.ID.String())
		assert.Equal(t, swConflict("2 scopes name this scope as their parent"), err)
		_, err = swDelete(deps, w.ID.String())
		require.NoError(t, err)
		_, err = swDelete(deps, parent.ID.String())
		assert.Equal(t, swConflict("1 scope names this scope as its parent"), err)
		_, err = swDelete(deps, r.ID.String())
		require.NoError(t, err)
		_, err = swDelete(deps, parent.ID.String())
		assert.Equal(t, swConflict("1 policy allows this scope"), err, "with the children gone, the policy blocks")
		_, err = s.Scopes().Get(context.Background(), parent.ID)
		require.NoError(t, err, "a refused delete leaves the scope")
	})
}

func TestScopesDeleteCountsAtMost200Children(t *testing.T) {
	s := memory.New()
	deps, eng := setup(t, s)
	parent := mkScope(t, eng, "t1", "billing", "")
	for i := range 200 {
		mkScope(t, eng, "t1", "c"+strconv.Itoa(i), "billing")
	}
	_, err := swDelete(deps, parent.ID.String())
	assert.Equal(t, swConflict("200 scopes name this scope as their parent"), err)

	mkScope(t, eng, "t1", "c200", "billing")
	_, err = swDelete(deps, parent.ID.String())
	assert.Equal(t, swConflict("more than 200 scopes name this scope as their parent"), err)
}

func TestScopesDeleteRefusesWhileAPolicyAllowsIt(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		read := mkScope(t, eng, "t1", "read", "")
		mkScope(t, eng, "t1", "write", "")
		a := mkPolicy(t, eng, "t1", &policy.Policy{Name: "a", AllowedScopes: []string{"read", "write"}})
		mkPolicy(t, eng, "t1", &policy.Policy{Name: "open"})
		// Another tenant's policy naming the same scope does not count.
		mkPolicy(t, eng, "t2", &policy.Policy{Name: "theirs", AllowedScopes: []string{"read"}})

		_, err := swDelete(deps, read.ID.String())
		assert.Equal(t, swConflict("1 policy allows this scope"), err)

		b := mkPolicy(t, eng, "t1", &policy.Policy{Name: "b", AllowedScopes: []string{"read"}})
		_, err = swDelete(deps, read.ID.String())
		assert.Equal(t, swConflict("2 policies allow this scope"), err)

		for _, p := range []*policy.Policy{a, b} {
			_, err = pwUpdate(deps, p.ID.String(), policyFields{AllowedScopes: pwList("write")})
			require.NoError(t, err)
		}
		_, err = swDelete(deps, read.ID.String())
		require.NoError(t, err)
	})
}

// swStore swaps in failing scope or policy stores.
type swStore struct {
	store.Store
	scopes   scope.Store
	policies policy.Store
}

func (w swStore) Scopes() scope.Store {
	if w.scopes != nil {
		return w.scopes
	}
	return w.Store.Scopes()
}

func (w swStore) Policies() policy.Store {
	if w.policies != nil {
		return w.policies
	}
	return w.Store.Policies()
}

const swSecret = "dial tcp 10.0.0.1: secret-connection-detail"

// swFlakyScopes fails List from its failFrom-th call on and, when failGet
// is set, every Get.
type swFlakyScopes struct {
	scope.Store
	calls    int
	failFrom int
	failGet  bool
}

func (w *swFlakyScopes) List(ctx context.Context, f *scope.ListFilter) ([]*scope.Scope, error) {
	w.calls++
	if w.failFrom > 0 && w.calls >= w.failFrom {
		return nil, createPlainErr(swSecret)
	}
	return w.Store.List(ctx, f)
}

func (w *swFlakyScopes) Get(ctx context.Context, sid id.ScopeID) (*scope.Scope, error) {
	if w.failGet {
		return nil, createPlainErr(swSecret)
	}
	return w.Store.Get(ctx, sid)
}

// swFlakyPolicies fails List from its failFrom-th call on.
type swFlakyPolicies struct {
	policy.Store
	calls    int
	failFrom int
}

func (w *swFlakyPolicies) List(ctx context.Context, f *policy.ListFilter) ([]*policy.Policy, error) {
	w.calls++
	if w.calls >= w.failFrom {
		return nil, createPlainErr(swSecret)
	}
	return w.Store.List(ctx, f)
}

// When the engine refuses and the count read then fails, the answer is still
// the CONFLICT, without a number, and the failure is logged.
func TestScopesDeleteRefusesWithoutACountWhenTheCountFails(t *testing.T) {
	s := memory.New()
	_, eng := setup(t, s)
	parent := mkScope(t, eng, "t1", "billing", "")
	mkScope(t, eng, "t1", "billing:read", "billing")
	read := mkScope(t, eng, "t1", "read", "")
	mkPolicy(t, eng, "t1", &policy.Policy{Name: "p", AllowedScopes: []string{"read"}})

	scopes := &swFlakyScopes{Store: s.Scopes(), failFrom: 2}
	policies := &swFlakyPolicies{Store: s.Policies(), failFrom: 2}
	flaky, err := keysmith.NewEngine(keysmith.WithStore(swStore{Store: s, scopes: scopes, policies: policies}))
	require.NoError(t, err)
	var lines []rotateLogLine
	deps := Deps{Engine: flaky, DefaultTenantID: "t1", Logger: rotateCapturingLogger{lines: &lines}}

	_, err = swDelete(deps, parent.ID.String())
	assert.Equal(t, swConflict("scopes name this scope as their parent"), err)
	assert.Equal(t, 2, scopes.calls, "the engine's check, then the count")

	scopes.failFrom = 0 // the next delete's children check must pass
	_, err = swDelete(deps, read.ID.String())
	assert.Equal(t, swConflict("policies allow this scope"), err)
	assert.Equal(t, 2, policies.calls, "the engine's check, then the count")

	require.Len(t, lines, 2)
	for _, l := range lines {
		assert.Equal(t, "scopes.delete", l.fields["intent"])
		assert.Contains(t, l.fields["error"], "secret-connection-detail")
	}

	// The engine's own read failing is INTERNAL and says nothing more.
	policies.calls, policies.failFrom = 0, 1
	_, err = swDelete(deps, read.ID.String())
	assert.Equal(t, &dashcontract.Error{Code: dashcontract.CodeInternal, Message: "an internal error occurred"}, err)
	assert.ElementsMatch(t, []string{"billing", "billing:read", "read"}, swNames(t, s, "t1"),
		"no failed delete removed a scope")
}

// A store failure on the reads before a write is INTERNAL, logged against
// the intent, and never echoes the store's text.
func TestScopesWritesAnswerInternalWhenAReadFails(t *testing.T) {
	s := memory.New()
	_, eng := setup(t, s)
	read := mkScope(t, eng, "t1", "read", "")
	scopes := &swFlakyScopes{Store: s.Scopes(), failGet: true}
	flaky, err := keysmith.NewEngine(keysmith.WithStore(swStore{Store: s, scopes: scopes}))
	require.NoError(t, err)
	var lines []rotateLogLine
	deps := Deps{Engine: flaky, DefaultTenantID: "t1", Logger: rotateCapturingLogger{lines: &lines}}
	internal := &dashcontract.Error{Code: dashcontract.CodeInternal, Message: "an internal error occurred"}

	_, err = swDelete(deps, read.ID.String())
	assert.Equal(t, internal, err)
	require.Len(t, lines, 1)
	assert.Equal(t, "scopes.delete", lines[0].fields["intent"])

	// The parent lookup on create goes through GetByName.
	failing := &swFailingGetByName{Store: s.Scopes()}
	flaky, err = keysmith.NewEngine(keysmith.WithStore(swStore{Store: s, scopes: failing}))
	require.NoError(t, err)
	deps.Engine = flaky
	_, err = swCreate(deps, scopesCreateRequest{Name: "read:users", Parent: "read"})
	assert.Equal(t, internal, err)
	require.Len(t, lines, 2)
	assert.Equal(t, "scopes.create", lines[1].fields["intent"])
	assert.Equal(t, []string{"read"}, swNames(t, s, "t1"))
}

type swFailingGetByName struct{ scope.Store }

func (swFailingGetByName) GetByName(context.Context, string, string) (*scope.Scope, error) {
	return nil, createPlainErr(swSecret)
}

func TestScopesDeleteIsTenantScoped(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		deps, eng := setup(t, s)
		theirs := mkScope(t, eng, "t2", "theirs", "")

		notFound := &dashcontract.Error{Code: dashcontract.CodeNotFound, Message: "scope not found"}
		_, foreign := swDelete(deps, theirs.ID.String())
		_, missing := swDelete(deps, id.NewScopeID().String())
		assert.Equal(t, notFound, foreign)
		assert.Equal(t, notFound, missing)
		assert.Equal(t, []string{"theirs"}, swNames(t, s, "t2"), "another tenant's scope is still there")

		_, err := swDelete(deps, "  ")
		assert.Equal(t, "id is required", badRequestMessage(t, err))
		_, err = swDelete(deps, id.NewKeyID().String())
		assert.Equal(t, "id is not a scope id", badRequestMessage(t, err))
	})
}

func TestScopesWritesRefuseWithoutAUserOrTenant(t *testing.T) {
	s := memory.New()
	deps, eng := setup(t, s)
	read := mkScope(t, eng, "t1", "read", "")
	ctx := context.Background()

	_, err := scopesCreateHandler(deps)(ctx, scopesCreateRequest{Name: "write"}, dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))
	_, err = scopesDeleteHandler(deps)(ctx, scopesDeleteRequest{ID: read.ID.String()}, dashcontract.Principal{})
	assert.Equal(t, dashcontract.CodeUnauthenticated, codeOf(t, err))

	deps.DefaultTenantID = ""
	_, err = scopesCreateHandler(deps)(ctx, scopesCreateRequest{Name: "write"}, principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))
	_, err = scopesDeleteHandler(deps)(ctx, scopesDeleteRequest{ID: read.ID.String()}, principal())
	assert.Equal(t, dashcontract.CodePermissionDenied, codeOf(t, err))

	assert.Equal(t, []string{"read"}, swNames(t, s, "t1"), "a refused write changed nothing")
}

func TestScopesWritesAreDispatchedAsWriteCommands(t *testing.T) {
	deps, _ := setup(t, memory.New())
	d := createTestDispatcher(t, deps)
	dispatch := func(intent, payload string) json.RawMessage {
		t.Helper()
		data, _, err := d.Dispatch(context.Background(), dashcontract.Request{
			Envelope: "v1", Kind: dashcontract.KindCommand, Contributor: ContributorName,
			Intent: intent, IntentVersion: 1, Payload: json.RawMessage(payload),
		}, principal())
		require.NoError(t, err)
		return data
	}

	var created struct {
		Scope map[string]any `json:"scope"`
	}
	require.NoError(t, json.Unmarshal(dispatch("scopes.create", `{"name":"read","parent":"","description":"Read things"}`), &created))
	scopeID, _ := created.Scope["id"].(string)
	assert.Equal(t, map[string]any{"id": scopeID, "name": "read", "description": "Read things"}, created.Scope)

	assert.JSONEq(t, `{"id":"`+scopeID+`"}`, string(dispatch("scopes.delete", `{"id":"`+scopeID+`"}`)))

	m, err := loader.Load(bytes.NewReader(manifestYAML), "keysmith/contract/manifest.yaml")
	require.NoError(t, err)
	want := map[string][]string{
		"scopes.create": {"scopes.list", "overview"},
		"scopes.delete": {"scopes.list", "keys.list", "keys.detail", "overview"},
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
