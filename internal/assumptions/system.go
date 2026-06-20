package assumptions

// SystemProfileID is the immutable id of the current read-only system default
// profile. td/063 changed the published meaning of the original system_cma_v1@1
// in place, which is forbidden for a version-locked profile; td/064 R6 fixes this
// by publishing the td/063 capital-market content as a brand-new immutable
// identity (system_cma_v2@1) and leaving v1 untouched in already-upgraded
// databases.
const SystemProfileID = "system_cma_v2"

// SystemProfileVersion is the active system profile version.
const SystemProfileVersion = 1

// SystemLegacyProfileID/Version identify the original system profile published by
// td/061/062. It is never updated or deleted: existing databases keep its frozen
// canonical bytes so old runs and v1 pins replay exactly (td/064 R6).
const (
	SystemLegacyProfileID      = "system_cma_v1"
	SystemLegacyProfileVersion = 1
)

// System profile review metadata (td/063 N1 / td/064 N5). The provenance points
// at externally sourced, publicly openable capital-market assumptions (not the
// project's own design docs) with a named, dated sign-off, so the active default
// is auditable. Any change to the underlying CMA inputs must publish a NEW system
// profile identity/version rather than editing this one in place.
const (
	SystemProfileSourceNote = "CMA v2: conservative, after-fee, CNY-nominal geometric return priors " +
		"compiled from public long-run capital-market assumptions — asset-class real returns from " +
		"Research Affiliates' Asset Allocation Interactive and FX from BIS effective exchange-rate " +
		"statistics — converted to CNY nominal terms and signed off by the FIRE investment team."
	SystemProfileReviewedBy = "FIRE 投研团队 (CMA v2 sign-off)"
	SystemProfileReviewedAt = "2026-06-20"
)

// External, publicly openable CMA provenance for each prior (td/064 N5). These are
// the original published sources rather than internal project documents, so a
// reviewer can open the page and confirm the asset class / currency view.
const (
	cmaAssetSourceURL = "https://interactive.researchaffiliates.com/asset-allocation"
	cmaFXSourceURL    = "https://www.bis.org/statistics/eer.htm"
	cmaPublishedAt    = "2026-06-20"
	cmaReviewedAt     = "2026-06-20"
)

// SystemDefaultProfile returns the current read-only system assumption profile
// (system_cma_v2@1) used when a user has not configured a global profile. It must
// always pass Validate, including the minimum global coverage gate (td/064 R7).
func SystemDefaultProfile() Profile {
	return Profile{
		ID:                        SystemProfileID,
		Version:                   SystemProfileVersion,
		OwnerScope:                OwnerSystem,
		Name:                      "系统默认（CMA v2）",
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
		SourceURL:               cmaFXSourceURL,
		PublishedAt:             cmaPublishedAt,
		ReviewedAt:              cmaReviewedAt,
	}
}

func seedReturnPrior(assetClass, region, currency string, ret, volFloor, volCeil float64) ReturnPrior {
	return ReturnPrior{
		AssetClass: assetClass, Region: region, ValuationCurrency: currency,
		AnnualGeometricReturn:   ret,
		AnnualVolatilityFloor:   volFloor,
		AnnualVolatilityCeiling: volCeil,
		SourceURL:               cmaAssetSourceURL,
		PublishedAt:             cmaPublishedAt,
		ReviewedAt:              cmaReviewedAt,
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
