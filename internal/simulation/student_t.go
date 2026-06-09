package simulation

import (
	"math"
)

const (
	ReturnFloor = -0.95
	ReturnCeil  = 2.0
)

// AssetReturnParams holds monthly Student-t parameters for one asset.
type AssetReturnParams struct {
	MonthlyMu    float64
	MonthlySigma float64
}

// ParamsFromAnnual derives monthly log-return parameters from annual metrics.
func ParamsFromAnnual(modeledAnnualReturn, annualVolatility float64) AssetReturnParams {
	annualLogMean := math.Log(1 + modeledAnnualReturn)
	return AssetReturnParams{
		MonthlyMu:    annualLogMean / 12,
		MonthlySigma: annualVolatility / math.Sqrt(12),
	}
}

// SampleStudentT draws one monthly simple return from an independent Student-t factor.
func SampleStudentT(rng *RNG, p AssetReturnParams, df int) (ret float64, truncated bool) {
	if p.MonthlySigma == 0 {
		return math.Exp(p.MonthlyMu) - 1, false
	}
	z := rng.NormFloat64()
	u := chiSquare(rng, df)
	scale := math.Sqrt(float64(df-2) / u)
	logRet := p.MonthlyMu + p.MonthlySigma*z*scale
	simple := math.Exp(logRet) - 1
	if simple < ReturnFloor {
		return ReturnFloor, true
	}
	if simple > ReturnCeil {
		return ReturnCeil, true
	}
	return simple, false
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
