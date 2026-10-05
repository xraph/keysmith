package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/store/memory"
)

// statusError is the shape forge's router looks for when it turns a handler
// error into a response. An error without it is answered as a 500.
type statusError interface {
	error
	StatusCode() int
}

func TestMapStoreErrorStatuses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"policy in use", keysmith.ErrPolicyInUse, http.StatusConflict},
		{"policy name taken", keysmith.ErrPolicyNameTaken, http.StatusConflict},
		{"scope name taken", keysmith.ErrScopeNameTaken, http.StatusConflict},
		{"scope has children", keysmith.ErrScopeHasChildren, http.StatusConflict},
		{"scope allowed by policy", keysmith.ErrScopeAllowedByPolicy, http.StatusConflict},
		{"invalid state transition", keysmith.ErrInvalidStateTransition, http.StatusConflict},
		{"key not found", keysmith.ErrKeyNotFound, http.StatusNotFound},
		{"policy not found", keysmith.ErrPolicyNotFound, http.StatusNotFound},
		{"scope not found", keysmith.ErrScopeNotFound, http.StatusNotFound},
		{"rotation not found", keysmith.ErrRotationNotFound, http.StatusNotFound},
	}

	for _, tc := range cases {
		for _, wrapped := range []bool{false, true} {
			err := tc.err
			label := tc.name + "/bare"
			if wrapped {
				err = fmt.Errorf("engine step: %w", tc.err)
				label = tc.name + "/wrapped"
			}
			t.Run(label, func(t *testing.T) {
				got := mapStoreError(err)
				var se statusError
				if !errors.As(got, &se) {
					t.Fatalf("mapStoreError(%v) = %v, want an HTTP error with status %d", err, got, tc.want)
				}
				if se.StatusCode() != tc.want {
					t.Fatalf("status = %d, want %d", se.StatusCode(), tc.want)
				}
				if !strings.Contains(se.Error(), tc.err.Error()) {
					t.Fatalf("message %q does not carry %q", se.Error(), tc.err.Error())
				}
			})
		}
	}
}

func TestMapStoreErrorLeavesUnknownErrorsAlone(t *testing.T) {
	if mapStoreError(nil) != nil {
		t.Fatal("mapStoreError(nil) is not nil")
	}

	unknown := errors.New("pq: connection reset while talking to 10.0.0.7")
	got := mapStoreError(unknown)
	if !errors.Is(got, unknown) {
		t.Fatalf("mapStoreError(unknown) = %v, want the same error back", got)
	}
	var se statusError
	if errors.As(got, &se) {
		t.Fatalf("an unknown error became HTTP %d; it should stay a plain 500", se.StatusCode())
	}

	// Through forge's router the plain error is a 500 that keeps its text to
	// itself.
	router := forge.NewRouter()
	_ = router.GET("/boom", func(forge.Context) error {
		return mapStoreError(unknown)
	})
	rec := send(t, router.Handler(), http.MethodGet, "/boom", nil, http.StatusInternalServerError, "unknown error")
	if strings.Contains(rec.Body.String(), "10.0.0.7") {
		t.Fatalf("500 body echoes the error text: %s", rec.Body.String())
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	eng, err := keysmith.NewEngine(keysmith.WithStore(memory.New()))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return New(eng, nil).Handler()
}

// send runs one request and fails the test unless it answers want.
func send(t *testing.T, h http.Handler, method, path string, body any, want int, what string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("%s: status %d, want %d, body %s", what, rec.Code, want, rec.Body.String())
	}
	return rec
}

// policyBody and scopeBody fill every field with a value: forge's binder
// treats a field without an optional tag as required and an empty string as
// missing.
func policyBody(name string, allowed ...string) map[string]any {
	return map[string]any{
		"name": name, "description": "d", "rate_limit_window": "1m", "max_key_lifetime": "90d",
		"rotation_period": "30d", "grace_period": "1h", "allowed_scopes": allowed,
	}
}

func scopeBody(name, parent string) map[string]any {
	return map[string]any{"name": name, "description": "d", "parent": parent}
}

func createdID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		ID string `json:"id"`
	}
	// Decode only the first value: the create handlers write the JSON body
	// themselves and also return it, so forge writes it a second time.
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil || out.ID == "" {
		t.Fatalf("no id in %s (%v)", rec.Body.String(), err)
	}
	return out.ID
}

func TestCreateAnswersConflictForTakenNames(t *testing.T) {
	h := newTestHandler(t)

	send(t, h, http.MethodPost, "/v1/policies", policyBody("standard"), http.StatusCreated, "first policy")
	send(t, h, http.MethodPost, "/v1/policies", policyBody("standard"), http.StatusConflict, "duplicate policy")

	send(t, h, http.MethodPost, "/v1/scopes", scopeBody("read", "root"), http.StatusCreated, "first scope")
	send(t, h, http.MethodPost, "/v1/scopes", scopeBody("read", "root"), http.StatusConflict, "duplicate scope")
}

func TestDeleteScopeAnswersConflictWhileInUse(t *testing.T) {
	h := newTestHandler(t)

	parentID := createdID(t, send(t, h, http.MethodPost, "/v1/scopes", scopeBody("read", "root"), http.StatusCreated, "parent scope"))
	send(t, h, http.MethodPost, "/v1/scopes", scopeBody("read:users", "read"), http.StatusCreated, "child scope")
	send(t, h, http.MethodDelete, "/v1/scopes/"+parentID, nil, http.StatusConflict, "delete parent")

	allowedID := createdID(t, send(t, h, http.MethodPost, "/v1/scopes", scopeBody("write", "root"), http.StatusCreated, "allowed scope"))
	send(t, h, http.MethodPost, "/v1/policies", policyBody("writers", "write"), http.StatusCreated, "policy")
	send(t, h, http.MethodDelete, "/v1/scopes/"+allowedID, nil, http.StatusConflict, "delete allowed scope")
}
