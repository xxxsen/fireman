package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

const holdingRegionWeightTolerance = 1e-12

type HoldingRegionChangeRequest struct {
	HoldingID    string `json:"holding_id"`
	TargetRegion string `json:"target_region"`
	PreviewHash  string `json:"preview_hash,omitempty"`
}

type HoldingRegionWeightView struct {
	HoldingID             string  `json:"holding_id"`
	AssetKey              string  `json:"asset_key"`
	Region                string  `json:"region"`
	PortfolioTargetWeight float64 `json:"portfolio_target_weight"`
}

type HoldingRegionChangePreview struct {
	PreviewHash       string                    `json:"preview_hash"`
	PlanConfigVersion int                       `json:"plan_config_version"`
	HoldingID         string                    `json:"holding_id"`
	AssetKey          string                    `json:"asset_key"`
	AssetClass        string                    `json:"asset_class"`
	FromRegion        string                    `json:"from_region"`
	TargetRegion      string                    `json:"target_region"`
	BeforeRegions     []repository.RegionTarget `json:"before_region_targets"`
	AfterRegions      []repository.RegionTarget `json:"after_region_targets"`
	BeforeWeights     []HoldingRegionWeightView `json:"before_weights"`
	AfterWeights      []HoldingRegionWeightView `json:"after_weights"`
	allocation        repository.PlanAllocation
	holdings          []repository.PlanHolding
}

type HoldingRegionChangeResult struct {
	Preview    HoldingRegionChangePreview `json:"preview"`
	Plan       repository.Plan            `json:"plan"`
	Allocation repository.PlanAllocation  `json:"allocation"`
	Holdings   []repository.PlanHolding   `json:"holdings"`
}

func (s *HoldingsService) PreviewRegionChange(
	ctx context.Context, planID string, req HoldingRegionChangeRequest,
) (HoldingRegionChangePreview, error) {
	var preview HoldingRegionChangePreview
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		var err error
		preview, err = s.buildRegionChangePreviewTx(ctx, tx, planID, req)
		return err
	})
	if err != nil {
		return HoldingRegionChangePreview{}, fmt.Errorf("preview holding region change: %w", err)
	}
	return preview, nil
}

func (s *HoldingsService) ApplyRegionChange(
	ctx context.Context, planID string, req HoldingRegionChangeRequest,
) (HoldingRegionChangeResult, error) {
	if req.PreviewHash == "" {
		return HoldingRegionChangeResult{}, newErr("holding_region_change_preview_stale",
			"preview_hash is required", nil)
	}
	var result HoldingRegionChangeResult
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		preview, err := s.buildRegionChangePreviewTx(ctx, tx, planID, req)
		if err != nil {
			return err
		}
		if preview.PreviewHash != req.PreviewHash {
			return newErr("holding_region_change_preview_stale",
				"plan data changed after preview; preview the region change again", nil)
		}
		allocRepo := repository.NewAllocationRepo(s.sql)
		if err := allocRepo.Replace(ctx, tx, planID, preview.allocation); err != nil {
			return fmt.Errorf("replace allocation after region change: %w", err)
		}
		if err := s.holdings.Replace(ctx, tx, planID, preview.holdings); err != nil {
			return fmt.Errorf("replace holdings after region change: %w", err)
		}
		_, err = s.plans.BumpVersionTx(ctx, tx, planID, preview.PlanConfigVersion)
		if err != nil {
			if errors.Is(err, repository.ErrVersionConflict) {
				return newErr("holding_region_change_preview_stale",
					"plan data changed after preview; preview the region change again", nil)
			}
			return fmt.Errorf("bump plan version after region change: %w", err)
		}
		updatedPlan, err := s.plans.GetByIDTx(ctx, tx, planID)
		if err != nil {
			return fmt.Errorf("reload plan after region change: %w", err)
		}
		result = HoldingRegionChangeResult{
			Preview: preview, Plan: updatedPlan, Allocation: preview.allocation, Holdings: preview.holdings,
		}
		result.Preview.allocation = repository.PlanAllocation{}
		result.Preview.holdings = nil
		return nil
	})
	if err != nil {
		return HoldingRegionChangeResult{}, fmt.Errorf("apply holding region change: %w", err)
	}
	return result, nil
}

//nolint:gocyclo,lll // Preview validation stays in one transaction to prevent an incomplete write-set.
func (s *HoldingsService) buildRegionChangePreviewTx(
	ctx context.Context, tx *sql.Tx, planID string, req HoldingRegionChangeRequest,
) (HoldingRegionChangePreview, error) {
	if req.TargetRegion != domain.RegionDomestic && req.TargetRegion != domain.RegionForeign {
		return HoldingRegionChangePreview{}, newErr("holding_region_invalid",
			"target_region must be domestic or foreign", nil)
	}
	plan, err := s.plans.GetByIDTx(ctx, tx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return HoldingRegionChangePreview{}, newErr("plan_not_found", "plan not found", nil)
		}
		return HoldingRegionChangePreview{}, fmt.Errorf("load plan for region change: %w", err)
	}
	allocRepo := repository.NewAllocationRepo(s.sql)
	alloc, err := allocRepo.GetTx(ctx, tx, planID)
	if err != nil {
		return HoldingRegionChangePreview{}, fmt.Errorf("load allocation for region change: %w", err)
	}
	holdings, err := s.holdings.ListByPlanTx(ctx, tx, planID)
	if err != nil {
		return HoldingRegionChangePreview{}, fmt.Errorf("load holdings for region change: %w", err)
	}
	targetIndex := -1
	for i := range holdings {
		if holdings[i].ID == req.HoldingID {
			targetIndex = i
			break
		}
	}
	if targetIndex < 0 {
		return HoldingRegionChangePreview{}, newErr("holding_not_found", "holding not found", nil)
	}
	target := holdings[targetIndex]
	if target.AssetClass == domain.AssetClassCash || repository.IsSystemCashAssetKey(target.AssetKey) {
		return HoldingRegionChangePreview{}, newErr("holding_region_invalid",
			"system cash cannot be moved to another simulation region", nil)
	}

	beforeDomain := holdingsToDomain(holdings)
	beforeAlloc := toDomainAllocation(alloc)
	beforeAbs := make(map[string]float64, len(holdings))
	for i, h := range holdings {
		beforeAbs[h.ID] = domain.PortfolioTargetWeight(beforeAlloc, beforeDomain[i])
	}
	afterHoldings := append([]repository.PlanHolding(nil), holdings...)
	afterHoldings[targetIndex].Region = req.TargetRegion
	afterAlloc := rebuildRegionWeightsPreservingAbsolute(alloc, afterHoldings, beforeAbs)
	if checks := domain.ValidateAllWeights(toDomainAllocation(afterAlloc), holdingsToDomain(afterHoldings)); !checks.Passed {
		return HoldingRegionChangePreview{}, newErr("plan_weights_invalid",
			"region change could not produce valid weights", map[string]any{"checks": checks.Checks})
	}

	beforeViews := holdingRegionWeightViews(holdings, beforeAlloc)
	afterViews := holdingRegionWeightViews(afterHoldings, toDomainAllocation(afterAlloc))
	for i := range beforeViews {
		if beforeViews[i].HoldingID != afterViews[i].HoldingID ||
			math.Abs(beforeViews[i].PortfolioTargetWeight-afterViews[i].PortfolioTargetWeight) > holdingRegionWeightTolerance {
			return HoldingRegionChangePreview{}, newErr("holding_region_weight_changed",
				"region change would alter an absolute target weight", map[string]any{
					"holding_id": beforeViews[i].HoldingID,
					"before":     beforeViews[i].PortfolioTargetWeight,
					"after":      afterViews[i].PortfolioTargetWeight,
				})
		}
	}

	preview := HoldingRegionChangePreview{
		PlanConfigVersion: plan.ConfigVersion, HoldingID: target.ID, AssetKey: target.AssetKey,
		AssetClass: target.AssetClass, FromRegion: target.Region, TargetRegion: req.TargetRegion,
		BeforeRegions: regionTargetsForClass(alloc.RegionTargets, target.AssetClass),
		AfterRegions:  regionTargetsForClass(afterAlloc.RegionTargets, target.AssetClass),
		BeforeWeights: beforeViews, AfterWeights: afterViews,
		allocation: afterAlloc, holdings: afterHoldings,
	}
	preview.PreviewHash, err = hashHoldingRegionPreview(planID, preview)
	if err != nil {
		return HoldingRegionChangePreview{}, err
	}
	return preview, nil
}

func rebuildRegionWeightsPreservingAbsolute(
	alloc repository.PlanAllocation, holdings []repository.PlanHolding, absolute map[string]float64,
) repository.PlanAllocation {
	out := repository.PlanAllocation{
		AssetClassTargets: append([]repository.AssetClassTarget(nil), alloc.AssetClassTargets...),
		RegionTargets:     append([]repository.RegionTarget(nil), alloc.RegionTargets...),
	}
	classWeights := map[string]float64{}
	type groupKey struct{ assetClass, region string }
	for _, target := range out.AssetClassTargets {
		classWeights[target.AssetClass] = target.Weight
	}
	existingRegions := map[groupKey]bool{}
	for _, target := range out.RegionTargets {
		existingRegions[groupKey{target.AssetClass, target.Region}] = true
	}
	for assetClass := range classWeights {
		for _, region := range []string{domain.RegionDomestic, domain.RegionForeign} {
			key := groupKey{assetClass, region}
			if !existingRegions[key] {
				out.RegionTargets = append(out.RegionTargets, repository.RegionTarget{
					AssetClass: assetClass, Region: region,
				})
			}
		}
	}
	sort.Slice(out.RegionTargets, func(i, j int) bool {
		left := out.RegionTargets[i].AssetClass + "/" + out.RegionTargets[i].Region
		right := out.RegionTargets[j].AssetClass + "/" + out.RegionTargets[j].Region
		return left < right
	})
	groupAbs := map[groupKey]float64{}
	for _, holding := range holdings {
		if holding.Enabled {
			groupAbs[groupKey{holding.AssetClass, holding.Region}] += absolute[holding.ID]
		}
	}
	for i := range out.RegionTargets {
		target := &out.RegionTargets[i]
		classWeight := classWeights[target.AssetClass]
		if classWeight > 0 {
			target.WeightWithinClass = groupAbs[groupKey{target.AssetClass, target.Region}] / classWeight
		}
	}
	for i := range holdings {
		if !holdings[i].Enabled {
			continue
		}
		groupWeight := groupAbs[groupKey{holdings[i].AssetClass, holdings[i].Region}]
		if groupWeight > 0 {
			holdings[i].WeightWithinGroup = absolute[holdings[i].ID] / groupWeight
		}
	}
	return out
}

func holdingRegionWeightViews(
	holdings []repository.PlanHolding, alloc domain.AllocationWeights,
) []HoldingRegionWeightView {
	domainHoldings := holdingsToDomain(holdings)
	out := make([]HoldingRegionWeightView, len(holdings))
	for i, holding := range holdings {
		out[i] = HoldingRegionWeightView{
			HoldingID: holding.ID, AssetKey: holding.AssetKey, Region: holding.Region,
			PortfolioTargetWeight: domain.PortfolioTargetWeight(alloc, domainHoldings[i]),
		}
	}
	return out
}

func regionTargetsForClass(all []repository.RegionTarget, assetClass string) []repository.RegionTarget {
	var out []repository.RegionTarget
	for _, target := range all {
		if target.AssetClass == assetClass {
			out = append(out, target)
		}
	}
	return out
}

func hashHoldingRegionPreview(planID string, preview HoldingRegionChangePreview) (string, error) {
	payload := struct {
		PlanID            string                    `json:"plan_id"`
		PlanConfigVersion int                       `json:"plan_config_version"`
		HoldingID         string                    `json:"holding_id"`
		TargetRegion      string                    `json:"target_region"`
		BeforeRegions     []repository.RegionTarget `json:"before_regions"`
		AfterRegions      []repository.RegionTarget `json:"after_regions"`
		BeforeWeights     []HoldingRegionWeightView `json:"before_weights"`
		AfterWeights      []HoldingRegionWeightView `json:"after_weights"`
	}{
		planID, preview.PlanConfigVersion, preview.HoldingID, preview.TargetRegion,
		preview.BeforeRegions, preview.AfterRegions, preview.BeforeWeights, preview.AfterWeights,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode holding region preview: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
