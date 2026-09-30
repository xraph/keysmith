package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"

	"github.com/xraph/keysmith/store/memory"
)

// TestRegister_NilEngineErrors checks the guard at the top of Register: a nil
// Engine must fail loudly here rather than panic the first time a handler runs.
func TestRegister_NilEngineErrors(t *testing.T) {
	d := dispatcher.New(nil)
	err := Register(d, dashcontract.NewRegistry(), dashcontract.NewWardenRegistry(), Deps{})
	if err == nil {
		t.Fatal("Register with a nil Engine: want an error, got nil")
	}
}

// TestEveryDeclaredIntentIsRegistered dispatches a request for every intent
// the embedded manifest declares and asserts the dispatcher knows it. A
// handler error is fine; the dispatcher not knowing the intent is what a
// manifest entry with no matching Register* call looks like.
func TestEveryDeclaredIntentIsRegistered(t *testing.T) {
	deps, _ := setup(t, memory.New())

	d := dispatcher.New(nil)
	if err := Register(d, dashcontract.NewRegistry(), dashcontract.NewWardenRegistry(), deps); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m, err := loader.Load(bytes.NewReader(manifestYAML), "keysmith/contract/manifest.yaml")
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(m.Intents) == 0 {
		t.Fatal("manifest declares no intents")
	}

	for _, intent := range m.Intents {
		kind := dashcontract.KindQuery
		if intent.Kind == dashcontract.IntentKindCommand {
			kind = dashcontract.KindCommand
		}
		req := dashcontract.Request{
			Envelope:      "v1",
			Kind:          kind,
			Contributor:   ContributorName,
			Intent:        intent.Name,
			IntentVersion: 1,
		}
		if kind == dashcontract.KindQuery {
			req.Params = map[string]any{}
		} else {
			req.Payload = json.RawMessage(`{}`)
		}

		_, _, dispatchErr := d.Dispatch(context.Background(), req, dashcontract.Principal{})
		if dispatchErr != nil && strings.Contains(strings.ToLower(dispatchErr.Error()), "not registered") {
			t.Errorf("%s is not registered: %v", intent.Name, dispatchErr)
		}
	}
}
