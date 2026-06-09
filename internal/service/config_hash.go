package service

import (
	"context"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

// ConfigHashService computes configuration hashes for change detection.
type ConfigHashService struct {
	plans    *repository.PlanRepo
	params   *repository.ParametersRepo
	alloc    *repository.AllocationRepo
	holdings *repository.HoldingsRepo
}

func NewConfigHashService(
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	holdings *repository.HoldingsRepo,
) *ConfigHashService {
	return &ConfigHashService{plans: plans, params: params, alloc: alloc, holdings: holdings}
}

func (s *ConfigHashService) Compute(ctx context.Context, planID string) (string, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return "", err
	}
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return "", err
	}
	flows, err := s.params.ListCashFlows(ctx, planID)
	if err != nil {
		return "", err
	}
	alloc, err := s.alloc.Get(ctx, planID)
	if err != nil {
		return "", err
	}
	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return "", err
	}

	in := domain.ConfigHashInput{
		PlanID:        planID,
		Name:          plan.Name,
		BaseCurrency:  plan.BaseCurrency,
		ValuationDate: plan.ValuationDate,
		Parameters:    parametersToMap(params),
		CashFlows:     cashFlowsToMaps(flows),
		AssetClass:    assetClassToMaps(alloc.AssetClassTargets),
		RegionTargets: regionToMaps(alloc.RegionTargets),
		Holdings:      holdingsToMaps(holds),
	}
	return domain.ComputeConfigHash(in)
}

func parametersToMap(p repository.PlanParameters) map[string]any {
	m := map[string]any{
		"current_age":                 p.CurrentAge,
		"retirement_age":              p.RetirementAge,
		"end_age":                     p.EndAge,
		"total_assets_minor":          p.TotalAssetsMinor,
		"annual_savings_minor":        p.AnnualSavingsMinor,
		"annual_savings_growth_rate":  p.AnnualSavingsGrowthRate,
		"annual_spending_minor":       p.AnnualSpendingMinor,
		"terminal_wealth_floor_minor": p.TerminalWealthFloorMinor,
		"inflation_mode":              p.InflationMode,
		"fixed_inflation_rate":        p.FixedInflationRate,
		"inflation_mu":                p.InflationMu,
		"inflation_phi":               p.InflationPhi,
		"inflation_sigma":             p.InflationSigma,
		"withdrawal_type":             p.WithdrawalType,
		"withdrawal_rate":             p.WithdrawalRate,
		"withdrawal_floor_ratio":      p.WithdrawalFloorRatio,
		"withdrawal_ceiling_ratio":    p.WithdrawalCeilingRatio,
		"withdrawal_tax_rate":         p.WithdrawalTaxRate,
		"taxable_withdrawal_ratio":    p.TaxableWithdrawalRatio,
		"rebalance_frequency":         p.RebalanceFrequency,
		"rebalance_threshold":         p.RebalanceThreshold,
		"transaction_cost_rate":       p.TransactionCostRate,
		"simulation_runs":             p.SimulationRuns,
		"student_t_df":                p.StudentTDf,
	}
	if p.SelectedScenarioID != nil {
		m["selected_scenario_id"] = *p.SelectedScenarioID
	}
	if p.Seed != nil {
		m["seed"] = *p.Seed
	}
	return m
}

func cashFlowsToMaps(flows []repository.PlanCashFlow) []map[string]any {
	out := make([]map[string]any, 0, len(flows))
	for _, f := range flows {
		out = append(out, map[string]any{
			"id": f.ID, "name": f.Name, "kind": f.Kind,
			"amount_minor": f.AmountMinor, "start_month_offset": f.StartMonthOffset,
			"end_month_offset": f.EndMonthOffset, "recurrence": f.Recurrence,
			"inflation_linked": f.InflationLinked, "annual_growth_rate": f.AnnualGrowthRate,
			"enabled": f.Enabled, "note": f.Note,
		})
	}
	return out
}

func assetClassToMaps(targets []repository.AssetClassTarget) []map[string]any {
	out := make([]map[string]any, 0, len(targets))
	for _, t := range targets {
		out = append(out, map[string]any{"asset_class": t.AssetClass, "weight": t.Weight})
	}
	return out
}

func regionToMaps(targets []repository.RegionTarget) []map[string]any {
	out := make([]map[string]any, 0, len(targets))
	for _, t := range targets {
		out = append(out, map[string]any{
			"asset_class": t.AssetClass, "region": t.Region,
			"weight_within_class": t.WeightWithinClass,
		})
	}
	return out
}

func holdingsToMaps(holds []repository.PlanHolding) []map[string]any {
	out := make([]map[string]any, 0, len(holds))
	for _, h := range holds {
		out = append(out, map[string]any{
			"instrument_id": h.InstrumentID, "enabled": h.Enabled,
			"weight_within_group":    h.WeightWithinGroup,
			"current_amount_minor":   h.CurrentAmountMinor,
			"simulation_snapshot_id": h.SimulationSnapshotID,
			"sort_order":             h.SortOrder,
		})
	}
	return out
}
