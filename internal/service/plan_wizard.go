package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/repository"
)

// WizardHoldingItem is one holding in the plan wizard request.
type WizardHoldingItem struct {
	InstrumentID       string  `json:"instrument_id"`
	Enabled            bool    `json:"enabled"`
	WeightWithinGroup  float64 `json:"weight_within_group"`
	CurrentAmountMinor int64   `json:"current_amount_minor"`
	SortOrder          int     `json:"sort_order"`
}

// PlanWizardRequest atomically creates a plan with parameters, allocation and holdings.
type PlanWizardRequest struct {
	Name                   string                    `json:"name"`
	BaseCurrency           string                    `json:"base_currency"`
	ValuationDate          string                    `json:"valuation_date"`
	SelectedScenarioID     string                    `json:"selected_scenario_id"`
	Parameters             PlanParametersAPI         `json:"parameters"`
	Holdings               []WizardHoldingItem       `json:"holdings"`
	RegionTargets          []repository.RegionTarget `json:"region_targets"`
	ApplyUnallocatedToCash bool                      `json:"apply_unallocated_to_cash"`
}

// CreateWizard creates a complete plan in a single transaction.
func (s *PlanService) CreateWizard(ctx context.Context, req PlanWizardRequest) (PlanDetail, error) {
	if err := validateWizardRequest(req); err != nil {
		return PlanDetail{}, err
	}
	req.RegionTargets = normalizeWizardRegionTargets(req.RegionTargets)
	if req.BaseCurrency == "" {
		req.BaseCurrency = "CNY"
	}

	scenarioID := req.SelectedScenarioID
	params, err := ParametersFromAPI(req.Parameters)
	if err != nil {
		return PlanDetail{}, newErr("parameters_invalid", err.Error(), nil)
	}
	params.SelectedScenarioID = &scenarioID
	applyNewPlanAssumptionDefaults(&params)
	if err := validateParameters(params); err != nil {
		return PlanDetail{}, newErr("parameters_invalid", err.Error(), nil)
	}
	if err := validatePinnedProfileActive(
		ctx, repository.NewAssumptionProfileRepo(s.sql), params,
	); err != nil {
		return PlanDetail{}, newErr("parameters_invalid", err.Error(), nil)
	}

	scn, err := s.scenario.GetByID(ctx, req.SelectedScenarioID)
	if err != nil {
		if errors.Is(err, repository.ErrScenarioNotFound) {
			return PlanDetail{}, newErr("scenario_not_found", "scenario not found", nil)
		}
		return PlanDetail{}, wrapRepo("get scenario for wizard", err)
	}

	gap, err := wizardHoldingsGap(params, req)
	if err != nil {
		return PlanDetail{}, err
	}

	planID := "plan_" + uuid.New().String()
	instruments, err := s.loadWizardInstruments(ctx, req.Holdings, req.ValuationDate)
	if err != nil {
		return PlanDetail{}, err
	}
	pending, err := s.buildWizardPendingSnaps(ctx, planID, req.ValuationDate, req.Holdings)
	if err != nil {
		return PlanDetail{}, err
	}

	alloc := repository.PlanAllocation{
		AssetClassTargets: scn.Weights,
		RegionTargets:     req.RegionTargets,
	}
	built := buildWizardHoldings(planID, req, instruments, pending, gap)
	if err := validateWizardWeights(alloc, built); err != nil {
		return PlanDetail{}, err
	}

	plan := repository.Plan{
		ID: planID, Name: req.Name, BaseCurrency: req.BaseCurrency,
		ValuationDate: req.ValuationDate, Status: "active", ConfigVersion: 1,
	}
	params.PlanID = planID

	err = s.saveWizardPlanTx(ctx, plan, params, alloc, pending, built)
	if err != nil {
		return PlanDetail{}, wrapRepo("create wizard tx", err)
	}
	return s.Get(ctx, planID)
}

// applyNewPlanAssumptionDefaults fills the return-assumption selection for a newly
// created plan when the client omitted it. New plans use the forward-looking,
// auditable blended_prior calibration following the user's global profile
// (td/061 §4.2.3 / td/063 R0); the repository's historical_cagr default is only
// for migration-era rows.
func applyNewPlanAssumptionDefaults(p *repository.PlanParameters) {
	if p.ReturnAssumptionMode == "" {
		p.ReturnAssumptionMode = repository.ModeBlendedPrior
	}
	if p.AssumptionSelectionMode == "" {
		p.AssumptionSelectionMode = SelectionFollowGlobal
	}
	if p.ReturnAssumptionScenario == "" {
		p.ReturnAssumptionScenario = repository.DefaultReturnAssumptionScenario
	}
}

// CountPlans returns the number of plans in the database.
func CountPlans(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plans`).Scan(&n)
	return n, wrapRepo("count plans", err)
}

// CountSnapshots returns plan-specific simulation snapshots count.
func CountSnapshots(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM instrument_simulation_snapshots WHERE plan_id IS NOT NULL`).Scan(&n)
	return n, wrapRepo("count snapshots", err)
}

// WizardErrorCode extracts business error code for tests.
func WizardErrorCode(err error) string {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}
