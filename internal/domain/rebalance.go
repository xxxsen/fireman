package domain

import "math"

// RebalanceLine is one row in a rebalance report.
type RebalanceLine struct {
	HoldingTargetLine
	Action                       string `json:"action"`
	SuggestedTradeMinor          int64  `json:"suggested_trade_minor"`
	PlanScaleAction              string `json:"plan_scale_action"`
	PlanScaleSuggestedTradeMinor int64  `json:"plan_scale_suggested_trade_minor"`
}

// RebalanceSummary aggregates rebalance KPIs.
type RebalanceSummary struct {
	TotalAssetsMinor          int64 `json:"total_assets_minor"`
	ConfiguredTotalMinor      int64 `json:"configured_total_minor"`
	HoldingsTotalMinor        int64 `json:"holdings_total_minor"`
	ScaleGapMinor             int64 `json:"scale_gap_minor"`
	TargetTotalMinor          int64 `json:"target_total_minor"`
	CurrentTotalMinor         int64 `json:"current_total_minor"`
	ActionableCount           int   `json:"actionable_count"`
	StructuralActionableCount int   `json:"structural_actionable_count"`
	PlanScaleActionableCount  int   `json:"plan_scale_actionable_count"`
	EstimatedTradeMinor       int64 `json:"estimated_trade_minor"`
	EstimatedCostMinor        int64 `json:"estimated_cost_minor"`
}

// RebalanceResult is the full rebalance output.
type RebalanceResult struct {
	Mode    string                 `json:"mode"`
	Summary RebalanceSummary       `json:"summary"`
	Lines   []RebalanceLine        `json:"lines"`
	Checks  WeightValidationResult `json:"weight_checks"`
}

// ComputeFullRebalance sells overweight holdings and buys underweight holdings.
func ComputeFullRebalance(
	alloc AllocationWeights,
	holdings []HoldingWeightInput,
	meta []struct {
		ID, AssetKey, SimulationSnapshotID string
		SortOrder                              int
	},
	totalAssetsMinor int64,
	threshold float64,
	transactionCostRate float64,
) RebalanceResult {
	targets := ComputeHoldingTargets(alloc, holdings, meta, totalAssetsMinor)
	holdingsTotal := HoldingsTotalMinor(holdings)
	lines := make([]RebalanceLine, 0, len(targets))

	var targetTotal, currentTotal int64
	var estimatedTrade int64
	structuralActionable := 0
	planScaleActionable := 0

	for _, t := range targets {
		structuralAction := SuggestAction(t.Enabled, t.StructuralGapWeight, t.StructuralGapAmountMinor, threshold)
		planScaleAction := SuggestAction(t.Enabled, t.PlanGapWeight, t.PlanGapAmountMinor, threshold)
		structuralTrade := int64(0)
		planScaleTrade := int64(0)
		if t.Enabled {
			targetTotal += t.StructuralTargetAmountMinor
			currentTotal += t.CurrentAmountMinor
			if structuralAction == RebalanceActionIncrease || structuralAction == RebalanceActionDecrease {
				structuralTrade = t.StructuralGapAmountMinor
				structuralActionable++
			}
			if planScaleAction == RebalanceActionIncrease || planScaleAction == RebalanceActionDecrease {
				planScaleTrade = t.PlanGapAmountMinor
				planScaleActionable++
			}
			estimatedTrade += int64(math.Abs(float64(structuralTrade)))
		}
		lines = append(lines, RebalanceLine{
			HoldingTargetLine:            t,
			Action:                       structuralAction,
			SuggestedTradeMinor:          structuralTrade,
			PlanScaleAction:              planScaleAction,
			PlanScaleSuggestedTradeMinor: planScaleTrade,
		})
	}

	cost := int64(math.Round(float64(estimatedTrade) * transactionCostRate))

	return RebalanceResult{
		Mode: RebalanceModeFull,
		Summary: RebalanceSummary{
			TotalAssetsMinor:          totalAssetsMinor,
			ConfiguredTotalMinor:      totalAssetsMinor,
			HoldingsTotalMinor:        holdingsTotal,
			ScaleGapMinor:             ScaleGapMinor(holdingsTotal, totalAssetsMinor),
			TargetTotalMinor:          targetTotal,
			CurrentTotalMinor:         currentTotal,
			ActionableCount:           structuralActionable,
			StructuralActionableCount: structuralActionable,
			PlanScaleActionableCount:  planScaleActionable,
			EstimatedTradeMinor:       estimatedTrade,
			EstimatedCostMinor:        cost,
		},
		Lines:  lines,
		Checks: ValidateAllWeights(alloc, holdings),
	}
}

// ComputeNewCashRebalance allocates new cash only to underweight holdings.
func ComputeNewCashRebalance(
	alloc AllocationWeights,
	holdings []HoldingWeightInput,
	meta []struct {
		ID, AssetKey, SimulationSnapshotID string
		SortOrder                              int
	},
	totalAssetsMinor int64,
	newCashMinor int64,
	threshold float64,
	transactionCostRate float64,
) RebalanceResult {
	targets := ComputeHoldingTargets(alloc, holdings, meta, totalAssetsMinor)
	holdingsTotal := HoldingsTotalMinor(holdings)
	lines, gaps, gapSum, targetTotal, currentTotal := buildNewCashRebalanceLines(targets, threshold)
	distributeNewCashAllocations(lines, gaps, gapSum, newCashMinor)

	var estimatedTrade int64
	planScaleActionable := 0
	for i := range lines {
		if lines[i].Enabled && lines[i].SuggestedTradeMinor > 0 {
			planScaleActionable++
			estimatedTrade += lines[i].SuggestedTradeMinor
		}
	}
	cost := int64(math.Round(float64(estimatedTrade) * transactionCostRate))

	return RebalanceResult{
		Mode: RebalanceModeNewCash,
		Summary: RebalanceSummary{
			TotalAssetsMinor:          totalAssetsMinor,
			ConfiguredTotalMinor:      totalAssetsMinor,
			HoldingsTotalMinor:        holdingsTotal,
			ScaleGapMinor:             ScaleGapMinor(holdingsTotal, totalAssetsMinor),
			TargetTotalMinor:          targetTotal,
			CurrentTotalMinor:         currentTotal,
			ActionableCount:           planScaleActionable,
			StructuralActionableCount: 0,
			PlanScaleActionableCount:  planScaleActionable,
			EstimatedTradeMinor:       estimatedTrade,
			EstimatedCostMinor:        cost,
		},
		Lines:  lines,
		Checks: ValidateAllWeights(alloc, holdings),
	}
}
