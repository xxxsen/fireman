package service

import (
	"context"
	"errors"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

// RebalanceService computes rebalance suggestions.
type RebalanceService struct {
	plans    *repository.PlanRepo
	params   *repository.ParametersRepo
	alloc    *repository.AllocationRepo
	holdings *repository.HoldingsRepo
}

func NewRebalanceService(
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	holdings *repository.HoldingsRepo,
) *RebalanceService {
	return &RebalanceService{plans: plans, params: params, alloc: alloc, holdings: holdings}
}

func (s *RebalanceService) GetRebalance(ctx context.Context, planID, mode string, newCashMinor int64) (domain.RebalanceResult, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return domain.RebalanceResult{}, newErr("plan_not_found", "plan not found", nil)
		}
		return domain.RebalanceResult{}, err
	}
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return domain.RebalanceResult{}, err
	}
	alloc, err := s.alloc.Get(ctx, planID)
	if err != nil {
		return domain.RebalanceResult{}, err
	}
	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return domain.RebalanceResult{}, err
	}
	da := toDomainAllocation(alloc)
	dh := holdingsToDomain(holds)
	meta := holdingMeta(holds)
	total := params.TotalAssetsMinor
	threshold := params.RebalanceThreshold
	costRate := params.TransactionCostRate

	switch mode {
	case "", domain.RebalanceModeFull:
		result := domain.ComputeFullRebalance(da, dh, meta, total, threshold, costRate)
		enrichRebalanceLines(&result, holds)
		return result, nil
	case domain.RebalanceModeNewCash:
		if newCashMinor <= 0 {
			return domain.RebalanceResult{}, newErr("validation_failed", "new_cash_minor must be > 0 for new_cash mode", nil)
		}
		result := domain.ComputeNewCashRebalance(da, dh, meta, total, newCashMinor, threshold, costRate)
		enrichRebalanceLines(&result, holds)
		return result, nil
	default:
		return domain.RebalanceResult{}, newErr("validation_failed", "mode must be full or new_cash", nil)
	}
}

func enrichRebalanceLines(result *domain.RebalanceResult, holds []repository.PlanHolding) {
	for i := range result.Lines {
		for _, h := range holds {
			if h.ID == result.Lines[i].HoldingID {
				result.Lines[i].InstrumentName = h.InstrumentName
				result.Lines[i].InstrumentCode = h.InstrumentCode
				break
			}
		}
	}
}
