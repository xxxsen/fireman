package service

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/repository"
)

func (s *DashboardService) loadScenarioName(ctx context.Context, scenarioID *string) string {
	if scenarioID == nil || *scenarioID == "" {
		return ""
	}
	scn, err := s.scenario.GetByID(ctx, *scenarioID)
	if err != nil {
		return ""
	}
	return scn.Name
}

func (s *DashboardService) loadLatestSimulationRun(ctx context.Context, planID string) *SimulationRunView {
	runs, err := s.sims.ListByPlan(ctx, planID, 1)
	if err != nil || len(runs) == 0 || runs[0].SuccessCount+runs[0].FailureCount == 0 {
		return nil
	}
	view, err := s.simulations.GetRun(ctx, runs[0].ID)
	if err != nil {
		return nil
	}
	return &view
}

func (s *RebalanceDraftService) recordCommitSnapshot(
	ctx context.Context,
	planID, draftID string,
	plan repository.Plan,
	req CommitRebalanceDraftRequest,
	existing []repository.PlanHolding,
	plannedByHolding map[string]int64,
) {
	items := make([]repository.PortfolioSnapshotItem, 0, len(existing))
	var total int64
	for _, h := range existing {
		amount := h.CurrentAmountMinor
		if planned, ok := plannedByHolding[h.ID]; ok {
			amount = planned
		}
		items = append(items, repository.PortfolioSnapshotItem{
			InstrumentID: h.InstrumentID, AmountMinor: amount,
		})
		total += amount
	}
	note := req.SnapshotNote
	if note == "" {
		note = "调仓计划提交后记录"
	}
	snap := repository.PortfolioSnapshot{
		ID: "psnap_" + uuid.New().String(), PlanID: planID,
		SnapshotDate: plan.ValuationDate, TotalAmountMinor: total, Note: note, Items: items,
	}
	if err := s.snapRepo.Create(ctx, snap); err != nil {
		slog.WarnContext(ctx, "rebalance draft commit snapshot failed",
			"plan_id", planID, "draft_id", draftID, "error", err)
	}
}
