package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

// frozenClassification carries the asset_class/region already recorded on a
// plan's holding so structural updates keep the plan-level frozen copy instead of
// silently adopting the library's current classification.
type frozenClassification struct {
	assetClass string
	region     string
}

func (s *HoldingsService) buildOnePreparedHolding(
	ctx context.Context,
	plan repository.Plan,
	item HoldingWriteItem,
	existingSnap map[string]string,
	existingClass map[string]frozenClassification,
) (repository.PlanHolding, *pendingHoldingSnap, error) {
	if item.AssetClass != nil || item.Region != nil || item.SimulationSnapshotID != nil {
		return repository.PlanHolding{}, nil, newErr(
			"holding_fields_read_only",
			"asset_class, region and simulation_snapshot_id are read-only",
			nil,
		)
	}
	instRec, err := s.instRepo.GetByID(ctx, item.InstrumentID)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return repository.PlanHolding{}, nil, newErr("instrument_not_found", "instrument not found",
				map[string]any{"instrument_id": item.InstrumentID})
		}
		return repository.PlanHolding{}, nil, wrapRepo("load instrument", err)
	}
	if _, err := EvaluateInstrumentForPlan(ctx, instRec, s.marketRepo, plan.ValuationDate); err != nil {
		return repository.PlanHolding{}, nil, err
	}
	inst, err := s.holdings.GetInstrument(ctx, item.InstrumentID)
	if err != nil {
		return repository.PlanHolding{}, nil, wrapRepo("load holding instrument", err)
	}
	snapID, ok := existingSnap[item.InstrumentID]
	var pending *pendingHoldingSnap
	if !ok {
		snap, err := s.snapSvc.BuildSnapshotForHolding(ctx, plan.ID, item.InstrumentID, plan.ValuationDate)
		if err != nil {
			return repository.PlanHolding{}, nil, MapSnapshotError(err)
		}
		snapID = snap.ID
		pending = &pendingHoldingSnap{
			snap: snap,
			skip: snap.ID == repository.SystemCashSnapshotID,
		}
	}
	// Freeze rule: an asset already in this plan keeps its plan-level
	// classification; only assets joining the plan for the first time copy the
	// library's current asset_class/region.
	assetClass, region := inst.AssetClass, inst.Region
	if prev, ok := existingClass[item.InstrumentID]; ok {
		assetClass, region = prev.assetClass, prev.region
	}
	holding := repository.PlanHolding{
		ID: "hold_" + uuid.New().String(), PlanID: plan.ID,
		InstrumentID: item.InstrumentID, Enabled: item.Enabled,
		AssetClass: assetClass, Region: region,
		WeightWithinGroup: item.WeightWithinGroup, CurrentAmountMinor: item.CurrentAmountMinor,
		SimulationSnapshotID: snapID, SortOrder: item.SortOrder,
	}
	return holding, pending, nil
}

func initialPlanAllocation(ctx context.Context, s *PlanService, scenarioID *string) (repository.PlanAllocation, error) {
	var alloc repository.PlanAllocation
	if scenarioID == nil {
		for _, ac := range domain.AssetClasses {
			alloc.AssetClassTargets = append(alloc.AssetClassTargets, repository.AssetClassTarget{
				AssetClass: ac, Weight: 0,
			})
		}
		alloc.RegionTargets = defaultRegionTargets()
		return alloc, nil
	}
	scn, err := s.scenario.GetByID(ctx, *scenarioID)
	if err != nil {
		if errors.Is(err, repository.ErrScenarioNotFound) {
			return repository.PlanAllocation{}, newErr("scenario_not_found", "scenario not found", nil)
		}
		return repository.PlanAllocation{}, wrapRepo("load scenario", err)
	}
	alloc.AssetClassTargets = scn.Weights
	alloc.RegionTargets = defaultRegionTargets()
	return alloc, nil
}

func validateParametersAssetsGap(
	params repository.PlanParameters,
	holds []repository.PlanHolding,
	applyCash bool,
) error {
	enabledSum := int64(0)
	for _, h := range holds {
		if h.Enabled {
			enabledSum += h.CurrentAmountMinor
		}
	}
	gap := params.TotalAssetsMinor - enabledSum
	if gap < -100 {
		return newErr("holdings_exceed_total", "enabled holdings exceed total assets",
			map[string]any{
				"total_assets_minor": params.TotalAssetsMinor, "holdings_sum_minor": enabledSum,
			})
	}
	if gap > 100 && !applyCash {
		return newErr("unallocated_gap_unresolved",
			"unallocated gap must be applied to cash or resolved via holdings", map[string]any{
				"gap_minor": gap,
			})
	}
	return nil
}

func enabledHoldingsSum(holds []repository.PlanHolding) int64 {
	sum := int64(0)
	for _, h := range holds {
		if h.Enabled {
			sum += h.CurrentAmountMinor
		}
	}
	return sum
}
