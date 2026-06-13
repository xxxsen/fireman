package domain

import "math"

type newCashGapEntry struct {
	idx int
	gap int64
}

func buildNewCashRebalanceLines(
	targets []HoldingTargetLine,
	threshold float64,
) ([]RebalanceLine, []newCashGapEntry, int64, int64, int64) {
	lines := make([]RebalanceLine, 0, len(targets))
	var gaps []newCashGapEntry
	var gapSum, targetTotal, currentTotal int64

	for i, t := range targets {
		action := SuggestAction(t.Enabled, t.PlanGapWeight, t.PlanGapAmountMinor, threshold)
		if t.Enabled {
			targetTotal += t.TargetAmountMinor
			currentTotal += t.CurrentAmountMinor
			if action == RebalanceActionIncrease {
				gap := t.PlanGapAmountMinor
				if gap > 0 {
					gaps = append(gaps, newCashGapEntry{idx: i, gap: gap})
					gapSum += gap
				}
			}
		}
		lines = append(lines, RebalanceLine{
			HoldingTargetLine:            t,
			Action:                       action,
			SuggestedTradeMinor:          0,
			PlanScaleAction:              action,
			PlanScaleSuggestedTradeMinor: 0,
		})
	}
	return lines, gaps, gapSum, targetTotal, currentTotal
}

func distributeNewCashAllocations(lines []RebalanceLine, gaps []newCashGapEntry, gapSum, newCashMinor int64) {
	allocatable := newCashMinor
	if gapSum > 0 && allocatable > gapSum {
		allocatable = gapSum
	}
	if gapSum <= 0 || allocatable <= 0 {
		return
	}
	remaining := allocatable
	maxGapIdx := -1
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
	if remaining > 0 && maxGapIdx >= 0 {
		lines[maxGapIdx].SuggestedTradeMinor += remaining
	}
}
