package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

func validateWizardRequest(req PlanWizardRequest) error {
	if req.Name == "" || req.ValuationDate == "" {
		return newErr("validation_failed", "name and valuation_date are required", nil)
	}
	if req.SelectedScenarioID == "" {
		return newErr("validation_failed", "selected_scenario_id is required", nil)
	}
	if len(req.Holdings) == 0 {
		return newErr("validation_failed", "at least one holding is required", nil)
	}
	return validateRegionTargets(req.RegionTargets)
}

func wizardHoldingsGap(params repository.PlanParameters, req PlanWizardRequest) (int64, error) {
	enabledSum := int64(0)
	for _, h := range req.Holdings {
		if h.Enabled {
			enabledSum += h.CurrentAmountMinor
		}
	}
	gap := params.TotalAssetsMinor - enabledSum
	if gap < -100 {
		return 0, newErr("holdings_exceed_total", "enabled holdings exceed total assets", map[string]any{
			"total_assets_minor": params.TotalAssetsMinor, "holdings_sum_minor": enabledSum,
		})
	}
	if gap > 100 && !req.ApplyUnallocatedToCash {
		return 0, newErr("unallocated_gap_unresolved",
			"unallocated gap must be applied to cash or resolved via holdings", map[string]any{
				"gap_minor": gap,
			})
	}
	return gap, nil
}

func (s *PlanService) loadWizardInstruments(
	ctx context.Context,
	holdings []WizardHoldingItem,
	valuationDate string,
) (map[string]repository.Instrument, error) {
	instruments := make(map[string]repository.Instrument, len(holdings))
	for _, item := range holdings {
		instRec, err := s.instRepo.GetByID(ctx, item.InstrumentID)
		if err != nil {
			if errors.Is(err, repository.ErrInstrumentNotFound) {
				return nil, newErr("instrument_not_found", "instrument not found",
					map[string]any{"instrument_id": item.InstrumentID})
			}
			return nil, wrapRepo("get instrument for wizard", err)
		}
		if _, err := EvaluateInstrumentForPlan(ctx, instRec, s.marketRepo, valuationDate); err != nil {
			return nil, err
		}
		inst, err := s.holdings.GetInstrument(ctx, item.InstrumentID)
		if err != nil {
			return nil, wrapRepo("get instrument metadata for wizard", err)
		}
		instruments[item.InstrumentID] = inst
	}
	return instruments, nil
}

type wizardPendingSnap struct {
	snap repository.SimulationSnapshot
	skip bool
}

func (s *PlanService) buildWizardPendingSnaps(
	ctx context.Context,
	planID, valuationDate string,
	holdings []WizardHoldingItem,
) ([]wizardPendingSnap, error) {
	pending := make([]wizardPendingSnap, 0, len(holdings))
	for _, item := range holdings {
		snap, err := s.snapSvc.BuildSnapshotForHolding(ctx, planID, item.InstrumentID, valuationDate)
		if err != nil {
			return nil, MapSnapshotError(err)
		}
		pending = append(pending, wizardPendingSnap{
			snap: snap,
			skip: snap.ID == repository.SystemCashSnapshotID,
		})
	}
	return pending, nil
}

func buildWizardHoldings(
	planID string,
	req PlanWizardRequest,
	instruments map[string]repository.Instrument,
	pending []wizardPendingSnap,
	gap int64,
) []repository.PlanHolding {
	built := make([]repository.PlanHolding, 0, len(req.Holdings)+1)
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
	return built
}

func validateWizardWeights(alloc repository.PlanAllocation, built []repository.PlanHolding) error {
	da := toDomainAllocation(alloc)
	dh := holdingsToDomain(built)
	check := domain.ValidateAllWeights(da, dh)
	if check.Passed {
		return nil
	}
	msg := "holding weights invalid"
	for _, c := range check.Checks {
		if !c.Passed && c.Message != "" {
			msg = c.Message
			break
		}
	}
	return newErr("plan_weights_invalid", msg, map[string]any{"checks": check.Checks})
}
