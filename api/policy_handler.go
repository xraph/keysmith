package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/policy"
)

func (a *API) createPolicy(ctx forge.Context, req *CreatePolicyRequest) (*PolicyResponse, error) {
	pol := &policy.Policy{
		ID:              id.NewPolicyID(),
		Name:            req.Name,
		Description:     req.Description,
		RateLimit:       req.RateLimit,
		RateLimitWindow: parseDuration(req.RateLimitWindow),
		BurstLimit:      req.BurstLimit,
		AllowedScopes:   req.AllowedScopes,
		AllowedIPs:      req.AllowedIPs,
		AllowedOrigins:  req.AllowedOrigins,
		MaxKeyLifetime:  parseDuration(req.MaxKeyLifetime),
		RotationPeriod:  parseDuration(req.RotationPeriod),
		GracePeriod:     parseDuration(req.GracePeriod),
		DailyQuota:      req.DailyQuota,
		MonthlyQuota:    req.MonthlyQuota,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := a.eng.CreatePolicy(ctx.Context(), pol); err != nil {
		return nil, fmt.Errorf("create policy: %w", err)
	}

	resp := toPolicyResponse(pol)
	return resp, ctx.JSON(http.StatusCreated, resp)
}

func (a *API) getPolicy(ctx forge.Context, _ *ListPoliciesRequest) (*PolicyResponse, error) {
	polID, err := id.ParsePolicyID(ctx.Param("policyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid policy ID: %v", err))
	}

	pol, err := a.eng.GetPolicy(ctx.Context(), polID)
	if err != nil {
		return nil, mapStoreError(err)
	}

	resp := toPolicyResponse(pol)
	return resp, ctx.JSON(http.StatusOK, resp)
}

func (a *API) listPolicies(ctx forge.Context, req *ListPoliciesRequest) ([]*PolicyResponse, error) {
	policies, err := a.eng.ListPolicies(ctx.Context(), &policy.ListFilter{
		Limit:  defaultLimit(req.Limit),
		Offset: req.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}

	resp := make([]*PolicyResponse, len(policies))
	for i, p := range policies {
		resp[i] = toPolicyResponse(p)
	}
	return resp, ctx.JSON(http.StatusOK, resp)
}

func (a *API) updatePolicy(ctx forge.Context, req *UpdatePolicyRequest) (*PolicyResponse, error) {
	polID, err := id.ParsePolicyID(ctx.Param("policyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid policy ID: %v", err))
	}

	pol, err := a.eng.GetPolicy(ctx.Context(), polID)
	if err != nil {
		return nil, mapStoreError(err)
	}

	pol.Name = req.Name
	pol.Description = req.Description
	pol.RateLimit = req.RateLimit
	pol.RateLimitWindow = parseDuration(req.RateLimitWindow)
	pol.BurstLimit = req.BurstLimit
	pol.AllowedScopes = req.AllowedScopes
	pol.AllowedIPs = req.AllowedIPs
	pol.AllowedOrigins = req.AllowedOrigins
	pol.MaxKeyLifetime = parseDuration(req.MaxKeyLifetime)
	pol.RotationPeriod = parseDuration(req.RotationPeriod)
	pol.GracePeriod = parseDuration(req.GracePeriod)
	pol.DailyQuota = req.DailyQuota
	pol.MonthlyQuota = req.MonthlyQuota
	pol.UpdatedAt = time.Now()

	if err := a.eng.UpdatePolicy(ctx.Context(), pol); err != nil {
		return nil, mapStoreError(err)
	}

	resp := toPolicyResponse(pol)
	return resp, ctx.JSON(http.StatusOK, resp)
}

func (a *API) deletePolicy(ctx forge.Context, _ *ListPoliciesRequest) (*struct{}, error) {
	polID, err := id.ParsePolicyID(ctx.Param("policyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid policy ID: %v", err))
	}

	if err := a.eng.DeletePolicy(ctx.Context(), polID); err != nil {
		return nil, mapStoreError(err)
	}

	return nil, ctx.NoContent(http.StatusNoContent)
}
