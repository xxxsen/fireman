package domain

// FrozenDraftLine captures immutable baseline fields for one holding in a rebalance plan draft.
type FrozenDraftLine struct {
	HoldingID                    string  `json:"holding_id"`
	InstrumentID                 string  `json:"instrument_id"`
	BaselineCurrentMinor         int64   `json:"baseline_current_minor"`
	PlannedCurrentMinor          int64   `json:"planned_current_minor"`
	FrozenTargetMinor            int64   `json:"frozen_target_minor"`
	FrozenGapMinor               int64   `json:"frozen_gap_minor"`
	FrozenGapWeight              float64 `json:"frozen_gap_weight"`
	FrozenAction                 string  `json:"frozen_action"`
	FrozenSuggestedTradeMinor    int64   `json:"frozen_suggested_trade_minor"`
	RecommendedPackageDeltaMinor int64   `json:"recommended_package_delta_minor"`
}

// BuildFrozenDraftLines derives draft lines from a full rebalance result at creation time.
func BuildFrozenDraftLines(result RebalanceResult) []FrozenDraftLine {
	inputs := make([]PackageDeltaInput, 0, len(result.Lines))
	for _, line := range result.Lines {
		if !line.Enabled {
			continue
		}
		inputs = append(inputs, PackageDeltaInput{
			HoldingID:          line.HoldingID,
			StructuralGapMinor: line.StructuralGapAmountMinor,
		})
	}
	packageDeltas := ComputeReferencePackageDeltas(inputs)

	lines := make([]FrozenDraftLine, 0, len(result.Lines))
	for _, line := range result.Lines {
		if !line.Enabled {
			continue
		}
		lines = append(lines, FrozenDraftLine{
			HoldingID:                    line.HoldingID,
			InstrumentID:                 line.InstrumentID,
			BaselineCurrentMinor:         line.CurrentAmountMinor,
			PlannedCurrentMinor:          line.CurrentAmountMinor,
			FrozenTargetMinor:            line.StructuralTargetAmountMinor,
			FrozenGapMinor:               line.StructuralGapAmountMinor,
			FrozenGapWeight:              line.StructuralGapWeight,
			FrozenAction:                 line.Action,
			FrozenSuggestedTradeMinor:    line.SuggestedTradeMinor,
			RecommendedPackageDeltaMinor: packageDeltas.ByHoldingID[line.HoldingID],
		})
	}
	return lines
}

// DraftFundPool summarizes released vs used capital within a draft.
type DraftFundPool struct {
	ReleasedMinor int64 `json:"released_minor"`
	UsedMinor     int64 `json:"used_minor"`
	NetMinor      int64 `json:"net_minor"`
}

// ComputeDraftFundPool calculates fund pool from baseline vs planned amounts.
func ComputeDraftFundPool(lines []FrozenDraftLine) DraftFundPool {
	var released, used int64
	for _, line := range lines {
		delta := line.PlannedCurrentMinor - line.BaselineCurrentMinor
		if delta < 0 {
			released += -delta
		} else if delta > 0 {
			used += delta
		}
	}
	return DraftFundPool{ReleasedMinor: released, UsedMinor: used, NetMinor: released - used}
}
