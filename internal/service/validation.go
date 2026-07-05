package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

var (
	errRegionTargetMissingClass     = errors.New("region_targets missing asset_class")
	errRegionTargetsSum             = errors.New("region targets must sum to 100%")
	errAgesMustBePositive           = errors.New("ages must be positive")
	errAgeOrderingInvalid           = errors.New("must satisfy 0 < current_age <= retirement_age < end_age <= 120")
	errAssetsSpendingPositive       = errors.New("total_assets and annual_spending must be > 0")
	errAmountsNonNegative           = errors.New("amounts must be non-negative")
	errSimulationRunsRange          = errors.New("simulation_runs must be in [1000, 100000]")
	errStudentTDfRange              = errors.New("student_t_df must be in [5, 30]")
	errRebalanceThresholdRange      = errors.New("rebalance_threshold must be in [0, 0.5]")
	errAnnualSavingsGrowthRateRange = errors.New("annual_savings_growth_rate must be in [-0.5, 0.5]")
	errWithdrawalTypeInvalid        = errors.New("withdrawal_type must be fixed_real, fixed_portfolio, or guardrail")
	errInflationModeInvalid         = errors.New("inflation_mode must be fixed_real or random_ar1")
	errRegionTargetsRequired        = errors.New("region_targets is required")
	errRegionWeightRange            = errors.New("region weight must be in [0, 1]")
	errScenarioWeightsSum           = errors.New("scenario weights must sum to 100%")
	errFixedInflationRange          = errors.New("fixed_inflation_rate must be in [-0.02, 0.20]")
	errInflationMuRange             = errors.New("inflation_mu must be in [-0.02, 0.20]")
	errInflationSigmaRange          = errors.New("inflation_sigma must be in [0, 0.20]")
	errInflationPhiRange            = errors.New("inflation_phi must be in [0, 1]")
	errWithdrawalRateRange          = errors.New("withdrawal_rate must be in [0, 1]")
	errWithdrawalFloorRange         = errors.New("withdrawal_floor_ratio must be in (0, 1]")
	errWithdrawalCeilingRange       = errors.New("withdrawal_ceiling_ratio must be in [1, 2]")
	errWithdrawalTaxRateRange       = errors.New("withdrawal_tax_rate must be in [0, 1]")
	errTaxableWithdrawalRange       = errors.New("taxable_withdrawal_ratio must be in [0, 1]")
	errWithdrawalFloorCeiling       = errors.New("withdrawal_floor_ratio must be < withdrawal_ceiling_ratio")
	errAssumptionModeInvalid        = errors.New(
		"return_assumption_mode must be historical_cagr, blended_prior or custom",
	)
	errAssumptionSelectionInvalid = errors.New(
		"assumption_selection_mode must be follow_global or pinned_profile",
	)
	errAssumptionScenarioInvalid = errors.New(
		"return_assumption_scenario must be conservative, baseline or optimistic",
	)
	errAssumptionPinMissing = errors.New(
		"pinned_profile selection requires return_assumption_set_id and version >= 1",
	)
	errAssumptionCustomJSON   = errors.New("custom_return_assumptions_json must be a JSON object of numbers")
	errAssumptionPinNotFound  = errors.New("pinned assumption profile not found")
	errAssumptionPinNotActive = errors.New("pinned assumption profile must be an active version")
)

// Assumption-selection enums shared by the parameters API and validator.
const (
	SelectionFollowGlobal  = "follow_global"
	SelectionPinnedProfile = "pinned_profile"
)

// errBaseCurrencyUnsupported rejects any plan base currency the published system
// profile does not cover. The system profile's return/FX/cash priors and the
// RequiredGlobalCoverage gate are CNY-only, so a non-CNY plan would save but then
// fail to simulate for a missing currency mapping. Supporting another
// base currency requires first publishing a system profile that fully covers it.
var errBaseCurrencyUnsupported = errors.New(
	"base_currency must be " + assumptions.BaseCoverageCurrency +
		" (the only currency the system assumption profile covers)",
)

// validateBaseCurrency enforces the CNY-only base-currency boundary at every plan
// write entry point (create, wizard, metadata update) so an unsupported plan is
// blocked at save time rather than at simulation time.
func validateBaseCurrency(c string) error {
	if c != assumptions.BaseCoverageCurrency {
		return errBaseCurrencyUnsupported
	}
	return nil
}

func validateParameters(p repository.PlanParameters) error {
	if err := validateParameterAges(p); err != nil {
		return err
	}
	if err := validateParameterAmounts(p); err != nil {
		return err
	}
	if err := validateParameterRanges(p); err != nil {
		return err
	}
	if err := validateParameterModes(p); err != nil {
		return err
	}
	if err := validateAssumptionSelection(p); err != nil {
		return err
	}
	return validateParameterAdvanced(p)
}

// validateAssumptionSelection enforces the return-assumption selection
// enums. It is tolerant of empty values (those are normalised to the
// per-context default before persistence) but rejects any non-empty value outside
// the allowed set, an incomplete pin, or an unparseable custom map. The pin's
// active status is checked separately because it requires a database lookup
// (validatePinnedProfileActive).
func validateAssumptionSelection(p repository.PlanParameters) error {
	switch p.ReturnAssumptionMode {
	case "", repository.ModeHistoricalCAGR, repository.ModeBlendedPrior, repository.ModeCustom:
	default:
		return errAssumptionModeInvalid
	}
	switch p.AssumptionSelectionMode {
	case "", SelectionFollowGlobal, SelectionPinnedProfile:
	default:
		return errAssumptionSelectionInvalid
	}
	switch p.ReturnAssumptionScenario {
	case "", assumptions.ScenarioConservative, assumptions.ScenarioBaseline, assumptions.ScenarioOptimistic:
	default:
		return errAssumptionScenarioInvalid
	}
	if p.AssumptionSelectionMode == SelectionPinnedProfile {
		if p.ReturnAssumptionSetID == "" || p.ReturnAssumptionSetVersion < 1 {
			return errAssumptionPinMissing
		}
	}
	if p.CustomReturnAssumptionsJSON != "" {
		var m map[string]float64
		if err := json.Unmarshal([]byte(p.CustomReturnAssumptionsJSON), &m); err != nil {
			return errAssumptionCustomJSON
		}
	}
	return nil
}

// validatePinnedProfileActive enforces that a pinned profile selection references
// an existing, active profile version. Plans pinning a
// draft/superseded version are rejected at create/update/wizard time; already
// frozen runs are unaffected because they snapshot the resolved profile.
func validatePinnedProfileActive(
	ctx context.Context, repo *repository.AssumptionProfileRepo, p repository.PlanParameters,
) error {
	if p.AssumptionSelectionMode != SelectionPinnedProfile {
		return nil
	}
	prof, err := repo.Get(ctx, p.ReturnAssumptionSetID, p.ReturnAssumptionSetVersion)
	if err != nil {
		if errors.Is(err, repository.ErrAssumptionProfileNotFound) {
			return errAssumptionPinNotFound
		}
		return fmt.Errorf("load pinned assumption profile: %w", err)
	}
	if prof.Status != assumptions.StatusActive {
		return errAssumptionPinNotActive
	}
	return nil
}

// validateParameterAdvanced enforces the ranges and cross-field relationships for
// the advanced FIRE parameters (inflation, withdrawal, taxation) exposed by the
// creation wizard and parameters page. It is the single authority shared by plan
// creation, wizard creation, parameter updates and simulation readiness, so a
// plan can never be created that later fails to simulate.
func validateParameterAdvanced(p repository.PlanParameters) error {
	if err := validateInflationParams(p); err != nil {
		return err
	}
	return validateWithdrawalParams(p)
}

func validateInflationParams(p repository.PlanParameters) error {
	if p.FixedInflationRate < -0.02 || p.FixedInflationRate > 0.20 {
		return errFixedInflationRange
	}
	if p.InflationMu < -0.02 || p.InflationMu > 0.20 {
		return errInflationMuRange
	}
	if p.InflationSigma < 0 || p.InflationSigma > 0.20 {
		return errInflationSigmaRange
	}
	if p.InflationPhi < 0 || p.InflationPhi > 1 {
		return errInflationPhiRange
	}
	return nil
}

func validateWithdrawalParams(p repository.PlanParameters) error {
	if p.WithdrawalRate < 0 || p.WithdrawalRate > 1 {
		return errWithdrawalRateRange
	}
	if p.WithdrawalFloorRatio <= 0 || p.WithdrawalFloorRatio > 1 {
		return errWithdrawalFloorRange
	}
	if p.WithdrawalCeilingRatio < 1 || p.WithdrawalCeilingRatio > 2 {
		return errWithdrawalCeilingRange
	}
	// Guardrail must keep a usable band: a floor at or above the ceiling leaves no
	// room to flex spending and is an invalid plan.
	if p.WithdrawalFloorRatio >= p.WithdrawalCeilingRatio {
		return errWithdrawalFloorCeiling
	}
	if p.WithdrawalTaxRate < 0 || p.WithdrawalTaxRate > 1 {
		return errWithdrawalTaxRateRange
	}
	if p.TaxableWithdrawalRatio < 0 || p.TaxableWithdrawalRatio > 1 {
		return errTaxableWithdrawalRange
	}
	// The net-of-tax withdrawal divides by (1 - tax*taxable); a product of 1 (or
	// more) makes the plan impossible to fund, so reject it at creation rather
	// than at simulation time.
	if p.WithdrawalTaxRate*p.TaxableWithdrawalRatio >= 1 {
		return errWithdrawalTaxInvalid
	}
	return nil
}

func validateParameterAges(p repository.PlanParameters) error {
	if p.CurrentAge <= 0 || p.RetirementAge <= 0 || p.EndAge <= 0 {
		return errAgesMustBePositive
	}
	if p.CurrentAge > p.RetirementAge || p.RetirementAge >= p.EndAge || p.EndAge > 120 {
		return errAgeOrderingInvalid
	}
	return nil
}

func validateParameterAmounts(p repository.PlanParameters) error {
	if p.TotalAssetsMinor <= 0 || p.AnnualSpendingMinor <= 0 {
		return errAssetsSpendingPositive
	}
	if p.AnnualSavingsMinor < 0 || p.TerminalWealthFloorMinor < 0 {
		return errAmountsNonNegative
	}
	return nil
}

func validateParameterRanges(p repository.PlanParameters) error {
	if p.SimulationRuns < 1000 || p.SimulationRuns > 100000 {
		return errSimulationRunsRange
	}
	if p.StudentTDf < 5 || p.StudentTDf > 30 {
		return errStudentTDfRange
	}
	if p.RebalanceThreshold < 0 || p.RebalanceThreshold > 0.5 {
		return errRebalanceThresholdRange
	}
	if p.AnnualSavingsGrowthRate < -0.5 || p.AnnualSavingsGrowthRate > 0.5 {
		return errAnnualSavingsGrowthRateRange
	}
	return nil
}

func validateParameterModes(p repository.PlanParameters) error {
	switch p.WithdrawalType {
	case "fixed_real", "fixed_portfolio", "guardrail":
	default:
		return errWithdrawalTypeInvalid
	}
	switch p.InflationMode {
	case "fixed_real", "random_ar1":
	default:
		return errInflationModeInvalid
	}
	return nil
}

func validateRegionTargets(targets []repository.RegionTarget) error {
	if len(targets) == 0 {
		return errRegionTargetsRequired
	}
	byClass := make(map[string]float64)
	for _, t := range targets {
		if t.WeightWithinClass < 0 || t.WeightWithinClass > 1 {
			return errRegionWeightRange
		}
		byClass[t.AssetClass] += t.WeightWithinClass
	}
	for _, ac := range domain.AssetClasses {
		sum, ok := byClass[ac]
		if !ok {
			return fmt.Errorf("%w %s", errRegionTargetMissingClass, ac)
		}
		if sum < 1.0-domain.WeightTolerance || sum > 1.0+domain.WeightTolerance {
			return fmt.Errorf("%w for %s", errRegionTargetsSum, ac)
		}
	}
	return nil
}

func validateScenarioWeights(weights []repository.AssetClassTarget) error {
	sum := 0.0
	for _, w := range weights {
		sum += w.Weight
	}
	if sum < 1.0-domain.WeightTolerance || sum > 1.0+domain.WeightTolerance {
		return errScenarioWeightsSum
	}
	return nil
}

func toDomainAllocation(alloc repository.PlanAllocation) domain.AllocationWeights {
	aw := domain.AllocationWeights{
		AssetClass: make(map[string]float64),
		Region:     make(map[string]map[string]float64),
	}
	for _, t := range alloc.AssetClassTargets {
		aw.AssetClass[t.AssetClass] = t.Weight
	}
	for _, t := range alloc.RegionTargets {
		if aw.Region[t.AssetClass] == nil {
			aw.Region[t.AssetClass] = make(map[string]float64)
		}
		aw.Region[t.AssetClass][t.Region] = t.WeightWithinClass
	}
	return aw
}

func holdingsToDomain(holds []repository.PlanHolding) []domain.HoldingWeightInput {
	out := make([]domain.HoldingWeightInput, len(holds))
	for i, h := range holds {
		out[i] = domain.HoldingWeightInput{
			AssetClass: h.AssetClass, Region: h.Region, Enabled: h.Enabled,
			WeightWithinGroup: h.WeightWithinGroup, CurrentAmountMinor: h.CurrentAmountMinor,
		}
	}
	return out
}

func enrichInstrumentNames(lines []domain.HoldingTargetLine, holds []repository.PlanHolding) {
	for i := range lines {
		for _, h := range holds {
			if h.ID == lines[i].HoldingID {
				lines[i].InstrumentName = h.InstrumentName
				lines[i].InstrumentCode = h.InstrumentCode
				break
			}
		}
	}
}

func holdingMeta(holds []repository.PlanHolding) []struct {
	ID, AssetKey, SimulationSnapshotID string
	SortOrder                              int
} {
	out := make([]struct {
		ID, AssetKey, SimulationSnapshotID string
		SortOrder                              int
	}, len(holds))
	for i, h := range holds {
		out[i].ID = h.ID
		out[i].AssetKey = h.AssetKey
		out[i].SimulationSnapshotID = h.SimulationSnapshotID
		out[i].SortOrder = h.SortOrder
	}
	return out
}
