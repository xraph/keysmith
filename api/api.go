// Package api provides Forge-style HTTP handlers for the Keysmith API.
package api

import (
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/keysmith"
)

// API wires all Forge-style HTTP handlers together for the keysmith system.
type API struct {
	eng    *keysmith.Engine
	router forge.Router
}

// New creates an API from a Keysmith Engine.
func New(eng *keysmith.Engine, router forge.Router) *API {
	return &API{eng: eng, router: router}
}

// Handler returns the fully assembled http.Handler with all routes.
func (a *API) Handler() http.Handler {
	if a.router == nil {
		a.router = forge.NewRouter()
	}
	a.RegisterRoutes(a.router)
	return a.router.Handler()
}

// RegisterRoutes registers all keysmith API routes into the given Forge router
// with full OpenAPI metadata.
func (a *API) RegisterRoutes(router forge.Router) {
	a.registerKeyRoutes(router)
	a.registerPolicyRoutes(router)
	a.registerScopeRoutes(router)
	a.registerUsageRoutes(router)
	a.registerRotationRoutes(router)
	a.registerValidationRoutes(router)
}

func (a *API) registerKeyRoutes(router forge.Router) {
	g := router.Group("/v1", forge.WithGroupTags("keys"))

	_ = g.POST("/keys", a.createKey,
		forge.WithSummary("Create API key"),
		forge.WithDescription("Creates a new API key. The raw key is returned only once."),
		forge.WithOperationID("createKey"),
		forge.WithRequestSchema(CreateKeyRequest{}),
		forge.WithResponseSchema(http.StatusCreated, "Created key with raw value", &KeyCreateResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.GET("/keys", a.listKeys,
		forge.WithSummary("List API keys"),
		forge.WithDescription("Returns API keys for the current tenant. Raw keys are never returned."),
		forge.WithOperationID("listKeys"),
		forge.WithRequestSchema(ListKeysRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Key list", []*KeyResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.GET("/keys/:keyId", a.getKey,
		forge.WithSummary("Get API key"),
		forge.WithDescription("Returns details of a specific API key."),
		forge.WithOperationID("getKey"),
		forge.WithResponseSchema(http.StatusOK, "Key details", &KeyResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.DELETE("/keys/:keyId", a.deleteKey,
		forge.WithSummary("Delete API key"),
		forge.WithDescription("Permanently deletes an API key."),
		forge.WithOperationID("deleteKey"),
		forge.WithNoContentResponse(),
		forge.WithErrorResponses(),
	)

	_ = g.POST("/keys/:keyId/rotate", a.rotateKey,
		forge.WithSummary("Rotate API key"),
		forge.WithDescription("Rotates an API key, returning the new raw key."),
		forge.WithOperationID("rotateKey"),
		forge.WithRequestSchema(RotateKeyRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Rotated key with new raw value", &KeyCreateResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.POST("/keys/:keyId/revoke", a.revokeKey,
		forge.WithSummary("Revoke API key"),
		forge.WithDescription("Permanently revokes an API key."),
		forge.WithOperationID("revokeKey"),
		forge.WithRequestSchema(RevokeKeyRequest{}),
		forge.WithNoContentResponse(),
		forge.WithErrorResponses(),
	)

	_ = g.POST("/keys/:keyId/suspend", a.suspendKey,
		forge.WithSummary("Suspend API key"),
		forge.WithDescription("Temporarily suspends an API key."),
		forge.WithOperationID("suspendKey"),
		forge.WithNoContentResponse(),
		forge.WithErrorResponses(),
	)

	_ = g.POST("/keys/:keyId/reactivate", a.reactivateKey,
		forge.WithSummary("Reactivate API key"),
		forge.WithDescription("Reactivates a suspended API key."),
		forge.WithOperationID("reactivateKey"),
		forge.WithNoContentResponse(),
		forge.WithErrorResponses(),
	)
}

func (a *API) registerPolicyRoutes(router forge.Router) {
	g := router.Group("/v1", forge.WithGroupTags("policies"))

	_ = g.POST("/policies", a.createPolicy,
		forge.WithSummary("Create policy"),
		forge.WithDescription("Creates a new key policy with rate limits, scopes, and restrictions."),
		forge.WithOperationID("createPolicy"),
		forge.WithRequestSchema(CreatePolicyRequest{}),
		forge.WithResponseSchema(http.StatusCreated, "Created policy", &PolicyResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.GET("/policies", a.listPolicies,
		forge.WithSummary("List policies"),
		forge.WithDescription("Returns key policies for the current tenant."),
		forge.WithOperationID("listPolicies"),
		forge.WithRequestSchema(ListPoliciesRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Policy list", []*PolicyResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.GET("/policies/:policyId", a.getPolicy,
		forge.WithSummary("Get policy"),
		forge.WithDescription("Returns details of a specific key policy."),
		forge.WithOperationID("getPolicy"),
		forge.WithResponseSchema(http.StatusOK, "Policy details", &PolicyResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.PUT("/policies/:policyId", a.updatePolicy,
		forge.WithSummary("Update policy"),
		forge.WithDescription("Updates an existing key policy."),
		forge.WithOperationID("updatePolicy"),
		forge.WithRequestSchema(UpdatePolicyRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Updated policy", &PolicyResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.DELETE("/policies/:policyId", a.deletePolicy,
		forge.WithSummary("Delete policy"),
		forge.WithDescription("Deletes a key policy. Fails if keys are assigned to it."),
		forge.WithOperationID("deletePolicy"),
		forge.WithNoContentResponse(),
		forge.WithErrorResponses(),
	)
}

func (a *API) registerScopeRoutes(router forge.Router) {
	g := router.Group("/v1", forge.WithGroupTags("scopes"))

	_ = g.POST("/scopes", a.createScope,
		forge.WithSummary("Create scope"),
		forge.WithDescription("Creates a new permission scope."),
		forge.WithOperationID("createScope"),
		forge.WithRequestSchema(CreateScopeRequest{}),
		forge.WithResponseSchema(http.StatusCreated, "Created scope", &ScopeResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.GET("/scopes", a.listScopes,
		forge.WithSummary("List scopes"),
		forge.WithDescription("Returns permission scopes for the current tenant."),
		forge.WithOperationID("listScopes"),
		forge.WithRequestSchema(ListScopesRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Scope list", []*ScopeResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.DELETE("/scopes/:scopeId", a.deleteScope,
		forge.WithSummary("Delete scope"),
		forge.WithDescription("Deletes a permission scope."),
		forge.WithOperationID("deleteScope"),
		forge.WithNoContentResponse(),
		forge.WithErrorResponses(),
	)

	_ = g.POST("/keys/:keyId/scopes", a.assignScopes,
		forge.WithSummary("Assign scopes to key"),
		forge.WithDescription("Assigns permission scopes to an API key."),
		forge.WithOperationID("assignScopes"),
		forge.WithRequestSchema(AssignScopesRequest{}),
		forge.WithNoContentResponse(),
		forge.WithErrorResponses(),
	)

	_ = g.DELETE("/keys/:keyId/scopes", a.removeScopes,
		forge.WithSummary("Remove scopes from key"),
		forge.WithDescription("Removes permission scopes from an API key."),
		forge.WithOperationID("removeScopes"),
		forge.WithRequestSchema(RemoveScopesRequest{}),
		forge.WithNoContentResponse(),
		forge.WithErrorResponses(),
	)
}

func (a *API) registerUsageRoutes(router forge.Router) {
	g := router.Group("/v1", forge.WithGroupTags("usage"))

	_ = g.GET("/keys/:keyId/usage", a.getKeyUsage,
		forge.WithSummary("Get key usage"),
		forge.WithDescription("Returns usage records for a specific key."),
		forge.WithOperationID("getKeyUsage"),
		forge.WithRequestSchema(GetKeyUsageRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Usage records", []*UsageResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.GET("/keys/:keyId/usage/aggregate", a.getKeyUsageAggregate,
		forge.WithSummary("Get key usage aggregate"),
		forge.WithDescription("Returns aggregated usage statistics for a key."),
		forge.WithOperationID("getKeyUsageAggregate"),
		forge.WithRequestSchema(GetKeyUsageAggregateRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Aggregated usage", []*AggregationResponse{}),
		forge.WithErrorResponses(),
	)

	_ = g.GET("/usage", a.listUsage,
		forge.WithSummary("List usage across all keys"),
		forge.WithDescription("Returns aggregated usage for the tenant."),
		forge.WithOperationID("listUsage"),
		forge.WithRequestSchema(ListUsageRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Tenant usage", []*AggregationResponse{}),
		forge.WithErrorResponses(),
	)
}

func (a *API) registerRotationRoutes(router forge.Router) {
	g := router.Group("/v1", forge.WithGroupTags("rotations"))

	_ = g.GET("/keys/:keyId/rotations", a.listRotations,
		forge.WithSummary("List key rotations"),
		forge.WithDescription("Returns rotation history for a specific key."),
		forge.WithOperationID("listKeyRotations"),
		forge.WithRequestSchema(ListRotationsRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Rotation history", []*RotationResponse{}),
		forge.WithErrorResponses(),
	)
}

func (a *API) registerValidationRoutes(router forge.Router) {
	g := router.Group("/v1", forge.WithGroupTags("validation"))

	_ = g.POST("/keys/validate", a.validateKey,
		forge.WithSummary("Validate API key"),
		forge.WithDescription("Validates a raw API key and returns its metadata if valid."),
		forge.WithOperationID("validateKey"),
		forge.WithRequestSchema(ValidateKeyRequest{}),
		forge.WithResponseSchema(http.StatusOK, "Validation result", &ValidationResponse{}),
		forge.WithErrorResponses(),
	)
}
