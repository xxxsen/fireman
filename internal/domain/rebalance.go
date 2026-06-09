package domain

import "math"

// RebalanceLine is one row in a rebalance report.
type RebalanceLine struct {
	HoldingTargetLine
	Action              string `json:"action"`
	SuggestedTradeMinor int64  `json:"suggested_trade_minor"`
}

// RebalanceSummary aggregates rebalance KPIs.
type RebalanceSummary struct {
	TotalAssetsMinor    int64 `json:"total_assets_minor"`
	TargetTotalMinor    int64 `json:"target_total_minor"`
	CurrentTotalMinor   int64 `json:"current_total_minor"`
	ActionableCount     int   `json:"actionable_count"`
	EstimatedTradeMinor int64 `json:"estimated_trade_minor"`
	EstimatedCostMinor  int64 `json:"estimated_cost_minor"`
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
		ID, InstrumentID, SimulationSnapshotID string
		SortOrder                              int
	},
	totalAssetsMinor int64,
	threshold float64,
	transactionCostRate float64,
) RebalanceResult {
	targets := ComputeHoldingTargets(alloc, holdings, meta, totalAssetsMinor)
	lines := make([]RebalanceLine, 0, len(targets))

	var targetTotal, currentTotal int64
	var estimatedTrade int64
	actionable := 0

	for _, t := range targets {
		action := SuggestAction(t.Enabled, t.DeviationWeight, t.DeviationAmountMinor, threshold)
		trade := int64(0)
		if t.Enabled {
			targetTotal += t.TargetAmountMinor
			currentTotal += t.CurrentAmountMinor
			if action == RebalanceActionIncrease || action == RebalanceActionDecrease {
				trade = t.DeviationAmountMinor
				actionable++
			}
			estimatedTrade += int64(math.Abs(float64(trade)))
		}
		lines = append(lines, RebalanceLine{
			HoldingTargetLine:   t,
			Action:              action,
			SuggestedTradeMinor: trade,
		})
	}

	cost := int64(math.Round(float64(estimatedTrade) * transactionCostRate))

	return RebalanceResult{
		Mode: RebalanceModeFull,
		Summary: RebalanceSummary{
			TotalAssetsMinor:    totalAssetsMinor,
			TargetTotalMinor:    targetTotal,
			CurrentTotalMinor:   currentTotal,
			ActionableCount:     actionable,
			EstimatedTradeMinor: estimatedTrade,
			EstimatedCostMinor:  cost,
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
		ID, InstrumentID, SimulationSnapshotID string
		SortOrder                              int
	},
	totalAssetsMinor int64,
	newCashMinor int64,
	threshold float64,
	transactionCostRate float64,
) RebalanceResult {
	targets := ComputeHoldingTargets(alloc, holdings, meta, totalAssetsMinor)
	lines := make([]RebalanceLine, 0, len(targets))

	type gapEntry struct {
		idx int
		gap int64
	}
	var gaps []gapEntry
	var gapSum int64
	var targetTotal, currentTotal int64

	for i, t := range targets {
		action := SuggestAction(t.Enabled, t.DeviationWeight, t.DeviationAmountMinor, threshold)
		trade := int64(0)
		if t.Enabled {
			targetTotal += t.TargetAmountMinor
			currentTotal += t.CurrentAmountMinor
			if action == RebalanceActionIncrease {
				gap := t.DeviationAmountMinor
				if gap > 0 {
					gaps = append(gaps, gapEntry{idx: i, gap: gap})
					gapSum += gap
				}
			}
		}
		lines = append(lines, RebalanceLine{
			HoldingTargetLine:   t,
			Action:              action,
			SuggestedTradeMinor: trade,
		})
	}

	allocatable := newCashMinor
	if gapSum > 0 && int64(allocatable) > gapSum {
		allocatable = gapSum
	}

	if gapSum > 0 && allocatable > 0 {
		remaining := allocatable
		var maxGapIdx = -1
		var maxGap int64
		for _, g := range gaps {
			buy := int64(math.Round(float64(allocatable) * float64(g.gap) / float64(gapSum)))
			if buy > remaining {
				buy = remaining
			}
			lines[g.idx].SuggestedTradeMinor = buy
			remaining -= buy
			if g.gap > maxGap {
				maxGap = g.gap
				maxGapIdx = g.idx
			}
		}
		// Assign rounding remainder to largest gap.
		if remaining > 0 && maxGapIdx >= 0 {
			lines[maxGapIdx].SuggestedTradeMinor += remaining
		}
	}

	var estimatedTrade int64
	actionable := 0
	for i := range lines {
		if lines[i].Enabled && lines[i].SuggestedTradeMinor > 0 {
			actionable++
			estimatedTrade += lines[i].SuggestedTradeMinor
		}
	}
	cost := int64(math.Round(float64(estimatedTrade) * transactionCostRate))

	return RebalanceResult{
		Mode: RebalanceModeNewCash,
		Summary: RebalanceSummary{
			TotalAssetsMinor:    totalAssetsMinor,
			TargetTotalMinor:    targetTotal,
			CurrentTotalMinor:   currentTotal,
			ActionableCount:     actionable,
			EstimatedTradeMinor: estimatedTrade,
			EstimatedCostMinor:  cost,
		},
		Lines:  lines,
		Checks: ValidateAllWeights(alloc, holdings),
	}
}
