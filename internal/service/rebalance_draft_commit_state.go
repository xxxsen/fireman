package service

import (
	"context"

	"github.com/fireman/fireman/internal/repository"
)

type commitDraftState struct {
	plan             repository.Plan
	detail           RebalanceDraftDetail
	existing         []repository.PlanHolding
	plannedByHolding map[string]int64
	net              int64
}

func (s *RebalanceDraftService) loadCommitDraftState(
	ctx context.Context,
	planID, _ string,
	req CommitRebalanceDraftRequest,
	draft repository.RebalanceDraft,
) (commitDraftState, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return commitDraftState{}, wrapRepo("get plan for commit", err)
	}
	if err := validateCommitPlanVersions(req, plan, draft); err != nil {
		return commitDraftState{}, err
	}
	detail, err := s.buildDetail(ctx, draft)
	if err != nil {
		return commitDraftState{}, wrapRepo("build draft detail for commit", err)
	}
	for _, line := range detail.Lines {
		if line.PlannedCurrentMinor < 0 {
			return commitDraftState{}, newErr("validation_failed", "planned amount cannot be negative", nil)
		}
	}
	existing, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return commitDraftState{}, wrapRepo("list holdings for commit", err)
	}
	plannedByHolding := make(map[string]int64, len(detail.Lines))
	for _, line := range detail.Lines {
		plannedByHolding[line.HoldingID] = line.PlannedCurrentMinor
	}
	net := detail.FundPool.NetMinor
	net, err = resolveCommitFundPool(net, req, existing, plannedByHolding)
	if err != nil {
		return commitDraftState{}, err
	}
	return commitDraftState{
		plan: plan, detail: detail, existing: existing,
		plannedByHolding: plannedByHolding, net: net,
	}, nil
}
