package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ParametersRepo manages plan_parameters and plan_cash_flows.
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
			simulation_runs, student_t_df, seed, updated_at
		FROM plan_parameters WHERE plan_id=?`, planID).Scan(
		&p.PlanID, &p.CurrentAge, &p.RetirementAge, &p.EndAge,
		&p.TotalAssetsMinor, &p.AnnualSavingsMinor, &p.AnnualSavingsGrowthRate,
		&p.AnnualSpendingMinor, &p.TerminalWealthFloorMinor, &scenario,
		&p.InflationMode, &p.FixedInflationRate, &p.InflationMu, &p.InflationPhi, &p.InflationSigma,
		&p.WithdrawalType, &p.WithdrawalRate, &p.WithdrawalFloorRatio, &p.WithdrawalCeilingRatio,
		&p.WithdrawalTaxRate, &p.TaxableWithdrawalRatio,
		&p.RebalanceFrequency, &p.RebalanceThreshold, &p.TransactionCostRate,
		&p.SimulationRuns, &p.StudentTDf, &seed, &p.UpdatedAt,
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
	_, err := exec.ExecContext(ctx, `
		INSERT INTO plan_parameters (
			plan_id, current_age, retirement_age, end_age,
			total_assets_minor, annual_savings_minor, annual_savings_growth_rate,
			annual_spending_minor, terminal_wealth_floor_minor, selected_scenario_id,
			inflation_mode, fixed_inflation_rate, inflation_mu, inflation_phi, inflation_sigma,
			withdrawal_type, withdrawal_rate, withdrawal_floor_ratio, withdrawal_ceiling_ratio,
			withdrawal_tax_rate, taxable_withdrawal_ratio,
			rebalance_frequency, rebalance_threshold, transaction_cost_rate,
			simulation_runs, student_t_df, seed, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
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
			seed=excluded.seed, updated_at=excluded.updated_at`,
		p.PlanID, p.CurrentAge, p.RetirementAge, p.EndAge,
		p.TotalAssetsMinor, p.AnnualSavingsMinor, p.AnnualSavingsGrowthRate,
		p.AnnualSpendingMinor, p.TerminalWealthFloorMinor, scenario,
		p.InflationMode, p.FixedInflationRate, p.InflationMu, p.InflationPhi, p.InflationSigma,
		p.WithdrawalType, p.WithdrawalRate, p.WithdrawalFloorRatio, p.WithdrawalCeilingRatio,
		p.WithdrawalTaxRate, p.TaxableWithdrawalRatio,
		p.RebalanceFrequency, p.RebalanceThreshold, p.TransactionCostRate,
		p.SimulationRuns, p.StudentTDf, seed, p.UpdatedAt)
	return wrapSQL("upsert plan parameters", err)
}

func (r *ParametersRepo) ListCashFlows(ctx context.Context, planID string) ([]PlanCashFlow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, name, kind, amount_minor, start_month_offset, end_month_offset,
			recurrence, inflation_linked, annual_growth_rate, enabled, note, created_at, updated_at
		FROM plan_cash_flows WHERE plan_id=? ORDER BY start_month_offset`, planID)
	if err != nil {
		return nil, wrapSQL("query plan cash flows", err)
	}
	defer func() { _ = rows.Close() }()
	return scanCashFlows(rows)
}

func (r *ParametersRepo) ReplaceCashFlows(ctx context.Context, tx *sql.Tx, planID string, flows []PlanCashFlow) error {
	exec := r.exec(tx)
	if _, err := exec.ExecContext(ctx, `DELETE FROM plan_cash_flows WHERE plan_id=?`, planID); err != nil {
		return wrapSQL("delete plan cash flows", err)
	}
	now := time.Now().UnixMilli()
	for _, f := range flows {
		if f.CreatedAt == 0 {
			f.CreatedAt = now
		}
		f.UpdatedAt = now
		_, err := exec.ExecContext(ctx, `
			INSERT INTO plan_cash_flows (
				id, plan_id, name, kind, amount_minor, start_month_offset, end_month_offset,
				recurrence, inflation_linked, annual_growth_rate, enabled, note, created_at, updated_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			f.ID, planID, f.Name, f.Kind, f.AmountMinor, f.StartMonthOffset, f.EndMonthOffset,
			f.Recurrence, boolToInt(f.InflationLinked), f.AnnualGrowthRate, boolToInt(f.Enabled),
			f.Note, f.CreatedAt, f.UpdatedAt)
		if err != nil {
			return wrapSQL("insert plan cash flow", err)
		}
	}
	return nil
}

func scanCashFlows(rows *sql.Rows) ([]PlanCashFlow, error) {
	var out []PlanCashFlow
	for rows.Next() {
		var f PlanCashFlow
		var inflLinked, enabled int
		if err := rows.Scan(&f.ID, &f.PlanID, &f.Name, &f.Kind, &f.AmountMinor,
			&f.StartMonthOffset, &f.EndMonthOffset, &f.Recurrence, &inflLinked,
			&f.AnnualGrowthRate, &enabled, &f.Note, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, wrapSQL("scan cash flow row", err)
		}
		f.InflationLinked = inflLinked == 1
		f.Enabled = enabled == 1
		out = append(out, f)
	}
	return out, wrapSQL("iterate cash flow rows", rows.Err())
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
