package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

// Asset-level override guard rails (td/061 §4.1.5). Overrides are a rare,
// plan-specific escape hatch, so the accepted ranges are generous but still
// reject obvious nonsense (e.g. a 900% forward return or zero volatility).
const (
	overrideMinForwardReturn = -0.5
	overrideMaxForwardReturn = 0.5
	overrideMinVolatility    = 0.005
	overrideMaxVolatility    = 1.5
)

// ReturnOverrideView is the API view of one asset-level override.
type ReturnOverrideView struct {
	InstrumentID     string   `json:"instrument_id"`
	ForwardReturn    *float64 `json:"forward_return"`
	AnnualVolatility *float64 `json:"annual_volatility"`
	Reason           string   `json:"reason"`
	ExpiresAt        string   `json:"expires_at"`
	Expired          bool     `json:"expired"`
	CreatedAt        int64    `json:"created_at"`
	UpdatedAt        int64    `json:"updated_at"`
}

// SetReturnOverrideRequest is the upsert payload for an asset-level override.
type SetReturnOverrideRequest struct {
	ForwardReturn    *float64 `json:"forward_return"`
	AnnualVolatility *float64 `json:"annual_volatility"`
	Reason           string   `json:"reason"`
	ExpiresAt        string   `json:"expires_at"`
}

// ListReturnOverrides returns the plan's asset-level overrides, marking any that
// have expired relative to the plan's valuation date.
func (s *SimulationService) ListReturnOverrides(
	ctx context.Context, planID string,
) ([]ReturnOverrideView, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("get plan for overrides", err)
	}
	rows, err := s.overrides.ListByPlan(ctx, planID)
	if err != nil {
		return nil, wrapRepo("list return overrides", err)
	}
	out := make([]ReturnOverrideView, 0, len(rows))
	for _, o := range rows {
		out = append(out, toReturnOverrideView(o, plan.ValuationDate))
	}
	return out, nil
}

// SetReturnOverride validates and upserts an asset-level override. The instrument
// must be held by the plan (an override for an unrelated instrument would never
// affect the simulation). Only forward return / volatility may be overridden,
// and at least one must be present.
func (s *SimulationService) SetReturnOverride(
	ctx context.Context, planID, instrumentID string, req SetReturnOverrideRequest,
) (ReturnOverrideView, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return ReturnOverrideView{}, newErr("plan_not_found", "plan not found", nil)
		}
		return ReturnOverrideView{}, wrapRepo("get plan for override upsert", err)
	}
	if err := s.ensureInstrumentInPlan(ctx, planID, instrumentID); err != nil {
		return ReturnOverrideView{}, err
	}
	if err := validateOverrideRequest(req); err != nil {
		return ReturnOverrideView{}, err
	}

	o := repository.PlanReturnOverride{
		PlanID:           planID,
		InstrumentID:     instrumentID,
		ForwardReturn:    req.ForwardReturn,
		AnnualVolatility: req.AnnualVolatility,
		Reason:           strings.TrimSpace(req.Reason),
		ExpiresAt:        req.ExpiresAt,
	}
	if err := s.overrides.Upsert(ctx, nil, o); err != nil {
		return ReturnOverrideView{}, wrapRepo("upsert return override", err)
	}
	rows, err := s.overrides.ListByPlan(ctx, planID)
	if err != nil {
		return ReturnOverrideView{}, wrapRepo("reload return overrides", err)
	}
	for _, r := range rows {
		if r.InstrumentID == instrumentID {
			return toReturnOverrideView(r, plan.ValuationDate), nil
		}
	}
	return toReturnOverrideView(o, plan.ValuationDate), nil
}

// DeleteReturnOverride removes an asset-level override; removing a missing one is
// a no-op so the UI's clear action is idempotent.
func (s *SimulationService) DeleteReturnOverride(
	ctx context.Context, planID, instrumentID string,
) error {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return newErr("plan_not_found", "plan not found", nil)
		}
		return wrapRepo("get plan for override delete", err)
	}
	if err := s.overrides.Delete(ctx, nil, planID, instrumentID); err != nil {
		return wrapRepo("delete return override", err)
	}
	return nil
}

func (s *SimulationService) ensureInstrumentInPlan(ctx context.Context, planID, instrumentID string) error {
	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return wrapRepo("list holdings for override", err)
	}
	for _, h := range holds {
		if h.InstrumentID == instrumentID {
			return nil
		}
	}
	return newErr("instrument_not_in_plan",
		"instrument is not held by this plan; only held instruments can be overridden",
		map[string]any{"instrument_id": instrumentID})
}

func validateOverrideRequest(req SetReturnOverrideRequest) error {
	if strings.TrimSpace(req.Reason) == "" {
		return newErr("override_invalid", "reason is required for an asset-level override", nil)
	}
	if _, err := time.Parse("2006-01-02", req.ExpiresAt); err != nil {
		return newErr("override_invalid", "expires_at must be an ISO date (YYYY-MM-DD)", nil)
	}
	if req.ForwardReturn == nil && req.AnnualVolatility == nil {
		return newErr("override_invalid",
			"at least one of forward_return or annual_volatility must be provided", nil)
	}
	if req.ForwardReturn != nil {
		r := *req.ForwardReturn
		if r < overrideMinForwardReturn || r > overrideMaxForwardReturn {
			return newErr("override_invalid", "forward_return is out of the accepted range",
				map[string]any{"min": overrideMinForwardReturn, "max": overrideMaxForwardReturn})
		}
	}
	if req.AnnualVolatility != nil {
		v := *req.AnnualVolatility
		if v < overrideMinVolatility || v > overrideMaxVolatility {
			return newErr("override_invalid", "annual_volatility is out of the accepted range",
				map[string]any{"min": overrideMinVolatility, "max": overrideMaxVolatility})
		}
	}
	return nil
}

func toReturnOverrideView(o repository.PlanReturnOverride, valuationDate string) ReturnOverrideView {
	return ReturnOverrideView{
		InstrumentID:     o.InstrumentID,
		ForwardReturn:    o.ForwardReturn,
		AnnualVolatility: o.AnnualVolatility,
		Reason:           o.Reason,
		ExpiresAt:        o.ExpiresAt,
		Expired:          o.ExpiresAt < valuationDate,
		CreatedAt:        o.CreatedAt,
		UpdatedAt:        o.UpdatedAt,
	}
}
