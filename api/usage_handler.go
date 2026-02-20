package api

import (
	"fmt"
	"net/http"

	"github.com/xraph/forge"

	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/usage"
)

func (a *API) getKeyUsage(ctx forge.Context, req *GetKeyUsageRequest) ([]*UsageResponse, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	records, err := a.eng.QueryUsage(ctx.Context(), &usage.QueryFilter{
		KeyID:  &keyID,
		After:  parseTime(req.After),
		Before: parseTime(req.Before),
		Limit:  defaultLimit(req.Limit),
		Offset: req.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("query usage: %w", err)
	}

	resp := make([]*UsageResponse, len(records))
	for i, r := range records {
		resp[i] = toUsageResponse(r)
	}
	return resp, ctx.JSON(http.StatusOK, resp)
}

func (a *API) getKeyUsageAggregate(ctx forge.Context, req *GetKeyUsageAggregateRequest) ([]*AggregationResponse, error) {
	keyID, err := id.ParseKeyID(ctx.Param("keyId"))
	if err != nil {
		return nil, forge.BadRequest(fmt.Sprintf("invalid key ID: %v", err))
	}

	aggs, err := a.eng.AggregateUsage(ctx.Context(), &usage.QueryFilter{
		KeyID:  &keyID,
		Period: req.Period,
		After:  parseTime(req.After),
		Before: parseTime(req.Before),
	})
	if err != nil {
		return nil, fmt.Errorf("aggregate usage: %w", err)
	}

	resp := make([]*AggregationResponse, len(aggs))
	for i, a := range aggs {
		resp[i] = toAggregationResponse(a)
	}
	return resp, ctx.JSON(http.StatusOK, resp)
}

func (a *API) listUsage(ctx forge.Context, req *ListUsageRequest) ([]*AggregationResponse, error) {
	aggs, err := a.eng.AggregateUsage(ctx.Context(), &usage.QueryFilter{
		Period: req.Period,
		After:  parseTime(req.After),
		Before: parseTime(req.Before),
	})
	if err != nil {
		return nil, fmt.Errorf("list usage: %w", err)
	}

	resp := make([]*AggregationResponse, len(aggs))
	for i, ag := range aggs {
		resp[i] = toAggregationResponse(ag)
	}
	return resp, ctx.JSON(http.StatusOK, resp)
}
