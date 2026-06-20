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

// System profile review metadata (td/063 N1 / td/064 N5 / td/065 R10). The
// provenance points at externally sourced, publicly openable capital-market
// assumptions (not the project's own design docs) with a named, dated sign-off,
// and references the immutable CMA evidence artifact (version + content hash) that
// records, per prior, the specific source, raw inputs and conversion to a
// CNY-nominal after-fee geometric return. Any change to the underlying CMA inputs
// changes the artifact hash and must publish a NEW system profile identity/version
// rather than editing this one in place.
var SystemProfileSourceNote = "CMA v2: conservative, after-fee, CNY-nominal geometric return priors " +
	"reproducible from the committed evidence artifact " + CMAEvidenceVersion +
	" (sha256:" + CMAEvidenceContentHash[:12] + "). Asset real returns from Research Affiliates' " +
	"Asset Allocation Interactive and FX from BIS exchange-rate statistics, converted to CNY nominal " +
	"terms (real + expected inflation - fee) and signed off by the FIRE investment team. See " +
	"internal/assumptions/cma_evidence_v2.json for the per-prior derivation."

const (
	SystemProfileReviewedBy = "FIRE 投研团队 (CMA v2 sign-off)"
	SystemProfileReviewedAt = "2026-06-20"
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
		// Return and FX priors are materialized from the immutable CMA evidence
		// artifact (td/065 R10): the value is recomputed from documented inputs and
		// each carries its specific dated source. Native-currency foreign priors
		// (USD/HKD) let an asset priced in its own currency calibrate its local
		// return without an FX/currency mismatch; the FX priors carry the currency
		// view separately (td/063 R2) and share the equity:foreign / bond:foreign
		// factor with the CNY cells.
		ReturnPriors:      buildSystemReturnPriors(),
		FXPriors:          buildSystemFXPriors(),
		CorrelationPriors: systemCorrelationPriors(),
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
