package simulation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// EngineVersion is bumped when simulation semantics change. 3.0.0 introduces the
// forward-return calibration and the joint (correlated, shared fat-tail) factor
// model. 2.x snapshots continue to replay with the independent per-asset sampler
// and their frozen ModeledAnnualReturn so old runs reproduce exactly.
const EngineVersion = "3.0.0"

// LegacyEngineVersion is the legacy independent-factor engine. Snapshots
// frozen at this version must keep replaying with the independent sampler.
const LegacyEngineVersion = "2.0.0"

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
	AssetKey        string  `json:"asset_key"`
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
	HistoricalAnnualGeometricReturn float64        `json:"historical_annual_geometric_return,omitempty"`
	ForwardAnnualGeometricReturn    float64        `json:"forward_annual_geometric_return,omitempty"`
	ForwardLogReturn                float64        `json:"forward_log_return,omitempty"`
	AnnualVolatilityUsed            float64        `json:"annual_volatility_used,omitempty"`
	ReturnAssumptionSource          string         `json:"return_assumption_source,omitempty"`
	ReturnAssumptionSetID           string         `json:"return_assumption_set_id,omitempty"`
	ReturnAssumptionSetVersion      int            `json:"return_assumption_set_version,omitempty"`
	ReturnAssumptionScenario        string         `json:"return_assumption_scenario,omitempty"`
	ReturnSampleYears               int            `json:"return_sample_years,omitempty"`
	ReturnHistoricalWeight          float64        `json:"return_historical_weight,omitempty"`
	ReturnWarnings                  []string       `json:"return_warnings,omitempty"`
	FeeTreatment                    string         `json:"fee_treatment"`
	ExpenseRatio                    *float64       `json:"expense_ratio,omitempty"`
	SourceHash                      string         `json:"source_hash"`
	Years                           []SnapshotYear `json:"years"`
	CompleteYearCount               int            `json:"complete_year_count"`
	MonthlyReturnCount              int            `json:"monthly_return_count"`
	HistoryDepth                    string         `json:"history_depth"`
	MetricsVersion                  string         `json:"metrics_version"`
	DataWarnings                    []string       `json:"data_warnings,omitempty"`
	FXSnapshotID                    string         `json:"fx_snapshot_id,omitempty"`
	FXModeledReturn                 float64        `json:"fx_modeled_return,omitempty"`
	FXAnnualVolatility              float64        `json:"fx_annual_volatility,omitempty"`
	FXCompleteYearCount             int            `json:"fx_complete_year_count,omitempty"`
	FXMonthlyReturnCount            int            `json:"fx_monthly_return_count,omitempty"`
	FXHistoryDepth                  string         `json:"fx_history_depth,omitempty"`
	FXMetricsVersion                string         `json:"fx_metrics_version,omitempty"`
	FXDataWarnings                  []string       `json:"fx_data_warnings,omitempty"`
	// Forward FX calibration audit. FXModeledReturn is the
	// value the engine consumes (the forward FX drift for blended_prior/custom, or
	// the raw historical drift for historical_cagr). The fields below explain how
	// it was derived; they are frozen so a run can always justify its FX drift.
	FXHistoricalReturn float64  `json:"fx_historical_return,omitempty"`
	FXPriorReturn      float64  `json:"fx_prior_return,omitempty"`
	FXHistoricalWeight float64  `json:"fx_historical_weight,omitempty"`
	FXReturnSource     string   `json:"fx_return_source,omitempty"`
	FXReturnScenario   string   `json:"fx_return_scenario,omitempty"`
	FXReturnWarnings   []string `json:"fx_return_warnings,omitempty"`
	// Months / FXMonths freeze the complete-year monthly log-return series (keyed
	// "YYYY-MM") used to estimate historical correlations in the joint factor
	// model. Empty means the pair falls back to the
	// profile correlation prior.
	Months   map[string]float64 `json:"months,omitempty"`
	FXMonths map[string]float64 `json:"fx_months,omitempty"`
}

// SnapshotParameters are plan FIRE parameters frozen for a run.
type SnapshotParameters struct {
	CurrentAge               int     `json:"current_age"`
	RetirementAge            int     `json:"retirement_age"`
	EndAge                   int     `json:"end_age"`
	TotalAssetsMinor         int64   `json:"total_assets_minor"`
	AnnualSavingsMinor       int64   `json:"annual_savings_minor"`
	AnnualSavingsGrowthRate  float64 `json:"annual_savings_growth_rate"`
	AnnualSpendingMinor      int64   `json:"annual_spending_minor"`
	TerminalWealthFloorMinor int64   `json:"terminal_wealth_floor_minor"`
	InflationMode            string  `json:"inflation_mode"`
	FixedInflationRate       float64 `json:"fixed_inflation_rate"`
	InflationMu              float64 `json:"inflation_mu"`
	InflationPhi             float64 `json:"inflation_phi"`
	InflationSigma           float64 `json:"inflation_sigma"`
	WithdrawalType           string  `json:"withdrawal_type"`
	WithdrawalRate           float64 `json:"withdrawal_rate"`
	WithdrawalFloorRatio     float64 `json:"withdrawal_floor_ratio"`
	WithdrawalCeilingRatio   float64 `json:"withdrawal_ceiling_ratio"`
	WithdrawalTaxRate        float64 `json:"withdrawal_tax_rate"`
	TaxableWithdrawalRatio   float64 `json:"taxable_withdrawal_ratio"`
	RebalanceFrequency       string  `json:"rebalance_frequency"`
	RebalanceThreshold       float64 `json:"rebalance_threshold"`
	TransactionCostRate      float64 `json:"transaction_cost_rate"`
	SimulationRuns           int     `json:"simulation_runs"`
	StudentTDf               int     `json:"student_t_df"`
	Seed                     string  `json:"seed"`
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
	// nil for legacy independent (2.x) snapshots so old runs replay unchanged.
	FactorModel     *FactorModel `json:"factor_model,omitempty"`
	AssetFactorRefs []FactorRef  `json:"asset_factor_refs,omitempty"`
	// DeterministicCashReturn is set for forward (3.0.0) inputs so cash slots grow
	// at their frozen, non-random monthly return instead of an implicit 0%.
	// Legacy 2.x snapshots leave it false so cash stays at
	// 0% and old runs replay byte-for-byte.
	DeterministicCashReturn bool `json:"deterministic_cash_return,omitempty"`
	// Frozen tail-risk parameters. For forward (3.0.0) runs these are
	// taken from the active profile so the Student-t df and the per-month return
	// truncation are versioned/auditable and a plan can no longer change them; the
	// joint and independent samplers read these frozen values only. Legacy (2.x)
	// snapshots leave them zero/nil and fall back to Parameters.StudentTDf and the
	// ReturnFloor/ReturnCeil constants, so old runs replay byte-for-byte.
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

// CanonicalJSON marshals with sorted keys for stable hashing.
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
