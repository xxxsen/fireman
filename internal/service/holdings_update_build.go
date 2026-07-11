package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// buildOnePreparedHolding validates one holding write item against the global
// market asset directory and prepares its simulation snapshot. Assets without
// local history are saved with an empty snapshot id; the simulation readiness
// check gates simulation until their history is synced.
func (s *HoldingsService) buildOnePreparedHolding(
	ctx context.Context,
	tx *sql.Tx,
	plan repository.Plan,
	item HoldingWriteItem,
	existingSnap map[string]string,
) (repository.PlanHolding, *pendingHoldingSnap, error) {
	if item.SimulationSnapshotID != nil {
		return repository.PlanHolding{}, nil, newErr(
			"holding_fields_read_only",
			"simulation_snapshot_id is read-only",
			nil,
		)
	}
	if !isValidHoldingAssetClass(item.AssetClass) || !isValidHoldingRegion(item.Region) {
		return repository.PlanHolding{}, nil, newErr(
			"holding_classification_invalid",
			"asset_class must be equity/bond/cash and region must be domestic/foreign",
			map[string]any{"asset_key": item.AssetKey},
		)
	}
	var asset repository.MarketAsset
	var err error
	if tx != nil {
		asset, err = s.assetRepo.GetByKeyTx(ctx, tx, item.AssetKey)
	} else {
		asset, err = s.assetRepo.GetByKey(ctx, item.AssetKey)
	}
	if err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return repository.PlanHolding{}, nil, newErr("market_asset_not_found",
				"market asset not found; sync the asset directory first",
				map[string]any{"asset_key": item.AssetKey})
		}
		return repository.PlanHolding{}, nil, wrapRepo("load market asset", err)
	}
	if !asset.Active && !item.AllowInactive {
		return repository.PlanHolding{}, nil, newErr("market_asset_inactive",
			"market asset is inactive; set allow_inactive to keep it",
			map[string]any{"asset_key": item.AssetKey})
	}
	if asset.InstrumentType == "cash" && asset.Currency != plan.BaseCurrency {
		return repository.PlanHolding{}, nil, newErr("foreign_cash_not_supported",
			"FIRE 计划目前只支持与计划基准币种相同的现金持仓",
			map[string]any{"asset_key": item.AssetKey, "cash_currency": asset.Currency, "base_currency": plan.BaseCurrency})
	}

	snapID, ok := existingSnap[item.AssetKey]
	var pending *pendingHoldingSnap
	if !ok {
		snapID, pending, err = s.tryBuildHoldingSnapshot(ctx, tx, plan, item.AssetKey)
		if err != nil {
			return repository.PlanHolding{}, nil, err
		}
	}
	holding := repository.PlanHolding{
		ID: "hold_" + uuid.New().String(), PlanID: plan.ID,
		AssetKey: item.AssetKey, Enabled: item.Enabled,
		AssetClass: item.AssetClass, Region: item.Region,
		WeightWithinGroup: item.WeightWithinGroup, CurrentAmountMinor: item.CurrentAmountMinor,
		SimulationSnapshotID: snapID, SortOrder: item.SortOrder,
	}
	return holding, pending, nil
}

// tryBuildHoldingSnapshot builds a plan snapshot for an asset joining the
// plan. Missing or insufficient history is not an error at save time: the
// holding is stored with an empty snapshot id (lazy) and readiness reports it.
func (s *HoldingsService) tryBuildHoldingSnapshot(
	ctx context.Context, tx *sql.Tx, plan repository.Plan, assetKey string,
) (string, *pendingHoldingSnap, error) {
	snap, err := s.snapSvc.BuildSnapshotForHoldingTx(ctx, tx, plan.ID, assetKey, plan.ValuationDate)
	if err != nil {
		var snapErr *marketdata.SnapshotError
		if errors.As(err, &snapErr) {
			return "", nil, nil
		}
		return "", nil, MapSnapshotError(err)
	}
	_, isCash := repository.SystemCashSnapshotIDForAsset(assetKey)
	return snap.ID, &pendingHoldingSnap{snap: snap, skip: isCash}, nil
}

func isValidHoldingAssetClass(v string) bool {
	for _, ac := range domain.AssetClasses {
		if ac == v {
			return true
		}
	}
	return false
}

func isValidHoldingRegion(v string) bool {
	for _, r := range domain.Regions {
		if r == v {
			return true
		}
	}
	return false
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
