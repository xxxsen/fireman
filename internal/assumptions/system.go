package assumptions

// SystemProfileID is the immutable id of the read-only system default profile.
const SystemProfileID = "system_cma_v1"

// SystemProfileVersion is the active system profile version.
const SystemProfileVersion = 1

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
		Scenarios: map[string]Scenario{
			ScenarioConservative: {ReturnShiftLog: -0.015, ReturnShiftLogFX: 0, VolatilityMultiplier: 1.15},
			ScenarioBaseline:     {ReturnShiftLog: 0, ReturnShiftLogFX: 0, VolatilityMultiplier: 1.00},
			ScenarioOptimistic:   {ReturnShiftLog: 0.015, ReturnShiftLogFX: 0, VolatilityMultiplier: 0.90},
		},
		ReturnPriors: []ReturnPrior{
			seedReturnPrior("equity", "domestic", 0.060, 0.12, 0.35),
			seedReturnPrior("equity", "foreign", 0.065, 0.12, 0.40),
			seedReturnPrior("bond", "domestic", 0.030, 0.02, 0.10),
			seedReturnPrior("bond", "foreign", 0.030, 0.03, 0.12),
			seedReturnPrior("cash", "domestic", 0.018, 0.00, 0.00),
		},
		FXPriors: []FXPrior{
			{
				FromCurrency: "USD", BaseCurrency: "CNY",
				AnnualGeometricReturn: 0.0, AnnualVolatilityFloor: 0.03, AnnualVolatilityCeiling: 0.12,
				SourceURL: seedSourceURL, PublishedAt: seedPublishedAt, ReviewedAt: seedReviewedAt,
			},
		},
		CorrelationPriors: systemCorrelationPriors(),
	}
}

func seedReturnPrior(assetClass, region string, ret, volFloor, volCeil float64) ReturnPrior {
	return ReturnPrior{
		AssetClass: assetClass, Region: region, ValuationCurrency: "CNY",
		AnnualGeometricReturn:   ret,
		AnnualVolatilityFloor:   volFloor,
		AnnualVolatilityCeiling: volCeil,
		SourceURL:               seedSourceURL,
		PublishedAt:             seedPublishedAt,
		ReviewedAt:              seedReviewedAt,
	}
}

func systemCorrelationPriors() []CorrelationPrior {
	eqD := AssetFactorKey("equity", "domestic")
	eqF := AssetFactorKey("equity", "foreign")
	bdD := AssetFactorKey("bond", "domestic")
	bdF := AssetFactorKey("bond", "foreign")
	cash := AssetFactorKey("cash", "domestic")
	fx := FXFactorKey("USD", "CNY")
	return []CorrelationPrior{
		{FactorA: eqD, FactorB: eqF, Rho: 0.60},
		{FactorA: eqD, FactorB: bdD, Rho: 0.15},
		{FactorA: eqD, FactorB: bdF, Rho: 0.10},
		{FactorA: eqF, FactorB: bdD, Rho: 0.10},
		{FactorA: eqF, FactorB: bdF, Rho: 0.20},
		{FactorA: bdD, FactorB: bdF, Rho: 0.40},
		{FactorA: eqF, FactorB: fx, Rho: 0.15},
		// Cash is uncorrelated with every risk factor and FX.
		{FactorA: cash, FactorB: eqD, Rho: 0},
		{FactorA: cash, FactorB: eqF, Rho: 0},
		{FactorA: cash, FactorB: bdD, Rho: 0},
		{FactorA: cash, FactorB: bdF, Rho: 0},
		{FactorA: cash, FactorB: fx, Rho: 0},
		{FactorA: eqD, FactorB: fx, Rho: 0},
		{FactorA: bdD, FactorB: fx, Rho: 0},
		{FactorA: bdF, FactorB: fx, Rho: 0},
	}
}
