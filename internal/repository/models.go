package repository

// Plan is a FIRE plan record.
type Plan struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	BaseCurrency  string `json:"base_currency"`
	ValuationDate string `json:"valuation_date"`
	Status        string `json:"status"`
	ConfigVersion int    `json:"config_version"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

// PlanParameters holds FIRE and simulation parameters.
type PlanParameters struct {
	PlanID                   string  `json:"plan_id"`
	CurrentAge               int     `json:"current_age"`
	RetirementAge            int     `json:"retirement_age"`
	EndAge                   int     `json:"end_age"`
	TotalAssetsMinor         int64   `json:"total_assets_minor"`
	AnnualSavingsMinor       int64   `json:"annual_savings_minor"`
	AnnualSavingsGrowthRate  float64 `json:"annual_savings_growth_rate"`
	AnnualSpendingMinor      int64   `json:"annual_spending_minor"`
	TerminalWealthFloorMinor int64   `json:"terminal_wealth_floor_minor"`
	SelectedScenarioID       *string `json:"selected_scenario_id,omitempty"`
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
	Seed                     *int64  `json:"seed,omitempty"`
	// td/061 return-assumption selection. These reference a global profile and
	// scenario; they never duplicate the profile's numeric values.
	ReturnAssumptionMode        string `json:"return_assumption_mode"`
	AssumptionSelectionMode     string `json:"assumption_selection_mode"`
	ReturnAssumptionSetID       string `json:"return_assumption_set_id"`
	ReturnAssumptionSetVersion  int    `json:"return_assumption_set_version"`
	ReturnAssumptionScenario    string `json:"return_assumption_scenario"`
	CustomReturnAssumptionsJSON string `json:"custom_return_assumptions_json,omitempty"`
	UpdatedAt                   int64  `json:"updated_at"`
}

// AssetClassTarget is a plan-level asset class weight.
type AssetClassTarget struct {
	AssetClass string  `json:"asset_class"`
	Weight     float64 `json:"weight"`
}

// RegionTarget is a plan-level region weight within an asset class.
type RegionTarget struct {
	AssetClass        string  `json:"asset_class"`
	Region            string  `json:"region"`
	WeightWithinClass float64 `json:"weight_within_class"`
}

// PlanAllocation groups asset class and region targets.
type PlanAllocation struct {
	AssetClassTargets []AssetClassTarget `json:"asset_class_targets"`
	RegionTargets     []RegionTarget     `json:"region_targets"`
}

// AllocationScenario is a reusable asset class and region target preset.
type AllocationScenario struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	IsBuiltin     bool               `json:"is_builtin"`
	Weights       []AssetClassTarget `json:"weights"`
	RegionTargets []RegionTarget     `json:"region_targets"`
	PlanCount     int                `json:"plan_count,omitempty"`
	CreatedAt     int64              `json:"created_at"`
	UpdatedAt     int64              `json:"updated_at"`
}

// Instrument is a minimal instrument record for holdings.
type Instrument struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Market     string `json:"market"`
	AssetClass string `json:"asset_class"`
	Region     string `json:"region"`
	Currency   string `json:"currency"`
	Status     string `json:"status"`
	IsSystem   bool   `json:"is_system"`
}

// PlanHolding is a plan position.
type PlanHolding struct {
	ID                          string  `json:"id"`
	PlanID                      string  `json:"plan_id"`
	InstrumentID                string  `json:"instrument_id"`
	Enabled                     bool    `json:"enabled"`
	AssetClass                  string  `json:"asset_class"`
	Region                      string  `json:"region"`
	WeightWithinGroup           float64 `json:"weight_within_group"`
	CurrentAmountMinor          int64   `json:"current_amount_minor"`
	SimulationSnapshotID        string  `json:"simulation_snapshot_id"`
	SimulationSnapshotCreatedAt int64   `json:"simulation_snapshot_created_at,omitempty"`
	SortOrder                   int     `json:"sort_order"`
	CreatedAt                   int64   `json:"created_at"`
	UpdatedAt                   int64   `json:"updated_at"`
	// Enriched fields for API responses.
	InstrumentCode             string   `json:"instrument_code,omitempty"`
	InstrumentName             string   `json:"instrument_name,omitempty"`
	SnapshotCompleteYearCount  int      `json:"snapshot_complete_year_count,omitempty"`
	SnapshotMonthlyReturnCount int      `json:"snapshot_monthly_return_count,omitempty"`
	SnapshotHistoryDepth       string   `json:"snapshot_history_depth,omitempty"`
	SnapshotMetricsVersion     string   `json:"snapshot_metrics_version,omitempty"`
	SnapshotWarnings           []string `json:"snapshot_warnings,omitempty"`
}

// PortfolioSnapshot records a point-in-time portfolio state.
type PortfolioSnapshot struct {
	ID               string                  `json:"id"`
	PlanID           string                  `json:"plan_id"`
	SnapshotDate     string                  `json:"snapshot_date"`
	TotalAmountMinor int64                   `json:"total_amount_minor"`
	Note             string                  `json:"note"`
	CreatedAt        int64                   `json:"created_at"`
	Items            []PortfolioSnapshotItem `json:"items,omitempty"`
}

// PortfolioSnapshotItem is one line in a portfolio snapshot.
type PortfolioSnapshotItem struct {
	InstrumentID string `json:"instrument_id"`
	AmountMinor  int64  `json:"amount_minor"`
}
