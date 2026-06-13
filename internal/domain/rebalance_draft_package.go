package domain

import "math"

const packageDeltaToleranceMinor = 100

// PackageDeltaInput is one row for reference package computation.
type PackageDeltaInput struct {
	HoldingID          string
	StructuralGapMinor int64
}

// PackageDeltaResult maps holding_id to frozen recommended package delta.
type PackageDeltaResult struct {
	ByHoldingID map[string]int64
}

// ComputeReferencePackageDeltas derives closed package deltas from structural gaps.
// Each delta starts from structural_gap_amount_minor; when multiple non-zero lines exist,
// the result is adjusted to remain closed within tolerance.
func ComputeReferencePackageDeltas(inputs []PackageDeltaInput) PackageDeltaResult {
	out := make(map[string]int64, len(inputs))
	if len(inputs) == 0 {
		return PackageDeltaResult{ByHoldingID: out}
	}
	var sum int64
	maxAbs := int64(-1)
	var adjustID string
	nonZero := 0
	for _, in := range inputs {
		delta := in.StructuralGapMinor
		out[in.HoldingID] = delta
		sum += delta
		if delta != 0 {
			nonZero++
		}
		abs := delta
		if abs < 0 {
			abs = -abs
		}
		if abs > maxAbs {
			maxAbs = abs
			adjustID = in.HoldingID
		}
	}
	// Single non-zero line (e.g. only A over) keeps its delta; commit may cash-sweep the remainder.
	if nonZero > 1 && adjustID != "" && math.Abs(float64(sum)) > packageDeltaToleranceMinor {
		out[adjustID] -= sum
	}
	return PackageDeltaResult{ByHoldingID: out}
}

// RecommendedPlannedMinor returns baseline + frozen package delta.
func RecommendedPlannedMinor(baselineMinor, packageDeltaMinor int64) int64 {
	return baselineMinor + packageDeltaMinor
}
