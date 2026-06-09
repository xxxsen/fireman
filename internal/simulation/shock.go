package simulation

import "math"

// AssetShock applies per-asset stress overlays for one month.
type AssetShock struct {
	// ReturnMul is a compound multiplicative shock on the sampled simple return:
	// stressed = (1+sampled)*(1+ReturnMul)-1.
	ReturnMul float64
	// DriftDelta is an annual return delta (decimal) applied before sampling.
	DriftDelta float64
	// FXReturnMul compounds on the FX simple return component.
	FXReturnMul float64
}

// MonthShock holds all overlays for a single month.
type MonthShock struct {
	Assets             map[int]AssetShock
	InflationAnnual    *float64
	ExtraSpendingMinor int64
	SpendingMultiplier float64
}

// ShockSchedule maps month offset to overlays. Nil means no stress.
type ShockSchedule map[int]MonthShock

// AnnualToMonthlyCompound converts an annual compound shock to a monthly rate.
func AnnualToMonthlyCompound(annualShock float64) float64 {
	return math.Pow(1+annualShock, 1.0/12) - 1
}

// DrawdownToMonthlyShock converts a max drawdown to an equivalent monthly compound shock.
func DrawdownToMonthlyShock(maxDrawdown float64) float64 {
	if maxDrawdown <= 0 {
		return 0
	}
	if maxDrawdown >= 1 {
		maxDrawdown = 0.99
	}
	return math.Pow(1-maxDrawdown, 1.0/12) - 1
}
