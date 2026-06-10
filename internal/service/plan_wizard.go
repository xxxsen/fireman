package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
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
	Name                   string              `json:"name"`
	BaseCurrency           string              `json:"base_currency"`
	ValuationDate          string              `json:"valuation_date"`
	SelectedScenarioID     string              `json:"selected_scenario_id"`
	Parameters             PlanParametersAPI   `json:"parameters"`
	Holdings               []WizardHoldingItem `json:"holdings"`
	ApplyUnallocatedToCash bool                `json:"apply_unallocated_to_cash"`
}

// CreateWizard creates a complete plan in a single transaction.
func (s *PlanService) CreateWizard(ctx context.Context, req PlanWizardRequest) (PlanDetail, error) {
	if req.Name == "" || req.ValuationDate == "" {
		return PlanDetail{}, newErr("validation_failed", "name and valuation_date are required", nil)
	}
	if req.SelectedScenarioID == "" {
		return PlanDetail{}, newErr("validation_failed", "selected_scenario_id is required", nil)
	}
	if req.BaseCurrency == "" {
		req.BaseCurrency = "CNY"
	}
	if len(req.Holdings) == 0 {
		return PlanDetail{}, newErr("validation_failed", "at least one holding is required", nil)
	}

	scenarioID := req.SelectedScenarioID
	params, err := ParametersFromAPI(req.Parameters)
	if err != nil {
		return PlanDetail{}, newErr("parameters_invalid", err.Error(), nil)
	}
	params.SelectedScenarioID = &scenarioID
	if err := validateParameters(params); err != nil {
		return PlanDetail{}, newErr("parameters_invalid", err.Error(), nil)
	}

	scn, err := s.scenario.GetByID(ctx, req.SelectedScenarioID)
	if err != nil {
		if errors.Is(err, repository.ErrScenarioNotFound) {
			return PlanDetail{}, newErr("scenario_not_found", "scenario not found", nil)
		}
		return PlanDetail{}, err
	}

	enabledSum := int64(0)
	for _, h := range req.Holdings {
		if h.Enabled {
			enabledSum += h.CurrentAmountMinor
		}
	}
	gap := params.TotalAssetsMinor - enabledSum
	if gap < -100 {
		return PlanDetail{}, newErr("holdings_exceed_total", "enabled holdings exceed total assets", map[string]any{
			"total_assets_minor": params.TotalAssetsMinor, "holdings_sum_minor": enabledSum,
		})
	}
	if gap > 100 && !req.ApplyUnallocatedToCash {
		return PlanDetail{}, newErr("unallocated_gap_unresolved", "unallocated gap must be applied to cash or resolved via holdings", map[string]any{
			"gap_minor": gap,
		})
	}

	planID := "plan_" + uuid.New().String()

	instruments := make(map[string]repository.Instrument, len(req.Holdings))
	for _, item := range req.Holdings {
		instRec, err := s.instRepo.GetByID(ctx, item.InstrumentID)
		if err != nil {
			if errors.Is(err, repository.ErrInstrumentNotFound) {
				return PlanDetail{}, newErr("instrument_not_found", "instrument not found", map[string]any{"instrument_id": item.InstrumentID})
			}
			return PlanDetail{}, err
		}
		if _, err := EvaluateInstrumentForPlan(ctx, instRec, s.marketRepo, req.ValuationDate); err != nil {
			return PlanDetail{}, err
		}
		inst, err := s.holdings.GetInstrument(ctx, item.InstrumentID)
		if err != nil {
			return PlanDetail{}, err
		}
		instruments[item.InstrumentID] = inst
	}

	type pendingSnap struct {
		snap repository.SimulationSnapshot
		skip bool
	}
	pending := make([]pendingSnap, 0, len(req.Holdings))
	for _, item := range req.Holdings {
		snap, err := s.snapSvc.BuildSnapshotForHolding(ctx, planID, item.InstrumentID, req.ValuationDate)
		if err != nil {
			return PlanDetail{}, MapSnapshotError(err)
		}
		pending = append(pending, pendingSnap{snap: snap, skip: snap.ID == repository.SystemCashSnapshotID})
	}

	alloc := repository.PlanAllocation{
		AssetClassTargets: scn.Weights,
		RegionTargets:     defaultRegionTargets(),
	}

	var built []repository.PlanHolding
	for i, item := range req.Holdings {
		inst := instruments[item.InstrumentID]
		snap := pending[i].snap
		built = append(built, repository.PlanHolding{
			ID: "hold_" + uuid.New().String(), PlanID: planID,
			InstrumentID: item.InstrumentID, Enabled: item.Enabled,
			AssetClass: inst.AssetClass, Region: inst.Region,
			WeightWithinGroup: item.WeightWithinGroup, CurrentAmountMinor: item.CurrentAmountMinor,
			SimulationSnapshotID: snap.ID, SortOrder: item.SortOrder,
		})
	}

	if req.ApplyUnallocatedToCash && gap > 100 {
		built = append(built, repository.PlanHolding{
			ID: "hold_" + uuid.New().String(), PlanID: planID,
			InstrumentID: repository.SystemCashInstrumentID, Enabled: true,
			AssetClass: domain.AssetClassCash, Region: domain.RegionDomestic,
			WeightWithinGroup: 1.0, CurrentAmountMinor: gap,
			SimulationSnapshotID: repository.SystemCashSnapshotID, SortOrder: 9999,
		})
	}

	da := toDomainAllocation(alloc)
	dh := holdingsToDomain(built)
	check := domain.ValidateAllWeights(da, dh)
	if !check.Passed {
		msg := "holding weights invalid"
		for _, c := range check.Checks {
			if !c.Passed && c.Message != "" {
				msg = c.Message
				break
			}
		}
		return PlanDetail{}, newErr("plan_weights_invalid", msg, map[string]any{"checks": check.Checks})
	}

	plan := repository.Plan{
		ID: planID, Name: req.Name, BaseCurrency: req.BaseCurrency,
		ValuationDate: req.ValuationDate, Status: "active", ConfigVersion: 1,
	}
	params.PlanID = planID

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.plans.CreateTx(ctx, tx, plan); err != nil {
			return err
		}
		for _, ps := range pending {
			if ps.skip {
				continue
			}
			if err := s.snapSvc.CreatePlanSnapshotTx(ctx, tx, ps.snap); err != nil {
				return err
			}
		}
		if err := s.params.Upsert(ctx, tx, params); err != nil {
			return err
		}
		if err := s.alloc.Replace(ctx, tx, planID, alloc); err != nil {
			return err
		}
		return s.holdings.Replace(ctx, tx, planID, built)
	})
	if err != nil {
		return PlanDetail{}, err
	}
	return s.Get(ctx, planID)
}

// CountPlans returns the number of plans in the database.
func CountPlans(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plans`).Scan(&n)
	return n, err
}

// CountSnapshots returns plan-specific simulation snapshots count.
func CountSnapshots(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM instrument_simulation_snapshots WHERE plan_id IS NOT NULL`).Scan(&n)
	return n, err
}

// WizardErrorCode extracts business error code for tests.
func WizardErrorCode(err error) string {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}
