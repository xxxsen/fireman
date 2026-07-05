package service

import (
	"context"
)

func computeInvestedRatio(investedMinor, totalAssetsMinor int64) float64 {
	if totalAssetsMinor <= 0 {
		return 0
	}
	return float64(investedMinor) / float64(totalAssetsMinor)
}

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
