package service

import (
	"math"

	"github.com/fireman/fireman/internal/repository"
)

// frozenClassification carries the asset_class/region already recorded on a
// plan's holding so structural refreshes keep the user's chosen classification.
type frozenClassification struct {
	assetClass string
	region     string
}

func validateAssetRefreshRequest(req AssetRefreshRequest) (map[string]int64, error) {
	if len(req.Holdings) == 0 {
		return nil, newErr("validation_failed", "holdings required", nil)
	}
	var sum int64
	amountByInstrument := make(map[string]int64, len(req.Holdings))
	for _, item := range req.Holdings {
		if item.AssetKey == "" {
			return nil, newErr("validation_failed", "asset_key required", nil)
		}
		if item.CurrentAmountMinor < 0 {
			return nil, newErr("validation_failed", "current amount cannot be negative", nil)
		}
		sum += item.CurrentAmountMinor
		amountByInstrument[item.AssetKey] = item.CurrentAmountMinor
	}
	if math.Abs(float64(sum-req.TotalAssetsMinor)) > amountToleranceMinor {
		return nil, newErr("validation_failed", "holdings sum does not match total assets", map[string]any{
			"holdings_sum_minor": sum, "total_assets_minor": req.TotalAssetsMinor,
		})
	}
	return amountByInstrument, nil
}

func buildAssetRefreshHoldingsReq(
	req AssetRefreshRequest,
	existing []repository.PlanHolding,
	amountByInstrument map[string]int64,
	pendingVersionBumps int,
) HoldingsUpdateRequest {
	holdingsVersion := req.ConfigVersion + pendingVersionBumps
	if assetRefreshIncludesStructure(req) {
		return buildAssetRefreshStructureReq(req, existing, holdingsVersion)
	}
	updateReq := buildAssetRefreshUpdateReq(req, existing, amountByInstrument)
	updateReq.ConfigVersion = holdingsVersion
	return updateReq
}

func assetRefreshIncludesStructure(req AssetRefreshRequest) bool {
	if req.ScenarioID != "" {
		return true
	}
	for _, item := range req.Holdings {
		if item.WeightWithinGroup != nil || item.SortOrder != nil {
			return true
		}
	}
	return false
}

func buildAssetRefreshStructureReq(
	req AssetRefreshRequest,
	existing []repository.PlanHolding,
	configVersion int,
) HoldingsUpdateRequest {
	classByAsset := make(map[string]frozenClassification, len(existing))
	for _, h := range existing {
		if _, ok := classByAsset[h.AssetKey]; !ok {
			classByAsset[h.AssetKey] = frozenClassification{assetClass: h.AssetClass, region: h.Region}
		}
	}
	items := make([]HoldingWriteItem, len(req.Holdings))
	for i, item := range req.Holdings {
		weight := 0.0
		if item.WeightWithinGroup != nil {
			weight = *item.WeightWithinGroup
		}
		sortOrder := i * 10
		if item.SortOrder != nil {
			sortOrder = *item.SortOrder
		}
		assetClass, region := item.AssetClass, item.Region
		if prev, ok := classByAsset[item.AssetKey]; ok {
			if assetClass == "" {
				assetClass = prev.assetClass
			}
			if region == "" {
				region = prev.region
			}
		}
		items[i] = HoldingWriteItem{
			AssetKey:           item.AssetKey,
			AssetClass:         assetClass,
			Region:             region,
			Enabled:            true,
			WeightWithinGroup:  weight,
			CurrentAmountMinor: item.CurrentAmountMinor,
			SortOrder:          sortOrder,
		}
	}
	return HoldingsUpdateRequest{ConfigVersion: configVersion, Holdings: items}
}

func buildAssetRefreshUpdateReq(
	req AssetRefreshRequest,
	existing []repository.PlanHolding,
	amountByInstrument map[string]int64,
) HoldingsUpdateRequest {
	updateReq := HoldingsUpdateRequest{
		ConfigVersion: req.ConfigVersion,
		Holdings:      make([]HoldingWriteItem, 0, len(existing)),
	}
	for _, h := range existing {
		amount := h.CurrentAmountMinor
		if v, ok := amountByInstrument[h.AssetKey]; ok {
			amount = v
		}
		updateReq.Holdings = append(updateReq.Holdings, HoldingWriteItem{
			AssetKey: h.AssetKey, Enabled: h.Enabled,
			AssetClass: h.AssetClass, Region: h.Region,
			WeightWithinGroup: h.WeightWithinGroup, CurrentAmountMinor: amount,
			SortOrder: h.SortOrder,
		})
	}
	return updateReq
}
