package service

import (
	"encoding/json"
	"errors"

	"github.com/fireman/fireman/internal/repository"
)

// PlanParametersAPI is the JSON-facing plan parameters DTO.
type PlanParametersAPI struct {
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
	Seed                     *string `json:"seed,omitempty"`
	// td/061 return-assumption selection. These must round-trip through the API so
	// the wizard/parameters page can persist blended_prior + profile selection;
	// otherwise the binding drops them and the plan silently reverts to
	// historical_cagr (td/063 R0).
	ReturnAssumptionMode        string `json:"return_assumption_mode"`
	AssumptionSelectionMode     string `json:"assumption_selection_mode"`
	ReturnAssumptionSetID       string `json:"return_assumption_set_id"`
	ReturnAssumptionSetVersion  int    `json:"return_assumption_set_version"`
	ReturnAssumptionScenario    string `json:"return_assumption_scenario"`
	CustomReturnAssumptionsJSON string `json:"custom_return_assumptions_json"`
	UpdatedAt                   int64  `json:"updated_at"`
}

// ParametersUpdateAPIRequest updates parameters via API DTOs.
type ParametersUpdateAPIRequest struct {
	ConfigVersion          int               `json:"config_version"`
	Parameters             PlanParametersAPI `json:"parameters"`
	ApplyUnallocatedToCash bool              `json:"apply_unallocated_to_cash,omitempty"`
}

func ParametersToAPI(p repository.PlanParameters) PlanParametersAPI {
	return PlanParametersAPI{
		PlanID: p.PlanID, CurrentAge: p.CurrentAge, RetirementAge: p.RetirementAge, EndAge: p.EndAge,
		TotalAssetsMinor: p.TotalAssetsMinor, AnnualSavingsMinor: p.AnnualSavingsMinor,
		AnnualSavingsGrowthRate: p.AnnualSavingsGrowthRate, AnnualSpendingMinor: p.AnnualSpendingMinor,
		TerminalWealthFloorMinor: p.TerminalWealthFloorMinor, SelectedScenarioID: p.SelectedScenarioID,
		InflationMode: p.InflationMode, FixedInflationRate: p.FixedInflationRate,
		InflationMu: p.InflationMu, InflationPhi: p.InflationPhi, InflationSigma: p.InflationSigma,
		WithdrawalType: p.WithdrawalType, WithdrawalRate: p.WithdrawalRate,
		WithdrawalFloorRatio: p.WithdrawalFloorRatio, WithdrawalCeilingRatio: p.WithdrawalCeilingRatio,
		WithdrawalTaxRate: p.WithdrawalTaxRate, TaxableWithdrawalRatio: p.TaxableWithdrawalRatio,
		RebalanceFrequency: p.RebalanceFrequency, RebalanceThreshold: p.RebalanceThreshold,
		TransactionCostRate: p.TransactionCostRate, SimulationRuns: p.SimulationRuns,
		StudentTDf: p.StudentTDf, Seed: FormatSeedString(p.Seed),
		ReturnAssumptionMode:        p.ReturnAssumptionMode,
		AssumptionSelectionMode:     p.AssumptionSelectionMode,
		ReturnAssumptionSetID:       p.ReturnAssumptionSetID,
		ReturnAssumptionSetVersion:  p.ReturnAssumptionSetVersion,
		ReturnAssumptionScenario:    p.ReturnAssumptionScenario,
		CustomReturnAssumptionsJSON: p.CustomReturnAssumptionsJSON,
		UpdatedAt:                   p.UpdatedAt,
	}
}

func ParametersFromAPI(p PlanParametersAPI) (repository.PlanParameters, error) {
	seed, err := ParseSeedString(p.Seed)
	if err != nil && !errors.Is(err, errSeedNotProvided) {
		return repository.PlanParameters{}, err
	}
	return repository.PlanParameters{
		PlanID: p.PlanID, CurrentAge: p.CurrentAge, RetirementAge: p.RetirementAge, EndAge: p.EndAge,
		TotalAssetsMinor: p.TotalAssetsMinor, AnnualSavingsMinor: p.AnnualSavingsMinor,
		AnnualSavingsGrowthRate: p.AnnualSavingsGrowthRate, AnnualSpendingMinor: p.AnnualSpendingMinor,
		TerminalWealthFloorMinor: p.TerminalWealthFloorMinor, SelectedScenarioID: p.SelectedScenarioID,
		InflationMode: p.InflationMode, FixedInflationRate: p.FixedInflationRate,
		InflationMu: p.InflationMu, InflationPhi: p.InflationPhi, InflationSigma: p.InflationSigma,
		WithdrawalType: p.WithdrawalType, WithdrawalRate: p.WithdrawalRate,
		WithdrawalFloorRatio: p.WithdrawalFloorRatio, WithdrawalCeilingRatio: p.WithdrawalCeilingRatio,
		WithdrawalTaxRate: p.WithdrawalTaxRate, TaxableWithdrawalRatio: p.TaxableWithdrawalRatio,
		RebalanceFrequency: p.RebalanceFrequency, RebalanceThreshold: p.RebalanceThreshold,
		TransactionCostRate: p.TransactionCostRate, SimulationRuns: p.SimulationRuns,
		StudentTDf: p.StudentTDf, Seed: seed,
		ReturnAssumptionMode:        p.ReturnAssumptionMode,
		AssumptionSelectionMode:     p.AssumptionSelectionMode,
		ReturnAssumptionSetID:       p.ReturnAssumptionSetID,
		ReturnAssumptionSetVersion:  p.ReturnAssumptionSetVersion,
		ReturnAssumptionScenario:    p.ReturnAssumptionScenario,
		CustomReturnAssumptionsJSON: p.CustomReturnAssumptionsJSON,
		UpdatedAt:                   p.UpdatedAt,
	}, nil
}

// PathIndexView is the API view of a simulation path index row.
type PathIndexView struct {
	RunID                    string  `json:"run_id"`
	PathNo                   int     `json:"path_no"`
	PathSeed                 string  `json:"path_seed"`
	Succeeded                bool    `json:"succeeded"`
	FailureMonth             *int    `json:"failure_month,omitempty"`
	TerminalWealthMinor      int64   `json:"terminal_wealth_minor"`
	MaxDrawdown              float64 `json:"max_drawdown"`
	RepresentativePercentile string  `json:"representative_percentile,omitempty"`
}

func PathIndexToView(p repository.PathIndexRow) PathIndexView {
	return PathIndexView{
		RunID: p.RunID, PathNo: p.PathNo, PathSeed: FormatSeedInt64(p.PathSeed),
		Succeeded: p.Succeeded, FailureMonth: p.FailureMonth,
		TerminalWealthMinor: p.TerminalWealthMinor, MaxDrawdown: p.MaxDrawdown,
		RepresentativePercentile: p.RepresentativePercentile,
	}
}

// WizardParametersAPI wraps wizard parameters with string seed in JSON.
type WizardParametersAPI struct {
	PlanParametersAPI
}

func (w *WizardParametersAPI) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &w.PlanParametersAPI)
}

func WizardParametersFromAPI(p PlanParametersAPI) (repository.PlanParameters, error) {
	return ParametersFromAPI(p)
}
