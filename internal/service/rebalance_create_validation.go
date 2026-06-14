package service

import (
	"context"
	"database/sql"
	"math"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

func loadStructuralRebalanceForCreate(
	ctx context.Context,
	sqlDB *sql.DB,
	rebalance *RebalanceService,
	planID string,
) (domain.RebalanceResult, error) {
	result, err := rebalance.GetRebalance(ctx, planID, domain.RebalanceModeFull, 0)
	if err != nil {
		return domain.RebalanceResult{}, wrapRepo("get rebalance for create", err)
	}
	if result.Summary.HoldingsTotalMinor <= 0 {
		return domain.RebalanceResult{}, newErr("validation_failed", "no enabled holdings", nil)
	}
	if result.Summary.StructuralActionableCount <= 0 {
		return domain.RebalanceResult{}, newErr("validation_failed", "no structural rebalance actions", nil)
	}
	maxGap := maxStructuralGapWeight(result.Lines)
	params, err := repository.NewParametersRepo(sqlDB).Get(ctx, planID)
	if err != nil {
		return domain.RebalanceResult{}, wrapRepo("get plan parameters for create", err)
	}
	if math.Abs(maxGap) <= params.RebalanceThreshold {
		return domain.RebalanceResult{}, newErr("validation_failed", "structural gap below rebalance threshold", nil)
	}
	return result, nil
}
