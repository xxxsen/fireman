package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ParametersRepo manages plan_parameters.
type ParametersRepo struct {
	db *sql.DB
}

func NewParametersRepo(db *sql.DB) *ParametersRepo {
	return &ParametersRepo{db: db}
}

func (r *ParametersRepo) Get(ctx context.Context, planID string) (PlanParameters, error) {
	var p PlanParameters
	var scenario sql.NullString
	var seed sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT plan_id, current_age, retirement_age, end_age,
			total_assets_minor, annual_savings_minor, annual_savings_growth_rate,
			annual_spending_minor, terminal_wealth_floor_minor, selected_scenario_id,
			inflation_mode, fixed_inflation_rate, inflation_mu, inflation_phi, inflation_sigma,
			withdrawal_type, withdrawal_rate, withdrawal_floor_ratio, withdrawal_ceiling_ratio,
			withdrawal_tax_rate, taxable_withdrawal_ratio,
			rebalance_frequency, rebalance_threshold, transaction_cost_rate,
			simulation_runs, student_t_df, seed,
			return_assumption_mode, assumption_selection_mode, return_assumption_set_id,
			return_assumption_set_version, return_assumption_scenario, custom_return_assumptions_json,
			updated_at
		FROM plan_parameters WHERE plan_id=?`, planID).Scan(
		&p.PlanID, &p.CurrentAge, &p.RetirementAge, &p.EndAge,
		&p.TotalAssetsMinor, &p.AnnualSavingsMinor, &p.AnnualSavingsGrowthRate,
		&p.AnnualSpendingMinor, &p.TerminalWealthFloorMinor, &scenario,
		&p.InflationMode, &p.FixedInflationRate, &p.InflationMu, &p.InflationPhi, &p.InflationSigma,
		&p.WithdrawalType, &p.WithdrawalRate, &p.WithdrawalFloorRatio, &p.WithdrawalCeilingRatio,
		&p.WithdrawalTaxRate, &p.TaxableWithdrawalRatio,
		&p.RebalanceFrequency, &p.RebalanceThreshold, &p.TransactionCostRate,
		&p.SimulationRuns, &p.StudentTDf, &seed,
		&p.ReturnAssumptionMode, &p.AssumptionSelectionMode, &p.ReturnAssumptionSetID,
		&p.ReturnAssumptionSetVersion, &p.ReturnAssumptionScenario, &p.CustomReturnAssumptionsJSON,
		&p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanParameters{}, ErrPlanNotFound
	}
	if err != nil {
		return PlanParameters{}, wrapSQL("scan plan parameters", err)
	}
	if scenario.Valid {
		s := scenario.String
		p.SelectedScenarioID = &s
	}
	if seed.Valid {
		v := seed.Int64
		p.Seed = &v
	}
	return p, nil
}

func (r *ParametersRepo) SetSelectedScenarioID(
	ctx context.Context,
	tx *sql.Tx,
	planID string,
	scenarioID string,
) error {
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx, `
		UPDATE plan_parameters
		SET selected_scenario_id=?, updated_at=?
		WHERE plan_id=?`,
		scenarioID, time.Now().UnixMilli(), planID)
	return wrapSQL("set selected scenario id", err)
}

func (r *ParametersRepo) SetTotalAssetsMinor(
	ctx context.Context,
	tx *sql.Tx,
	planID string,
	totalAssetsMinor int64,
) error {
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx, `
		UPDATE plan_parameters
		SET total_assets_minor=?, updated_at=?
		WHERE plan_id=?`,
		totalAssetsMinor, time.Now().UnixMilli(), planID)
	return wrapSQL("set total assets minor", err)
}

func (r *ParametersRepo) Upsert(ctx context.Context, tx *sql.Tx, p PlanParameters) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	p.UpdatedAt = now
	var scenario any
	if p.SelectedScenarioID != nil {
		scenario = *p.SelectedScenarioID
	}
	var seed any
	if p.Seed != nil {
		seed = *p.Seed
	}
	p.applyAssumptionDefaults()
	_, err := exec.ExecContext(ctx, `
		INSERT INTO plan_parameters (
			plan_id, current_age, retirement_age, end_age,
			total_assets_minor, annual_savings_minor, annual_savings_growth_rate,
			annual_spending_minor, terminal_wealth_floor_minor, selected_scenario_id,
			inflation_mode, fixed_inflation_rate, inflation_mu, inflation_phi, inflation_sigma,
			withdrawal_type, withdrawal_rate, withdrawal_floor_ratio, withdrawal_ceiling_ratio,
			withdrawal_tax_rate, taxable_withdrawal_ratio,
			rebalance_frequency, rebalance_threshold, transaction_cost_rate,
			simulation_runs, student_t_df, seed,
			return_assumption_mode, assumption_selection_mode, return_assumption_set_id,
			return_assumption_set_version, return_assumption_scenario, custom_return_assumptions_json,
			updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(plan_id) DO UPDATE SET
			current_age=excluded.current_age, retirement_age=excluded.retirement_age, end_age=excluded.end_age,
			total_assets_minor=excluded.total_assets_minor, annual_savings_minor=excluded.annual_savings_minor,
			annual_savings_growth_rate=excluded.annual_savings_growth_rate,
			annual_spending_minor=excluded.annual_spending_minor,
			terminal_wealth_floor_minor=excluded.terminal_wealth_floor_minor,
			selected_scenario_id=excluded.selected_scenario_id,
			inflation_mode=excluded.inflation_mode, fixed_inflation_rate=excluded.fixed_inflation_rate,
			inflation_mu=excluded.inflation_mu, inflation_phi=excluded.inflation_phi,
			inflation_sigma=excluded.inflation_sigma,
			withdrawal_type=excluded.withdrawal_type, withdrawal_rate=excluded.withdrawal_rate,
			withdrawal_floor_ratio=excluded.withdrawal_floor_ratio,
			withdrawal_ceiling_ratio=excluded.withdrawal_ceiling_ratio,
			withdrawal_tax_rate=excluded.withdrawal_tax_rate,
			taxable_withdrawal_ratio=excluded.taxable_withdrawal_ratio,
			rebalance_frequency=excluded.rebalance_frequency, rebalance_threshold=excluded.rebalance_threshold,
			transaction_cost_rate=excluded.transaction_cost_rate,
			simulation_runs=excluded.simulation_runs, student_t_df=excluded.student_t_df,
			seed=excluded.seed,
			return_assumption_mode=excluded.return_assumption_mode,
			assumption_selection_mode=excluded.assumption_selection_mode,
			return_assumption_set_id=excluded.return_assumption_set_id,
			return_assumption_set_version=excluded.return_assumption_set_version,
			return_assumption_scenario=excluded.return_assumption_scenario,
			custom_return_assumptions_json=excluded.custom_return_assumptions_json,
			updated_at=excluded.updated_at`,
		p.PlanID, p.CurrentAge, p.RetirementAge, p.EndAge,
		p.TotalAssetsMinor, p.AnnualSavingsMinor, p.AnnualSavingsGrowthRate,
		p.AnnualSpendingMinor, p.TerminalWealthFloorMinor, scenario,
		p.InflationMode, p.FixedInflationRate, p.InflationMu, p.InflationPhi, p.InflationSigma,
		p.WithdrawalType, p.WithdrawalRate, p.WithdrawalFloorRatio, p.WithdrawalCeilingRatio,
		p.WithdrawalTaxRate, p.TaxableWithdrawalRatio,
		p.RebalanceFrequency, p.RebalanceThreshold, p.TransactionCostRate,
		p.SimulationRuns, p.StudentTDf, seed,
		p.ReturnAssumptionMode, p.AssumptionSelectionMode, p.ReturnAssumptionSetID,
		p.ReturnAssumptionSetVersion, p.ReturnAssumptionScenario, p.CustomReturnAssumptionsJSON,
		p.UpdatedAt)
	return wrapSQL("upsert plan parameters", err)
}

// Assumption-selection defaults. New plans currently default to historical_cagr
// so existing numerical behavior is preserved; the blended_prior/baseline flip
// is gated behind the final forward-engine rollout step once the joint factor engine and
// regression report land.
const (
	DefaultReturnAssumptionMode     = "historical_cagr"
	DefaultAssumptionSelectionMode  = "follow_global"
	DefaultReturnAssumptionScenario = "baseline"

	// Return-assumption modes. ModeHistoricalCAGR is the legacy/compat default for
	// migrated plans and direct fixtures; new plans use ModeBlendedPrior.
	ModeHistoricalCAGR = "historical_cagr"
	ModeBlendedPrior   = "blended_prior"
	ModeCustom         = "custom"

	// DefaultStudentTDf is the server-assigned Student-t df for new plans. The
	// plan-level df is a legacy field used only to replay 2.x snapshots; forward
	// (blended_prior/custom) runs freeze the global profile's df instead, so the
	// plan value is never client-writable.
	DefaultStudentTDf = 7
)

func (p *PlanParameters) applyAssumptionDefaults() {
	if p.ReturnAssumptionMode == "" {
		p.ReturnAssumptionMode = DefaultReturnAssumptionMode
	}
	if p.AssumptionSelectionMode == "" {
		p.AssumptionSelectionMode = DefaultAssumptionSelectionMode
	}
	if p.ReturnAssumptionScenario == "" {
		p.ReturnAssumptionScenario = DefaultReturnAssumptionScenario
	}
}

func (r *ParametersRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

type dbExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
