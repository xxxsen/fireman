package assumptions

// SystemProfileID is the immutable id of the read-only system default profile.
const SystemProfileID = "system_cma_v1"

// SystemProfileVersion is the active system profile version.
const SystemProfileVersion = 1

// System profile review metadata (td/063 N1). These are named, sourced and dated
// so the active default is auditable rather than an anonymous seed. They are
// replaced (with a new version) whenever the underlying CMA inputs change.
const (
	SystemProfileSourceNote = "Internal CMA seed v1: conservative after-fee CNY nominal " +
		"geometric priors compiled from public long-run capital-market assumptions. " +
		"Pending replacement by an externally sourced CMA pack."
	SystemProfileReviewedBy = "FIRE 投研团队 (CMA Seed Review)"
	SystemProfileReviewedAt = "2026-06-20"
)

// seedSource documents that these are versioned seed values awaiting a named
// reviewer's sign-off (td/061 §3.2). They are intentionally conservative,
// after-fee, CNY nominal geometric priors and are NOT reverse-engineered from any
// target success rate. They live as data (not as logic constants scattered across
// the code), so a future review only replaces this single source of truth.
const (
	seedSourceURL   = "https://github.com/fireman/fireman/tree/master/td#td-061"
	seedPublishedAt = "2026-06-20"
	seedReviewedAt  = "2026-06-20"
)

// SystemDefaultProfile returns the read-only system assumption profile used when
// a user has not configured a global profile. It must always pass Validate.
func SystemDefaultProfile() Profile {
	return Profile{
		ID:                        SystemProfileID,
		Version:                   SystemProfileVersion,
		OwnerScope:                OwnerSystem,
		Name:                      "系统默认（CMA v1）",
		Status:                    StatusActive,
		PriorStrengthYears:        20,
		CorrelationStrengthMonths: 36,
		StudentTDf:                7,
		// Per-month simple-return truncation band, owned by the profile so the tail
		// clamp is versioned and frozen into each run (td/063 R3). These match the
		// historical engine constants so forward runs keep the same bound.
		ReturnFloor: -0.95,
		ReturnCeil:  2.0,
		Scenarios: map[string]Scenario{
			ScenarioConservative: {ReturnShiftLog: -0.015, ReturnShiftLogFX: 0, VolatilityMultiplier: 1.15},
			ScenarioBaseline:     {ReturnShiftLog: 0, ReturnShiftLogFX: 0, VolatilityMultiplier: 1.00},
			ScenarioOptimistic:   {ReturnShiftLog: 0.015, ReturnShiftLogFX: 0, VolatilityMultiplier: 0.90},
		},
		ReturnPriors: []ReturnPrior{
			seedReturnPrior("equity", "domestic", "CNY", 0.060, 0.12, 0.35),
			seedReturnPrior("equity", "foreign", "CNY", 0.065, 0.12, 0.40),
			seedReturnPrior("bond", "domestic", "CNY", 0.030, 0.02, 0.10),
			seedReturnPrior("bond", "foreign", "CNY", 0.030, 0.03, 0.12),
			seedReturnPrior("cash", "domestic", "CNY", 0.018, 0.00, 0.00),
			// Native-currency foreign priors so an asset priced in its own currency
			// calibrates its local return without an FX/currency mismatch; the FX
			// priors below carry the currency view separately (td/063 R2). These
			// share the equity:foreign / bond:foreign factor with the CNY cells.
			seedReturnPrior("equity", "foreign", "USD", 0.065, 0.12, 0.40),
			seedReturnPrior("bond", "foreign", "USD", 0.030, 0.03, 0.12),
			seedReturnPrior("equity", "foreign", "HKD", 0.065, 0.12, 0.40),
			seedReturnPrior("bond", "foreign", "HKD", 0.030, 0.03, 0.12),
		},
		FXPriors: []FXPrior{
			seedFXPrior("USD", 0.03, 0.12),
			// HKD is pegged to USD, so its CNY drift and volatility track USD/CNY.
			seedFXPrior("HKD", 0.03, 0.12),
		},
		CorrelationPriors: systemCorrelationPriors(),
	}
}

func seedFXPrior(from string, volFloor, volCeil float64) FXPrior {
	return FXPrior{
		FromCurrency: from, BaseCurrency: "CNY",
		AnnualGeometricReturn:   0.0,
		AnnualVolatilityFloor:   volFloor,
		AnnualVolatilityCeiling: volCeil,
		SourceURL:               seedSourceURL,
		PublishedAt:             seedPublishedAt,
		ReviewedAt:              seedReviewedAt,
	}
}

func seedReturnPrior(assetClass, region, currency string, ret, volFloor, volCeil float64) ReturnPrior {
	return ReturnPrior{
		AssetClass: assetClass, Region: region, ValuationCurrency: currency,
		AnnualGeometricReturn:   ret,
		AnnualVolatilityFloor:   volFloor,
		AnnualVolatilityCeiling: volCeil,
		SourceURL:               seedSourceURL,
		PublishedAt:             seedPublishedAt,
		ReviewedAt:              seedReviewedAt,
	}
}

// systemCorrelationPriors returns one prior for every distinct pair of random
// factors (deterministic cash is excluded from the universe, so it carries no
// correlation prior). The six factors — domestic/foreign equity, domestic/foreign
// bond and the USD/CNY and HKD/CNY FX factors — yield the full set of C(6,2)=15
// pairs required by Profile.Validate (td/063 R4). HKD is USD-pegged, so its FX
// factor mirrors USD/CNY's correlations and is near-perfectly correlated with it.
func systemCorrelationPriors() []CorrelationPrior {
	eqD := AssetFactorKey("equity", "domestic")
	eqF := AssetFactorKey("equity", "foreign")
	bdD := AssetFactorKey("bond", "domestic")
	bdF := AssetFactorKey("bond", "foreign")
	fxUSD := FXFactorKey("USD", "CNY")
	fxHKD := FXFactorKey("HKD", "CNY")
	return []CorrelationPrior{
		{FactorA: eqD, FactorB: eqF, Rho: 0.60},
		{FactorA: eqD, FactorB: bdD, Rho: 0.15},
		{FactorA: eqD, FactorB: bdF, Rho: 0.10},
		{FactorA: eqF, FactorB: bdD, Rho: 0.10},
		{FactorA: eqF, FactorB: bdF, Rho: 0.20},
		{FactorA: bdD, FactorB: bdF, Rho: 0.40},
		{FactorA: eqF, FactorB: fxUSD, Rho: 0.15},
		{FactorA: eqD, FactorB: fxUSD, Rho: 0},
		{FactorA: bdD, FactorB: fxUSD, Rho: 0},
		{FactorA: bdF, FactorB: fxUSD, Rho: 0},
		{FactorA: eqF, FactorB: fxHKD, Rho: 0.15},
		{FactorA: eqD, FactorB: fxHKD, Rho: 0},
		{FactorA: bdD, FactorB: fxHKD, Rho: 0},
		{FactorA: bdF, FactorB: fxHKD, Rho: 0},
		{FactorA: fxUSD, FactorB: fxHKD, Rho: 0.95},
	}
}
