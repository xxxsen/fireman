package service

import (
	"context"
	"errors"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

// TargetView is the read-only target configuration page data.
type TargetView struct {
	TotalAssetsMinor int64                         `json:"total_assets_minor"`
	ConfigHash       string                        `json:"config_hash"`
	WeightChecks     domain.WeightValidationResult `json:"weight_checks"`
	AssetClass       []repository.AssetClassTarget `json:"asset_class_targets"`
	RegionTargets    []repository.RegionTarget     `json:"region_targets"`
	Holdings         []domain.HoldingTargetLine    `json:"holdings"`
}

// TargetService computes read-only target expansion.
type TargetService struct {
	plans    *repository.PlanRepo
	params   *repository.ParametersRepo
	alloc    *repository.AllocationRepo
	holdings *repository.HoldingsRepo
	hash     *ConfigHashService
}

func NewTargetService(
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	holdings *repository.HoldingsRepo,
	hash *ConfigHashService,
) *TargetService {
	return &TargetService{plans: plans, params: params, alloc: alloc, holdings: holdings, hash: hash}
}

func (s *TargetService) GetTargets(ctx context.Context, planID string) (TargetView, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return TargetView{}, newErr("plan_not_found", "plan not found", nil)
		}
		return TargetView{}, err
	}
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return TargetView{}, err
	}
	alloc, err := s.alloc.Get(ctx, planID)
	if err != nil {
		return TargetView{}, err
	}
	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return TargetView{}, err
	}
	da := toDomainAllocation(alloc)
	dh := holdingsToDomain(holds)
	meta := holdingMeta(holds)
	lines := domain.ComputeHoldingTargets(da, dh, meta, params.TotalAssetsMinor)
	enrichInstrumentNames(lines, holds)
	hash, err := s.hash.Compute(ctx, planID)
	if err != nil {
		return TargetView{}, err
	}
	return TargetView{
		TotalAssetsMinor: params.TotalAssetsMinor,
		ConfigHash:       hash,
		WeightChecks:     domain.ValidateAllWeights(da, dh),
		AssetClass:       alloc.AssetClassTargets,
		RegionTargets:    alloc.RegionTargets,
		Holdings:         lines,
	}, nil
}
