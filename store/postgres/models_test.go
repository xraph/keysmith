package postgres

import (
	"testing"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/scope"
	"github.com/xraph/keysmith/usage"
)

// The keysmith_* tables declare their jsonb columns as NOT NULL DEFAULT '{}'
// (or '[]'). grove always lists every column in the INSERT, so the DEFAULT
// never applies — a nil Go map/slice is sent as an explicit SQL NULL and trips
// the not-null constraint. The *ToModel functions must coalesce nil collections
// to empty (but non-nil) values so pgx serializes '{}' / '[]' instead of NULL.

func TestKeyToModel_nilMetadataBecomesEmpty(t *testing.T) {
	m := keyToModel(&key.Key{ID: id.NewKeyID(), Metadata: nil})
	if m.Metadata == nil {
		t.Fatal("keyToModel: Metadata is nil; want non-nil empty map (would insert SQL NULL)")
	}
}

func TestPolicyToModel_nilCollectionsBecomeEmpty(t *testing.T) {
	m := policyToModel(&policy.Policy{ID: id.NewPolicyID()})
	if m.Metadata == nil {
		t.Error("policyToModel: Metadata is nil; want non-nil empty map")
	}
	if m.AllowedScopes == nil {
		t.Error("policyToModel: AllowedScopes is nil; want non-nil empty slice")
	}
	if m.AllowedIPs == nil {
		t.Error("policyToModel: AllowedIPs is nil; want non-nil empty slice")
	}
	if m.AllowedOrigins == nil {
		t.Error("policyToModel: AllowedOrigins is nil; want non-nil empty slice")
	}
	if m.AllowedMethods == nil {
		t.Error("policyToModel: AllowedMethods is nil; want non-nil empty slice")
	}
	if m.AllowedPaths == nil {
		t.Error("policyToModel: AllowedPaths is nil; want non-nil empty slice")
	}
}

func TestScopeToModel_nilMetadataBecomesEmpty(t *testing.T) {
	m := scopeToModel(&scope.Scope{ID: id.NewScopeID(), Metadata: nil})
	if m.Metadata == nil {
		t.Fatal("scopeToModel: Metadata is nil; want non-nil empty map")
	}
}

func TestUsageToModel_nilMetadataBecomesEmpty(t *testing.T) {
	m := usageToModel(&usage.Record{ID: id.NewUsageID(), KeyID: id.NewKeyID(), Metadata: nil})
	if m.Metadata == nil {
		t.Fatal("usageToModel: Metadata is nil; want non-nil empty map")
	}
}
