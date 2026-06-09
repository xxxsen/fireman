package simulation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// EngineVersion is bumped when simulation semantics change.
const EngineVersion = "1.0.0"

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
	HoldingID           string         `json:"holding_id"`
	InstrumentID        string         `json:"instrument_id"`
	SnapshotID          string         `json:"snapshot_id"`
	Currency            string         `json:"currency"`
	AssetClass          string         `json:"asset_class"`
	IsCash              bool           `json:"is_cash"`
	InitialMinor        int64          `json:"initial_minor"`
	TargetWeight        float64        `json:"target_weight"`
	ModeledAnnualReturn float64        `json:"modeled_annual_return"`
	AnnualVolatility    float64        `json:"annual_volatility"`
	MaxDrawdown         float64        `json:"max_drawdown"`
	FeeTreatment        string         `json:"fee_treatment"`
	ExpenseRatio        *float64       `json:"expense_ratio,omitempty"`
	SourceHash          string         `json:"source_hash"`
	Years               []SnapshotYear `json:"years"`
	FXSnapshotID        string         `json:"fx_snapshot_id,omitempty"`
	FXModeledReturn     float64        `json:"fx_modeled_return,omitempty"`
	FXAnnualVolatility  float64        `json:"fx_annual_volatility,omitempty"`
}

// SnapshotCashFlow is a frozen cash-flow event.
type SnapshotCashFlow struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	AmountMinor      int64   `json:"amount_minor"`
	StartMonthOffset int     `json:"start_month_offset"`
	EndMonthOffset   int     `json:"end_month_offset"`
	Recurrence       string  `json:"recurrence"`
	InflationLinked  bool    `json:"inflation_linked"`
	AnnualGrowthRate float64 `json:"annual_growth_rate"`
	Enabled          bool    `json:"enabled"`
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
	CashFlows          []SnapshotCashFlow `json:"cash_flows"`
	ConfigHash         string             `json:"config_hash"`
	MarketSnapshotHash string             `json:"market_snapshot_hash"`
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
	fmt.Sscan(in.Parameters.Seed, &v)
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
