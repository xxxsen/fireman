package simulation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// EngineVersion is bumped when simulation semantics change. 3.0.0 introduced the
// forward-return calibration and the joint (correlated, shared fat-tail) factor
// model. 3.1.0 fixes the guardrail withdrawal strategy so anniversary ±10%
// adjustments compound on the previous year's spending instead of resetting to
// the inflation baseline every year. 3.2.0 adds aggregate cash liquidity and
// fact-based failure states. 3.3.0 adds stable after-retirement net income.
// 3.4.0 fixes retirement-income settlement, failed-path accounting, random
// inflation initialization, and failure-age month precision.
const EngineVersion = "3.5.0"

const (
	FXTreatmentNone               = "none"
	FXTreatmentEmbeddedInAssetNAV = "embedded_in_asset_nav"
	FXTreatmentSeparateFactor     = "separate_factor"
)

// LegacyEngineVersion identifies snapshots created by the former independent
// factor engine.
const LegacyEngineVersion = "2.0.0"

// GuardrailUsesLegacyAnnualReset reports whether a snapshot frozen at the
// given engine version must replay the pre-3.1.0 guardrail semantics
// (anniversary proposal resets to the inflation baseline before the single
// ±10% adjustment). Persisted summaries and path indexes of such runs were
// computed with that behavior, so path regeneration must match it exactly.
// Every engine version shipped before 3.1.0 (1.0.0, 2.0.0, 3.0.0) carried
// the annual-reset behavior.
func GuardrailUsesLegacyAnnualReset(engineVersion string) bool {
	switch engineVersion {
	case "1.0.0", LegacyEngineVersion, "3.0.0":
		return true
	default:
		return false
	}
}

// UsesFactBasedFailureStates centralizes the version gate for failure labels.
func UsesFactBasedFailureStates(engineVersion string) bool {
	switch engineVersion {
	case "1.0.0", LegacyEngineVersion, "3.0.0", "3.1.0":
		return false
	default:
		return true
	}
}

// UsesNetRetirementSettlement reports whether after-tax retirement income is
// netted against living spending before portfolio-withdrawal tax is applied.
func UsesNetRetirementSettlement(engineVersion string) bool {
	return engineVersionAtLeast3_4(engineVersion)
}

// UsesStationaryInflationInitialState reports whether random AR(1) inflation
// starts at its long-run mean instead of the legacy zero value.
func UsesStationaryInflationInitialState(engineVersion string) bool {
	return engineVersionAtLeast3_4(engineVersion)
}

// UsesZeroPaddedFailureSeries reports whether failed paths record the failure
// month and remain at zero wealth for the rest of the horizon.
func UsesZeroPaddedFailureSeries(engineVersion string) bool {
	return engineVersionAtLeast3_4(engineVersion)
}

// UsesMonthPrecisionFailureAge reports whether failure age is calculated at
// the end of the failure month with fractional-year precision.
func UsesMonthPrecisionFailureAge(engineVersion string) bool {
	return engineVersionAtLeast3_4(engineVersion)
}

func engineVersionAtLeast3_4(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	values := [3]int{}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return false
		}
		values[i] = value
	}
	want := [3]int{3, 4, 0}
	for i := range values {
		if values[i] != want[i] {
			return values[i] > want[i]
		}
	}
	return true
}

// Random factor model identifiers (InputSnapshot.RandomFactorModel).
const (
	FactorModelIndependent  = "independent_student_t"
	FactorModelMultivariate = "multivariate_student_t"
)

// FactorRef maps one asset slot to its factor indices in the frozen FactorModel.
// AssetFactorIndex is -1 for cash (no sampled return); FXFactorIndex is -1 when
// the asset needs no separate FX factor (base-currency or CNY-priced QDII).
type FactorRef struct {
	AssetFactorIndex int `json:"asset_factor_index"`
	FXFactorIndex    int `json:"fx_factor_index"`
}

// SnapshotYear is one complete year in a holding snapshot.
type SnapshotYear struct {
	Year         int     `json:"year"`
	AnnualReturn float64 `json:"annual_return"`
	StartDate    string  `json:"start_date"`
	EndDate      string  `json:"end_date"`
	Observations int     `json:"observations"`
}

// SnapshotAsset is one simulated asset frozen at job creation.
type SnapshotAsset struct {
	HoldingID           string  `json:"holding_id"`
	AssetKey            string  `json:"asset_key"`
	InstrumentName      string  `json:"instrument_name,omitempty"`
	InstrumentCode      string  `json:"instrument_code,omitempty"`
	SnapshotID          string  `json:"snapshot_id"`
	Currency            string  `json:"currency"`
	AssetClass          string  `json:"asset_class"`
	Region              string  `json:"region,omitempty"`
	IsCash              bool    `json:"is_cash"`
	InitialMinor        int64   `json:"initial_minor"`
	TargetWeight        float64 `json:"target_weight"`
	ModeledAnnualReturn float64 `json:"modeled_annual_return"`
	AnnualVolatility    float64 `json:"annual_volatility"`
	MaxDrawdown         float64 `json:"max_drawdown"`
	// Forward-return calibration audit fields. These are frozen at run
	// creation so a run can always explain how its drift was derived. Historical
	// facts (HistoricalAnnualGeometricReturn) and the value actually fed to the
	// engine (ForwardAnnualGeometricReturn == ModeledAnnualReturn) are kept
	// separate and never mixed.
	HistoricalAnnualGeometricReturn float64  `json:"historical_annual_geometric_return,omitempty"`
	HistoricalAnnualVolatility      float64  `json:"historical_annual_volatility,omitempty"`
	ForwardAnnualGeometricReturn    float64  `json:"forward_annual_geometric_return,omitempty"`
	ForwardLogReturn                float64  `json:"forward_log_return,omitempty"`
	AnnualVolatilityUsed            float64  `json:"annual_volatility_used,omitempty"`
	ReturnAssumptionSource          string   `json:"return_assumption_source,omitempty"`
	ReturnAssumptionSetID           string   `json:"return_assumption_set_id,omitempty"`
	ReturnAssumptionSetVersion      int      `json:"return_assumption_set_version,omitempty"`
	ReturnAssumptionScenario        string   `json:"return_assumption_scenario,omitempty"`
	ReturnSampleYears               int      `json:"return_sample_years,omitempty"`
	ReturnHistoricalWeight          float64  `json:"return_historical_weight,omitempty"`
	ReturnWarnings                  []string `json:"return_warnings,omitempty"`
	OverrideForwardReturn           *float64 `json:"override_forward_return,omitempty"`
	OverrideAnnualVolatility        *float64 `json:"override_annual_volatility,omitempty"`
	OverrideReason                  string   `json:"override_reason,omitempty"`
	FeeTreatment                    string   `json:"fee_treatment"`
	// ExpenseRatio is retained only to decode historical snapshots. New 3.5
	// snapshots never populate it because ongoing fees are already embedded in
	// fund NAV returns and profile priors.
	ExpenseRatio         *float64       `json:"expense_ratio,omitempty"`
	FXTreatment          string         `json:"fx_treatment,omitempty"`
	SourceHash           string         `json:"source_hash"`
	Years                []SnapshotYear `json:"years"`
	CompleteYearCount    int            `json:"complete_year_count"`
	MonthlyReturnCount   int            `json:"monthly_return_count"`
	HistoryDepth         string         `json:"history_depth"`
	MetricsVersion       string         `json:"metrics_version"`
	DataWarnings         []string       `json:"data_warnings,omitempty"`
	FXSnapshotID         string         `json:"fx_snapshot_id,omitempty"`
	FXModeledReturn      float64        `json:"fx_modeled_return,omitempty"`
	FXAnnualVolatility   float64        `json:"fx_annual_volatility,omitempty"`
	FXCompleteYearCount  int            `json:"fx_complete_year_count,omitempty"`
	FXMonthlyReturnCount int            `json:"fx_monthly_return_count,omitempty"`
	FXHistoryDepth       string         `json:"fx_history_depth,omitempty"`
	FXMetricsVersion     string         `json:"fx_metrics_version,omitempty"`
	FXDataWarnings       []string       `json:"fx_data_warnings,omitempty"`
	// Forward FX calibration audit. FXModeledReturn is the
	// value the engine consumes (the forward FX drift for blended_prior/custom, or
	// the raw historical drift for historical_cagr). The fields below explain how
	// it was derived; they are frozen so a run can always justify its FX drift.
	FXHistoricalReturn     float64  `json:"fx_historical_return,omitempty"`
	FXHistoricalVolatility float64  `json:"fx_historical_volatility,omitempty"`
	FXPriorReturn          float64  `json:"fx_prior_return,omitempty"`
	FXHistoricalWeight     float64  `json:"fx_historical_weight,omitempty"`
	FXReturnSource         string   `json:"fx_return_source,omitempty"`
	FXReturnScenario       string   `json:"fx_return_scenario,omitempty"`
	FXReturnWarnings       []string `json:"fx_return_warnings,omitempty"`
	// Months / FXMonths freeze the complete-year monthly log-return series (keyed
	// "YYYY-MM") used to estimate historical correlations in the joint factor
	// model. Empty means the pair falls back to the
	// profile correlation prior.
	Months   map[string]float64 `json:"months,omitempty"`
	FXMonths map[string]float64 `json:"fx_months,omitempty"`
}

// SnapshotParameters are plan FIRE parameters frozen for a run.
type SnapshotParameters struct {
	CurrentAge                       int     `json:"current_age"`
	RetirementAge                    int     `json:"retirement_age"`
	EndAge                           int     `json:"end_age"`
	TotalAssetsMinor                 int64   `json:"total_assets_minor"`
	AnnualSavingsMinor               int64   `json:"annual_savings_minor"`
	AnnualSavingsGrowthRate          float64 `json:"annual_savings_growth_rate"`
	AnnualSpendingMinor              int64   `json:"annual_spending_minor"`
	AnnualRetirementIncomeMinor      int64   `json:"annual_retirement_income_minor"`
	AnnualRetirementIncomeGrowthRate float64 `json:"annual_retirement_income_growth_rate"`
	TerminalWealthFloorMinor         int64   `json:"terminal_wealth_floor_minor"`
	InflationMode                    string  `json:"inflation_mode"`
	FixedInflationRate               float64 `json:"fixed_inflation_rate"`
	InflationMu                      float64 `json:"inflation_mu"`
	InflationPhi                     float64 `json:"inflation_phi"`
	InflationSigma                   float64 `json:"inflation_sigma"`
	WithdrawalType                   string  `json:"withdrawal_type"`
	WithdrawalRate                   float64 `json:"withdrawal_rate"`
	WithdrawalFloorRatio             float64 `json:"withdrawal_floor_ratio"`
	WithdrawalCeilingRatio           float64 `json:"withdrawal_ceiling_ratio"`
	WithdrawalTaxRate                float64 `json:"withdrawal_tax_rate"`
	TaxableWithdrawalRatio           float64 `json:"taxable_withdrawal_ratio"`
	RebalanceFrequency               string  `json:"rebalance_frequency"`
	RebalanceThreshold               float64 `json:"rebalance_threshold"`
	TransactionCostRate              float64 `json:"transaction_cost_rate"`
	SimulationRuns                   int     `json:"simulation_runs"`
	StudentTDf                       int     `json:"student_t_df"`
	Seed                             string  `json:"seed"`
}

// InputSnapshot is the immutable input captured for a simulation job.
type InputSnapshot struct {
	EngineVersion      string             `json:"engine_version"`
	PlanID             string             `json:"plan_id"`
	BaseCurrency       string             `json:"base_currency"`
	RandomFactorModel  string             `json:"random_factor_model"`
	Parameters         SnapshotParameters `json:"parameters"`
	Assets             []SnapshotAsset    `json:"assets"`
	ConfigHash         string             `json:"config_hash"`
	MarketSnapshotHash string             `json:"market_snapshot_hash"`
	// Joint risk model. Present only for multivariate (3.0.0) runs;
	// nil for independent-factor snapshots.
	FactorModel     *FactorModel `json:"factor_model,omitempty"`
	AssetFactorRefs []FactorRef  `json:"asset_factor_refs,omitempty"`
	// DeterministicCashReturn is set for forward (3.0.0) inputs so cash slots grow
	// at their frozen, non-random monthly return instead of an implicit 0%.
	// When false, cash stays at 0%.
	DeterministicCashReturn bool `json:"deterministic_cash_return,omitempty"`
	// AggregateCashLiquidity records that every cash slot participates in one
	// liquidity pool for savings and withdrawals.
	AggregateCashLiquidity bool `json:"aggregate_cash_liquidity"`
	// Frozen tail-risk parameters. For forward (3.0.0) runs these are
	// taken from the active profile so the Student-t df and the per-month return
	// truncation are versioned/auditable and a plan can no longer change them; the
	// joint and independent samplers read these frozen values only. Legacy (2.x)
	// Snapshots without them fall back to Parameters.StudentTDf and the
	// ReturnFloor/ReturnCeil constants.
	TailStudentTDf  int      `json:"tail_student_t_df,omitempty"`
	TailReturnFloor *float64 `json:"tail_return_floor,omitempty"`
	TailReturnCeil  *float64 `json:"tail_return_ceil,omitempty"`
	// Assumption provenance: the exact system/user profile identity,
	// its canonical content hash and (for system profiles) the backing CMA evidence
	// artifact hash a run was calibrated against, so a result is always explainable
	// by a specific, immutable model. Empty on legacy snapshots predating the field.
	AssumptionProfileID          string `json:"assumption_profile_id,omitempty"`
	AssumptionProfileVersion     int    `json:"assumption_profile_version,omitempty"`
	AssumptionProfileContentHash string `json:"assumption_profile_content_hash,omitempty"`
	AssumptionEvidenceHash       string `json:"assumption_evidence_hash,omitempty"`
	// Run-level return-assumption selection. Per-asset source fields may be
	// overrides or cash rules and must never be used to infer these values.
	ReturnAssumptionMode       string `json:"return_assumption_mode,omitempty"`
	ReturnAssumptionScenario   string `json:"return_assumption_scenario,omitempty"`
	ReturnAssumptionSetID      string `json:"return_assumption_set_id,omitempty"`
	ReturnAssumptionSetVersion int    `json:"return_assumption_set_version,omitempty"`
	// ScenarioComparisonFactorModel freezes the exact correlation matrix used to
	// derive on-demand scenario variants without rereading mutable market data.
	ScenarioComparisonReady       bool         `json:"scenario_comparison_ready,omitempty"`
	ScenarioComparisonFactorModel *FactorModel `json:"scenario_comparison_factor_model,omitempty"`
}

// EffectiveDf returns the frozen Student-t degrees of freedom for sampling: the
// profile-frozen value for forward (3.0.0) runs, or the legacy plan parameter for
// 2.x snapshots.
func (in *InputSnapshot) EffectiveDf() int {
	if in.TailStudentTDf > 0 {
		return in.TailStudentTDf
	}
	return in.Parameters.StudentTDf
}

// TailTruncationBounds returns the frozen per-month simple-return truncation. It
// uses the profile-frozen bounds for forward runs and the legacy constants for
// 2.x snapshots, so historical replays keep their exact clamp.
func (in *InputSnapshot) TailTruncationBounds() TailTruncation {
	if in.TailReturnFloor != nil && in.TailReturnCeil != nil {
		return TailTruncation{Floor: *in.TailReturnFloor, Ceil: *in.TailReturnCeil}
	}
	return LegacyTailTruncation()
}

// HorizonMonths returns the simulated month count.
func (in *InputSnapshot) HorizonMonths() int {
	return (in.Parameters.EndAge - in.Parameters.CurrentAge) * 12
}

// RetirementMonth returns the month index when retirement begins.
func (in *InputSnapshot) RetirementMonth() int {
	return (in.Parameters.RetirementAge - in.Parameters.CurrentAge) * 12
}

// RootSeed parses the root seed string.
func (in *InputSnapshot) RootSeed() int64 {
	var v int64
	_, _ = fmt.Sscan(in.Parameters.Seed, &v)
	if v < 0 {
		return 0
	}
	return v
}

// HashInput returns SHA-256 of canonical JSON for input_hash.
func HashInput(in *InputSnapshot) (string, error) {
	b, err := CanonicalJSON(in)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// CanonicalJSON serializes for stable hashing. Determinism rests on two
// encoding/json guarantees: struct fields are emitted in declaration order,
// and map keys are sorted. That covers InputSnapshot's struct tree plus its
// string-keyed maps (Months/FXMonths). Do NOT introduce interface-typed or
// non-string-keyed map fields inside InputSnapshot — a contract test walks
// the type tree and enforces this, keeping input_hash reproducible.
func CanonicalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// MarketHashFromAssets hashes sorted source hashes.
func MarketHashFromAssets(assets []SnapshotAsset) string {
	hashes := make([]string, 0, len(assets)*2)
	for _, a := range assets {
		hashes = append(hashes, a.SourceHash)
		if a.FXSnapshotID != "" {
			hashes = append(hashes, a.FXSnapshotID)
		}
	}
	sort.Strings(hashes)
	sum := sha256.Sum256([]byte(fmt.Sprint(hashes)))
	return hex.EncodeToString(sum[:])
}
