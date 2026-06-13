package domain

import "math"

// HoldingWeightInput carries the fields needed to compute portfolio-level weights.
type HoldingWeightInput struct {
	AssetClass         string
	Region             string
	Enabled            bool
	WeightWithinGroup  float64
	CurrentAmountMinor int64
}

// AllocationWeights holds plan-level asset class and region targets.
type AllocationWeights struct {
	AssetClass map[string]float64            // asset_class -> weight
	Region     map[string]map[string]float64 // asset_class -> region -> weight_within_class
}

// PortfolioTargetWeight returns the full-portfolio target weight for a holding.
func PortfolioTargetWeight(alloc AllocationWeights, h HoldingWeightInput) float64 {
	if !h.Enabled {
		return 0
	}
	acW := alloc.AssetClass[h.AssetClass]
	regW := 0.0
	if m, ok := alloc.Region[h.AssetClass]; ok {
		regW = m[h.Region]
	}
	return acW * regW * h.WeightWithinGroup
}

// CurrentWeight returns current_amount / total_assets (plan-scale denominator).
func CurrentWeight(currentAmountMinor, totalAssetsMinor int64) float64 {
	if totalAssetsMinor <= 0 {
		return 0
	}
	return float64(currentAmountMinor) / float64(totalAssetsMinor)
}

// HoldingsTotalMinor sums enabled holdings' current amounts.
func HoldingsTotalMinor(holdings []HoldingWeightInput) int64 {
	var sum int64
	for _, h := range holdings {
		if h.Enabled {
			sum += h.CurrentAmountMinor
		}
	}
	return sum
}

// StructuralCurrentWeight returns current_amount / holdings_total.
func StructuralCurrentWeight(currentAmountMinor, holdingsTotalMinor int64) float64 {
	if holdingsTotalMinor <= 0 {
		return 0
	}
	return float64(currentAmountMinor) / float64(holdingsTotalMinor)
}

// StructuralGapAmountMinor rounds holdings_total * structural_gap_weight.
func StructuralGapAmountMinor(holdingsTotalMinor int64, structuralGapWeight float64) int64 {
	return int64(math.Round(float64(holdingsTotalMinor) * structuralGapWeight))
}

// ScaleGapMinor returns holdings_total - configured_total.
func ScaleGapMinor(holdingsTotalMinor, configuredTotalMinor int64) int64 {
	return holdingsTotalMinor - configuredTotalMinor
}

// TargetAmountMinor returns total_assets * portfolio_target_weight.
func TargetAmountMinor(totalAssetsMinor int64, targetWeight float64) int64 {
	return int64(math.Round(float64(totalAssetsMinor) * targetWeight))
}

// DeviationAmountMinor returns target - current.
func DeviationAmountMinor(targetAmountMinor, currentAmountMinor int64) int64 {
	return targetAmountMinor - currentAmountMinor
}

// DeviationWeight returns target_weight - current_weight.
func DeviationWeight(targetWeight, currentWeight float64) float64 {
	return targetWeight - currentWeight
}

// SuggestAction determines whether a holding should be increased, reduced, or unchanged.
func SuggestAction(enabled bool, deviationWeight float64, deviationAmountMinor int64, threshold float64) string {
	if !enabled {
		return RebalanceActionDisabled
	}
	if math.Abs(deviationWeight) < threshold {
		return RebalanceActionHold
	}
	if deviationAmountMinor > 0 {
		return RebalanceActionIncrease
	}
	if deviationAmountMinor < 0 {
		return RebalanceActionDecrease
	}
	return RebalanceActionHold
}

// HoldingTargetLine is a computed target line for one holding.
type HoldingTargetLine struct {
	HoldingID                   string  `json:"holding_id"`
	InstrumentID                string  `json:"instrument_id"`
	InstrumentName              string  `json:"instrument_name,omitempty"`
	InstrumentCode              string  `json:"instrument_code,omitempty"`
	AssetClass                  string  `json:"asset_class"`
	Region                      string  `json:"region"`
	Enabled                     bool    `json:"enabled"`
	AssetClassWeight            float64 `json:"asset_class_weight"`
	RegionWeight                float64 `json:"region_weight"`
	WeightWithinGroup           float64 `json:"weight_within_group"`
	PortfolioTargetWeight       float64 `json:"portfolio_target_weight"`
	TargetAmountMinor           int64   `json:"target_amount_minor"`
	CurrentAmountMinor          int64   `json:"current_amount_minor"`
	CurrentWeight               float64 `json:"current_weight"`
	DeviationAmountMinor        int64   `json:"deviation_amount_minor"`
	DeviationWeight             float64 `json:"deviation_weight"`
	StructuralCurrentWeight     float64 `json:"structural_current_weight"`
	StructuralGapWeight         float64 `json:"structural_gap_weight"`
	StructuralGapAmountMinor    int64   `json:"structural_gap_amount_minor"`
	StructuralTargetAmountMinor int64   `json:"structural_target_amount_minor"`
	PlanGapWeight               float64 `json:"plan_gap_weight"`
	PlanGapAmountMinor          int64   `json:"plan_gap_amount_minor"`
	SimulationSnapshotID        string  `json:"simulation_snapshot_id"`
	SortOrder                   int     `json:"sort_order"`
}

// ComputeHoldingTargets expands all holdings into target lines with structural and plan-scale deviations.
func ComputeHoldingTargets(alloc AllocationWeights, holdings []HoldingWeightInput, holdingMeta []struct {
	ID, InstrumentID, SimulationSnapshotID string
	SortOrder                              int
}, totalAssetsMinor int64,
) []HoldingTargetLine {
	holdingsTotal := HoldingsTotalMinor(holdings)
	lines := make([]HoldingTargetLine, 0, len(holdings))
	for i, h := range holdings {
		ptw := PortfolioTargetWeight(alloc, h)
		planCW := CurrentWeight(h.CurrentAmountMinor, totalAssetsMinor)
		structuralCW := StructuralCurrentWeight(h.CurrentAmountMinor, holdingsTotal)
		planTAM := TargetAmountMinor(totalAssetsMinor, ptw)
		structuralTAM := TargetAmountMinor(holdingsTotal, ptw)
		planGapAmount := DeviationAmountMinor(planTAM, h.CurrentAmountMinor)
		planGapWeight := DeviationWeight(ptw, planCW)
		structuralGapWeight := DeviationWeight(ptw, structuralCW)
		structuralGapAmount := StructuralGapAmountMinor(holdingsTotal, structuralGapWeight)

		regW := 0.0
		if m, ok := alloc.Region[h.AssetClass]; ok {
			regW = m[h.Region]
		}

		line := HoldingTargetLine{
			AssetClass:                  h.AssetClass,
			Region:                      h.Region,
			Enabled:                     h.Enabled,
			AssetClassWeight:            alloc.AssetClass[h.AssetClass],
			RegionWeight:                regW,
			WeightWithinGroup:           h.WeightWithinGroup,
			PortfolioTargetWeight:       ptw,
			TargetAmountMinor:           planTAM,
			CurrentAmountMinor:          h.CurrentAmountMinor,
			CurrentWeight:               planCW,
			DeviationAmountMinor:        planGapAmount,
			DeviationWeight:             planGapWeight,
			StructuralCurrentWeight:     structuralCW,
			StructuralGapWeight:         structuralGapWeight,
			StructuralGapAmountMinor:    structuralGapAmount,
			StructuralTargetAmountMinor: structuralTAM,
			PlanGapWeight:               planGapWeight,
			PlanGapAmountMinor:          planGapAmount,
		}
		if i < len(holdingMeta) {
			line.HoldingID = holdingMeta[i].ID
			line.InstrumentID = holdingMeta[i].InstrumentID
			line.SimulationSnapshotID = holdingMeta[i].SimulationSnapshotID
			line.SortOrder = holdingMeta[i].SortOrder
		}
		lines = append(lines, line)
	}
	return lines
}
