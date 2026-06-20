package simulation

import (
	"math"
)

const (
	ReturnFloor = -0.95
	ReturnCeil  = 2.0
)

// TailTruncation bounds a single simulated monthly simple return. It is frozen
// per run from the active profile (3.0.0) or from the legacy constants (2.x), so
// the truncation is auditable and a plan can no longer change it (td/063 R3).
type TailTruncation struct {
	Floor float64
	Ceil  float64
}

// LegacyTailTruncation is the pre-td/063 hard-coded band kept for 2.x replay and
// for any snapshot that did not freeze profile bounds.
func LegacyTailTruncation() TailTruncation {
	return TailTruncation{Floor: ReturnFloor, Ceil: ReturnCeil}
}

// clamp truncates a simple return to the band and reports whether it was clamped.
func (t TailTruncation) clamp(simple float64) (float64, bool) {
	switch {
	case simple < t.Floor:
		return t.Floor, true
	case simple > t.Ceil:
		return t.Ceil, true
	default:
		return simple, false
	}
}

// AssetReturnParams holds monthly Student-t parameters for one asset.
type AssetReturnParams struct {
	MonthlyMu    float64
	MonthlySigma float64
}

// ParamsFromAnnual derives monthly log-return parameters from the forward annual
// GEOMETRIC return and annual volatility. The monthly log drift is
// ln(1+annualGeometricReturn)/12, so 12 months of unperturbed compounding
// reproduce the annual geometric return exactly (no arithmetic-return mixing).
func ParamsFromAnnual(annualGeometricReturn, annualVolatility float64) AssetReturnParams {
	annualLogMean := math.Log(1 + annualGeometricReturn)
	return AssetReturnParams{
		MonthlyMu:    annualLogMean / 12,
		MonthlySigma: annualVolatility / math.Sqrt(12),
	}
}

// SampleStudentT draws one monthly simple return from an independent Student-t
// factor and truncates it to the frozen tail band (td/063 R3).
func SampleStudentT(rng *RNG, p AssetReturnParams, df int, trunc TailTruncation) (float64, bool) {
	if p.MonthlySigma == 0 {
		return math.Exp(p.MonthlyMu) - 1, false
	}
	z := rng.NormFloat64()
	u := chiSquare(rng, df)
	scale := math.Sqrt(float64(df-2) / u)
	logRet := p.MonthlyMu + p.MonthlySigma*z*scale
	return trunc.clamp(math.Exp(logRet) - 1)
}

func chiSquare(rng *RNG, df int) float64 {
	// Sum of df squared standard normals.
	sum := 0.0
	for i := 0; i < df; i++ {
		z := rng.NormFloat64()
		sum += z * z
	}
	if sum <= 0 {
		return 1
	}
	return sum
}

// CompositeBaseReturn combines local asset and FX simple returns.
func CompositeBaseReturn(assetLocal, fxReturn float64) float64 {
	return (1+assetLocal)*(1+fxReturn) - 1
}
