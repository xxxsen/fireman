package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

var errAssumptionRepositoryNotConfigured = errors.New(
	"resolve effective assumption identity: repository is not configured",
)

// ConfigHashService computes configuration hashes for change detection.
type ConfigHashService struct {
	plans       *repository.PlanRepo
	params      *repository.ParametersRepo
	alloc       *repository.AllocationRepo
	holdings    *repository.HoldingsRepo
	overrides   *repository.ReturnOverrideRepo
	assumptions *repository.AssumptionProfileRepo
}

func NewConfigHashService(
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	holdings *repository.HoldingsRepo,
	overrides *repository.ReturnOverrideRepo,
	assumptionRepos ...*repository.AssumptionProfileRepo,
) *ConfigHashService {
	var assumptionRepo *repository.AssumptionProfileRepo
	if len(assumptionRepos) > 0 {
		assumptionRepo = assumptionRepos[0]
	} else {
		assumptionRepo = params.AssumptionProfiles()
	}
	return &ConfigHashService{
		plans: plans, params: params, alloc: alloc, holdings: holdings,
		overrides: overrides, assumptions: assumptionRepo,
	}
}

func (s *ConfigHashService) Compute(ctx context.Context, planID string) (string, error) {
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("load parameters: %w", err)
	}
	var identity *EffectiveAssumptionIdentity
	if params.ReturnAssumptionMode == repository.ModeBlendedPrior ||
		params.ReturnAssumptionMode == repository.ModeCustom {
		if s.assumptions == nil {
			return "", errAssumptionRepositoryNotConfigured
		}
		if err := s.assumptions.EnsureSystemDefault(ctx); err != nil {
			return "", fmt.Errorf("ensure system assumption profile: %w", err)
		}
		profile, scenario, contentHash, err := resolveProfileAndScenario(ctx, s.assumptions, params)
		if err != nil {
			return "", err
		}
		identity = &EffectiveAssumptionIdentity{
			ProfileID: profile.ID, ProfileVersion: profile.Version,
			ContentHash: contentHash, Scenario: scenario,
		}
	}
	return s.compute(ctx, planID, params, identity)
}

// ComputeWithIdentity hashes a plan using an already-resolved identity. Run
// creation passes the same identity to this method and the snapshot builder,
// preventing a global preference change between the two reads.
func (s *ConfigHashService) ComputeWithIdentity(
	ctx context.Context, planID string, identity *EffectiveAssumptionIdentity,
) (string, error) {
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("load parameters: %w", err)
	}
	return s.compute(ctx, planID, params, identity)
}

func (s *ConfigHashService) compute(
	ctx context.Context, planID string, params repository.PlanParameters,
	identity *EffectiveAssumptionIdentity,
) (string, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("load plan: %w", err)
	}
	alloc, err := s.alloc.Get(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("load allocation: %w", err)
	}
	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("list holdings: %w", err)
	}
	overrides, err := s.overrides.ListByPlan(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("list return overrides: %w", err)
	}

	parameterMap := parametersToMap(params)
	if identity != nil {
		parameterMap["effective_assumption_profile_id"] = identity.ProfileID
		parameterMap["effective_assumption_profile_version"] = identity.ProfileVersion
		parameterMap["effective_assumption_profile_content_hash"] = identity.ContentHash
		parameterMap["effective_assumption_scenario"] = identity.Scenario
	}
	in := domain.ConfigHashInput{
		PlanID:        planID,
		Name:          plan.Name,
		BaseCurrency:  plan.BaseCurrency,
		ValuationDate: plan.ValuationDate,
		Parameters:    parameterMap,
		AssetClass:    assetClassToMaps(alloc.AssetClassTargets),
		RegionTargets: regionToMaps(alloc.RegionTargets),
		Holdings:      holdingsToMaps(holds, overrides),
	}
	hash, err := domain.ComputeConfigHash(in)
	if err != nil {
		return "", fmt.Errorf("compute config hash: %w", err)
	}
	return hash, nil
}

func parametersToMap(p repository.PlanParameters) map[string]any {
	p = normalizeActiveParameters(p)
	m := map[string]any{
		"current_age":                          p.CurrentAge,
		"retirement_age":                       p.RetirementAge,
		"end_age":                              p.EndAge,
		"total_assets_minor":                   p.TotalAssetsMinor,
		"annual_savings_minor":                 p.AnnualSavingsMinor,
		"annual_savings_growth_rate":           p.AnnualSavingsGrowthRate,
		"annual_spending_minor":                p.AnnualSpendingMinor,
		"annual_retirement_income_minor":       p.AnnualRetirementIncomeMinor,
		"annual_retirement_income_growth_rate": p.AnnualRetirementIncomeGrowthRate,
		"terminal_wealth_floor_minor":          p.TerminalWealthFloorMinor,
		"inflation_mode":                       p.InflationMode,
		"withdrawal_type":                      p.WithdrawalType,
		"withdrawal_tax_rate":                  p.WithdrawalTaxRate,
		"taxable_withdrawal_ratio":             p.TaxableWithdrawalRatio,
		"rebalance_frequency":                  p.RebalanceFrequency,
		"rebalance_threshold":                  p.RebalanceThreshold,
		"transaction_cost_rate":                p.TransactionCostRate,
		"simulation_runs":                      p.SimulationRuns,
		// The return-assumption selection is part of the plan config, so
		// switching mode/profile/version/scenario marks existing runs stale and
		// changes the input hash (§6.1.6, §6.2.4).
		"return_assumption_mode":        p.ReturnAssumptionMode,
		"assumption_selection_mode":     p.AssumptionSelectionMode,
		"return_assumption_set_id":      p.ReturnAssumptionSetID,
		"return_assumption_set_version": p.ReturnAssumptionSetVersion,
		"return_assumption_scenario":    p.ReturnAssumptionScenario,
	}
	if p.InflationMode == "random_ar1" {
		m["inflation_mu"] = p.InflationMu
		m["inflation_phi"] = p.InflationPhi
		m["inflation_sigma"] = p.InflationSigma
	} else {
		m["fixed_inflation_rate"] = p.FixedInflationRate
	}
	switch p.WithdrawalType {
	case "fixed_portfolio":
		m["withdrawal_rate"] = p.WithdrawalRate
	case "guardrail":
		m["withdrawal_floor_ratio"] = p.WithdrawalFloorRatio
		m["withdrawal_ceiling_ratio"] = p.WithdrawalCeilingRatio
	}
	// student_t_df is a legacy 2.x-only field. Forward (blended_prior/custom) runs
	// freeze the global profile's df, so the plan value has no effect on a forward
	// run and must not be part of its config identity; including it would let an
	// irrelevant field mark forward runs stale. historical_cagr replay
	// configs still depend on the plan df, so they keep it in the hash.
	if p.ReturnAssumptionMode == "" || p.ReturnAssumptionMode == repository.ModeHistoricalCAGR {
		m["student_t_df"] = p.StudentTDf
	}
	if p.CustomReturnAssumptionsJSON != "" {
		m["custom_return_assumptions_json"] = p.CustomReturnAssumptionsJSON
	}
	if p.SelectedScenarioID != nil {
		m["selected_scenario_id"] = *p.SelectedScenarioID
	}
	if p.Seed != nil {
		m["seed"] = *p.Seed
	}
	return m
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

func holdingsToMaps(holds []repository.PlanHolding,
	overrides []repository.PlanReturnOverride,
) []map[string]any {
	byAsset := make(map[string]repository.PlanReturnOverride, len(overrides))
	for _, o := range overrides {
		byAsset[o.AssetKey] = o
	}
	out := make([]map[string]any, 0, len(holds))
	for _, h := range holds {
		m := map[string]any{
			"asset_key": h.AssetKey, "enabled": h.Enabled,
			"asset_class": h.AssetClass, "region": h.Region,
			"weight_within_group":    h.WeightWithinGroup,
			"current_amount_minor":   h.CurrentAmountMinor,
			"simulation_snapshot_id": h.SimulationSnapshotID,
			"sort_order":             h.SortOrder,
		}
		// Fold the asset-level override into the hash so editing it marks existing
		// runs stale and changes the input hash. Overrides for
		// assets not held don't affect simulation, so they're ignored here.
		if o, ok := byAsset[h.AssetKey]; ok {
			m["override_forward_return"] = o.ForwardReturn
			m["override_annual_volatility"] = o.AnnualVolatility
			m["override_reason"] = o.Reason
			m["override_expires_at"] = o.ExpiresAt
		}
		out = append(out, m)
	}
	return out
}
