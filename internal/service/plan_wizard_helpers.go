package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/marketdata"
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
	if err := validateRegionTargets(normalizeWizardRegionTargets(req.RegionTargets)); err != nil {
		return newErr("validation_failed", err.Error(), nil)
	}
	return nil
}

// normalizeWizardRegionTargets fills missing asset_class rows with domestic=100% defaults.
func normalizeWizardRegionTargets(targets []repository.RegionTarget) []repository.RegionTarget {
	if len(targets) == 0 {
		return defaultRegionTargets()
	}
	present := map[string]struct{}{}
	for _, t := range targets {
		present[t.AssetClass] = struct{}{}
	}
	out := append([]repository.RegionTarget(nil), targets...)
	for _, ac := range domain.AssetClasses {
		if _, ok := present[ac]; ok {
			continue
		}
		for _, region := range domain.Regions {
			w := 0.0
			if region == domain.RegionDomestic {
				w = 1.0
			}
			out = append(out, repository.RegionTarget{
				AssetClass: ac, Region: region, WeightWithinClass: w,
			})
		}
	}
	return out
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

// validateWizardAssets checks every wizard holding against the market asset
// directory: existence, active status and user-chosen classification, plus
// the plan-level asset_key uniqueness rule — one market asset may only be
// owned by a single asset_class/region within a plan.
func (s *PlanService) validateWizardAssets(
	ctx context.Context,
	holdings []WizardHoldingItem,
) error {
	seen := make(map[string]struct{}, len(holdings))
	for _, item := range holdings {
		if !isValidHoldingAssetClass(item.AssetClass) || !isValidHoldingRegion(item.Region) {
			return newErr("holding_classification_invalid",
				"asset_class must be equity/bond/cash and region must be domestic/foreign",
				map[string]any{"asset_key": item.AssetKey})
		}
		if _, ok := seen[item.AssetKey]; ok {
			return newErr("holding_duplicate",
				"duplicate asset_key within the plan",
				map[string]any{"asset_key": item.AssetKey})
		}
		seen[item.AssetKey] = struct{}{}
		asset, err := s.assetRepo.GetByKey(ctx, item.AssetKey)
		if err != nil {
			if errors.Is(err, repository.ErrMarketAssetNotFound) {
				return newErr("market_asset_not_found",
					"market asset not found; sync the asset directory first",
					map[string]any{"asset_key": item.AssetKey})
			}
			return wrapRepo("get market asset for wizard", err)
		}
		if !asset.Active && !item.AllowInactive {
			return newErr("market_asset_inactive",
				"market asset is inactive; set allow_inactive to keep it",
				map[string]any{"asset_key": item.AssetKey})
		}
	}
	return nil
}

type wizardPendingSnap struct {
	snap repository.SimulationSnapshot
	skip bool
}

// buildWizardPendingSnaps builds simulation snapshots for the wizard
// holdings. Assets without local history get an empty snapshot id (lazy);
// simulation readiness gates simulation until their history is synced.
func (s *PlanService) buildWizardPendingSnaps(
	ctx context.Context,
	planID, valuationDate string,
	holdings []WizardHoldingItem,
) (map[string]string, []wizardPendingSnap, error) {
	snapIDs := make(map[string]string, len(holdings))
	pending := make([]wizardPendingSnap, 0, len(holdings))
	for _, item := range holdings {
		if _, ok := snapIDs[item.AssetKey]; ok {
			continue
		}
		snap, err := s.snapSvc.BuildSnapshotForHolding(ctx, planID, item.AssetKey, valuationDate)
		if err != nil {
			var snapErr *marketdata.SnapshotError
			if errors.As(err, &snapErr) {
				snapIDs[item.AssetKey] = ""
				continue
			}
			return nil, nil, MapSnapshotError(err)
		}
		snapIDs[item.AssetKey] = snap.ID
		_, isCash := repository.SystemCashSnapshotIDForAsset(item.AssetKey)
		pending = append(pending, wizardPendingSnap{snap: snap, skip: isCash})
	}
	return snapIDs, pending, nil
}

func buildWizardHoldings(
	planID string,
	req PlanWizardRequest,
	snapIDs map[string]string,
	gap int64,
) []repository.PlanHolding {
	built := make([]repository.PlanHolding, 0, len(req.Holdings)+1)
	for _, item := range req.Holdings {
		built = append(built, repository.PlanHolding{
			ID: "hold_" + uuid.New().String(), PlanID: planID,
			AssetKey: item.AssetKey, Enabled: item.Enabled,
			AssetClass: item.AssetClass, Region: item.Region,
			WeightWithinGroup: item.WeightWithinGroup, CurrentAmountMinor: item.CurrentAmountMinor,
			SimulationSnapshotID: snapIDs[item.AssetKey], SortOrder: item.SortOrder,
		})
	}
	if req.ApplyUnallocatedToCash && gap > 100 {
		// Merge into an existing base-currency cash row so the plan-level
		// asset_key uniqueness holds: validateWizardAssets already
		// guarantees at most one row per asset_key, so matching by asset_key
		// alone can never merge into the wrong row.
		merged := false
		for i := range built {
			if built[i].AssetKey == repository.SystemCashAssetKey {
				built[i].CurrentAmountMinor += gap
				built[i].Enabled = true
				merged = true
				break
			}
		}
		if !merged {
			built = append(built, repository.PlanHolding{
				ID: "hold_" + uuid.New().String(), PlanID: planID,
				AssetKey: repository.SystemCashAssetKey, Enabled: true,
				AssetClass: domain.AssetClassCash, Region: domain.RegionDomestic,
				WeightWithinGroup: 1.0, CurrentAmountMinor: gap,
				SimulationSnapshotID: repository.SystemCashSnapshotID, SortOrder: 9999,
			})
		}
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
