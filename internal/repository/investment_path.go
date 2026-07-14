package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrInvestmentPathRunNotFound = errors.New("investment path run not found")

type InvestmentPathRun struct {
	ID                string `json:"id"`
	TaskID            string `json:"task_id"`
	AssetKey          string `json:"asset_key"`
	Mode              string `json:"mode"`
	InputHash         string `json:"input_hash"`
	SourceHash        string `json:"source_hash"`
	InputSnapshotJSON string `json:"input_snapshot_json"`
	EngineVersion     string `json:"engine_version"`
	BaseCurrency      string `json:"base_currency"`
	EvaluationStart   string `json:"evaluation_start"`
	EvaluationEnd     string `json:"evaluation_end"`
	PrimaryStart      string `json:"primary_start"`
	PrimaryEnd        string `json:"primary_end"`
	HorizonMonths     int    `json:"horizon_months"`
	SummaryJSON       string `json:"summary_json"`
	DataQualityJSON   string `json:"data_quality_json"`
	CreatedAt         int64  `json:"created_at"`
	CompletedAt       *int64 `json:"completed_at,omitempty"`
}

type InvestmentPathPoint struct {
	RunID                               string  `json:"run_id"`
	StrategyKey                         string  `json:"strategy_key"`
	ValuationDate                       string  `json:"valuation_date"`
	AccountValueMinor                   int64   `json:"account_value_minor"`
	AssetValueMinor                     int64   `json:"asset_value_minor"`
	CashValueMinor                      int64   `json:"cash_value_minor"`
	CumulativeExternalContributionMinor int64   `json:"cumulative_external_contribution_minor"`
	UnitNAV                             float64 `json:"unit_nav"`
	Drawdown                            float64 `json:"drawdown"`
}

type InvestmentPathTrade struct {
	RunID                string `json:"run_id"`
	StrategyKey          string `json:"strategy_key"`
	SequenceNo           int    `json:"sequence_no"`
	TradeDate            string `json:"trade_date"`
	Side                 string `json:"side"`
	Reason               string `json:"reason"`
	GrossTradeMinor      int64  `json:"gross_trade_minor"`
	FeeMinor             int64  `json:"fee_minor"`
	AssetValueDeltaMinor int64  `json:"asset_value_delta_minor"`
	CashDeltaMinor       int64  `json:"cash_delta_minor"`
}

type InvestmentPathWindow struct {
	RunID                           string   `json:"run_id"`
	StrategyKey                     string   `json:"strategy_key"`
	WindowStart                     string   `json:"window_start"`
	WindowEnd                       string   `json:"window_end"`
	TotalContributionMinor          int64    `json:"total_contribution_minor"`
	TerminalValueMinor              int64    `json:"terminal_value_minor"`
	ProfitMinor                     int64    `json:"profit_minor"`
	XIRR                            *float64 `json:"xirr,omitempty"`
	XIRRReason                      string   `json:"xirr_reason,omitempty"`
	TWRTotal                        float64  `json:"twr_total"`
	TWRAnnualized                   float64  `json:"twr_annualized"`
	MaxDrawdown                     float64  `json:"max_drawdown"`
	MaxDrawdownStart                string   `json:"max_drawdown_start"`
	MaxDrawdownEnd                  string   `json:"max_drawdown_end"`
	LongestUnderwaterDays           int      `json:"longest_underwater_days"`
	MaxPrincipalDeficitMinor        int64    `json:"max_principal_deficit_minor"`
	MaxPrincipalDeficitRatio        float64  `json:"max_principal_deficit_ratio"`
	LongestBelowPrincipalDays       int      `json:"longest_below_principal_days"`
	FirstRecoveryAbovePrincipalDate string   `json:"first_recovery_above_principal_date,omitempty"`
	AverageCashWeight               float64  `json:"average_cash_weight"`
	TotalTransactionCostMinor       int64    `json:"total_transaction_cost_minor"`
	TradeCount                      int      `json:"trade_count"`
	Turnover                        float64  `json:"turnover"`
	DeploymentCompleteDate          string   `json:"deployment_complete_date,omitempty"`
}

type InvestmentPathRepo struct{ db *sql.DB }

func NewInvestmentPathRepo(db *sql.DB) *InvestmentPathRepo { return &InvestmentPathRepo{db: db} }

func (r *InvestmentPathRepo) CreateRunTx(ctx context.Context, tx *sql.Tx, run InvestmentPathRun) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO research_investment_path_runs (
		id,task_id,asset_key,mode,input_hash,source_hash,input_snapshot_json,engine_version,
		base_currency,evaluation_start,evaluation_end,primary_start,primary_end,horizon_months,
		summary_json,data_quality_json,created_at,completed_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.TaskID, run.AssetKey, run.Mode,
		run.InputHash, run.SourceHash, run.InputSnapshotJSON, run.EngineVersion, run.BaseCurrency,
		run.EvaluationStart, run.EvaluationEnd, run.PrimaryStart, run.PrimaryEnd, run.HorizonMonths,
		run.SummaryJSON, run.DataQualityJSON, run.CreatedAt, run.CompletedAt)
	if err != nil {
		return fmt.Errorf("insert investment path run: %w", err)
	}
	return nil
}

const investmentPathRunColumns = `id,task_id,asset_key,mode,input_hash,source_hash,input_snapshot_json,
	engine_version,base_currency,evaluation_start,evaluation_end,primary_start,primary_end,horizon_months,
	summary_json,data_quality_json,created_at,completed_at`

func scanInvestmentPathRun(row rowScanner) (InvestmentPathRun, error) {
	var run InvestmentPathRun
	err := row.Scan(&run.ID, &run.TaskID, &run.AssetKey, &run.Mode, &run.InputHash, &run.SourceHash,
		&run.InputSnapshotJSON, &run.EngineVersion, &run.BaseCurrency, &run.EvaluationStart, &run.EvaluationEnd,
		&run.PrimaryStart, &run.PrimaryEnd, &run.HorizonMonths, &run.SummaryJSON, &run.DataQualityJSON,
		&run.CreatedAt, &run.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return run, ErrInvestmentPathRunNotFound
	}
	if err != nil {
		return run, fmt.Errorf("scan investment path run: %w", err)
	}
	return run, nil
}

func (r *InvestmentPathRepo) GetRun(ctx context.Context, id string) (InvestmentPathRun, error) {
	return scanInvestmentPathRun(r.db.QueryRowContext(ctx, `SELECT `+investmentPathRunColumns+`
		FROM research_investment_path_runs WHERE id=?`, id))
}

func (r *InvestmentPathRepo) GetRunByTaskID(ctx context.Context, taskID string) (InvestmentPathRun, error) {
	return scanInvestmentPathRun(r.db.QueryRowContext(ctx, `SELECT `+investmentPathRunColumns+`
		FROM research_investment_path_runs WHERE task_id=?`, taskID))
}

func (r *InvestmentPathRepo) FindReusable(ctx context.Context, assetKey, inputHash string) (InvestmentPathRun, error) {
	return scanInvestmentPathRun(r.db.QueryRowContext(ctx, `SELECT `+prefixedRunColumns("r")+`
		FROM research_investment_path_runs r JOIN worker_tasks t ON t.id=r.task_id
		WHERE r.asset_key=? AND r.input_hash=? AND t.status IN ('pending','running','pre_complete','complete')
		ORDER BY r.created_at DESC LIMIT 1`, assetKey, inputHash))
}

func (r *InvestmentPathRepo) ListRuns(ctx context.Context, limit, offset int) ([]InvestmentPathRun, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+investmentPathRunColumns+`
		FROM research_investment_path_runs ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list investment path runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []InvestmentPathRun
	for rows.Next() {
		run, scanErr := scanInvestmentPathRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate investment path runs: %w", err)
	}
	return out, nil
}

func prefixedRunColumns(prefix string) string {
	return prefix + ".id," + prefix + ".task_id," + prefix + ".asset_key," + prefix + ".mode," +
		prefix + ".input_hash," + prefix + ".source_hash," + prefix + ".input_snapshot_json," +
		prefix + ".engine_version," + prefix + ".base_currency," + prefix + ".evaluation_start," +
		prefix + ".evaluation_end," + prefix + ".primary_start," + prefix + ".primary_end," +
		prefix + ".horizon_months," + prefix + ".summary_json," + prefix + ".data_quality_json," +
		prefix + ".created_at," + prefix + ".completed_at"
}

func (r *InvestmentPathRepo) CompleteTx(
	ctx context.Context, tx *sql.Tx, runID, summaryJSON, qualityJSON string, completedAt int64,
	points []InvestmentPathPoint, trades []InvestmentPathTrade, windows []InvestmentPathWindow,
) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM research_investment_path_points WHERE run_id=?`, runID); err != nil {
		return fmt.Errorf("clear investment path points: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM research_investment_path_trades WHERE run_id=?`, runID); err != nil {
		return fmt.Errorf("clear investment path trades: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM research_investment_path_windows WHERE run_id=?`, runID); err != nil {
		return fmt.Errorf("clear investment path windows: %w", err)
	}
	for _, p := range points {
		_, err := tx.ExecContext(ctx, `INSERT INTO research_investment_path_points VALUES (?,?,?,?,?,?,?,?,?)`,
			runID, p.StrategyKey, p.ValuationDate, p.AccountValueMinor, p.AssetValueMinor, p.CashValueMinor,
			p.CumulativeExternalContributionMinor, p.UnitNAV, p.Drawdown)
		if err != nil {
			return fmt.Errorf("insert investment path point: %w", err)
		}
	}
	for _, trade := range trades {
		_, err := tx.ExecContext(ctx, `INSERT INTO research_investment_path_trades VALUES (?,?,?,?,?,?,?,?,?,?)`,
			runID, trade.StrategyKey, trade.SequenceNo, trade.TradeDate, trade.Side, trade.Reason,
			trade.GrossTradeMinor, trade.FeeMinor, trade.AssetValueDeltaMinor, trade.CashDeltaMinor)
		if err != nil {
			return fmt.Errorf("insert investment path trade: %w", err)
		}
	}
	for _, w := range windows {
		_, err := tx.ExecContext(ctx, `INSERT INTO research_investment_path_windows VALUES (
			?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, runID, w.StrategyKey, w.WindowStart, w.WindowEnd,
			w.TotalContributionMinor, w.TerminalValueMinor, w.ProfitMinor, w.XIRR, w.XIRRReason, w.TWRTotal,
			w.TWRAnnualized, w.MaxDrawdown, w.MaxDrawdownStart, w.MaxDrawdownEnd, w.LongestUnderwaterDays,
			w.MaxPrincipalDeficitMinor, w.MaxPrincipalDeficitRatio, w.LongestBelowPrincipalDays,
			w.FirstRecoveryAbovePrincipalDate, w.AverageCashWeight, w.TotalTransactionCostMinor, w.TradeCount,
			w.Turnover, w.DeploymentCompleteDate)
		if err != nil {
			return fmt.Errorf("insert investment path window: %w", err)
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE research_investment_path_runs
		SET summary_json=?,data_quality_json=?,completed_at=? WHERE id=?`, summaryJSON, qualityJSON, completedAt, runID)
	if err != nil {
		return fmt.Errorf("complete investment path run: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrInvestmentPathRunNotFound
	}
	return nil
}

func (r *InvestmentPathRepo) ListPoints(ctx context.Context, runID, strategy string) ([]InvestmentPathPoint, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT run_id,strategy_key,valuation_date,account_value_minor,
		asset_value_minor,cash_value_minor,cumulative_external_contribution_minor,unit_nav,drawdown
		FROM research_investment_path_points WHERE run_id=? AND strategy_key=? ORDER BY valuation_date`, runID, strategy)
	if err != nil {
		return nil, fmt.Errorf("list investment path points: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []InvestmentPathPoint
	for rows.Next() {
		var p InvestmentPathPoint
		if err := rows.Scan(&p.RunID, &p.StrategyKey, &p.ValuationDate, &p.AccountValueMinor, &p.AssetValueMinor,
			&p.CashValueMinor, &p.CumulativeExternalContributionMinor, &p.UnitNAV, &p.Drawdown); err != nil {
			return nil, fmt.Errorf("scan investment path point: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate investment path points: %w", err)
	}
	return out, nil
}

func (r *InvestmentPathRepo) ListTrades(ctx context.Context, runID, strategy string) ([]InvestmentPathTrade, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT run_id,strategy_key,sequence_no,trade_date,side,reason,
		gross_trade_minor,fee_minor,asset_value_delta_minor,cash_delta_minor
		FROM research_investment_path_trades WHERE run_id=? AND strategy_key=? ORDER BY sequence_no`, runID, strategy)
	if err != nil {
		return nil, fmt.Errorf("list investment path trades: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []InvestmentPathTrade
	for rows.Next() {
		var trade InvestmentPathTrade
		if err := rows.Scan(
			&trade.RunID, &trade.StrategyKey, &trade.SequenceNo, &trade.TradeDate, &trade.Side,
			&trade.Reason, &trade.GrossTradeMinor, &trade.FeeMinor, &trade.AssetValueDeltaMinor, &trade.CashDeltaMinor,
		); err != nil {
			return nil, fmt.Errorf("scan investment path trade: %w", err)
		}
		out = append(out, trade)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate investment path trades: %w", err)
	}
	return out, nil
}

func (r *InvestmentPathRepo) ListWindows(
	ctx context.Context, runID, strategy string, limit, offset int,
) ([]InvestmentPathWindow, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT run_id,strategy_key,window_start,window_end,total_contribution_minor,
		terminal_value_minor,profit_minor,xirr,xirr_reason,twr_total,twr_annualized,max_drawdown,max_drawdown_start,
		max_drawdown_end,longest_underwater_days,max_principal_deficit_minor,max_principal_deficit_ratio,
		longest_below_principal_days,first_recovery_above_principal_date,average_cash_weight,total_transaction_cost_minor,
		trade_count,turnover,deployment_complete_date FROM research_investment_path_windows
		WHERE run_id=? AND strategy_key=? ORDER BY window_start LIMIT ? OFFSET ?`, runID, strategy, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list investment path windows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []InvestmentPathWindow
	for rows.Next() {
		var w InvestmentPathWindow
		var xirr sql.NullFloat64
		if err := rows.Scan(&w.RunID, &w.StrategyKey, &w.WindowStart, &w.WindowEnd, &w.TotalContributionMinor,
			&w.TerminalValueMinor, &w.ProfitMinor, &xirr, &w.XIRRReason, &w.TWRTotal, &w.TWRAnnualized,
			&w.MaxDrawdown, &w.MaxDrawdownStart, &w.MaxDrawdownEnd, &w.LongestUnderwaterDays,
			&w.MaxPrincipalDeficitMinor, &w.MaxPrincipalDeficitRatio, &w.LongestBelowPrincipalDays,
			&w.FirstRecoveryAbovePrincipalDate, &w.AverageCashWeight, &w.TotalTransactionCostMinor,
			&w.TradeCount, &w.Turnover, &w.DeploymentCompleteDate); err != nil {
			return nil, fmt.Errorf("scan investment path window: %w", err)
		}
		if xirr.Valid {
			w.XIRR = &xirr.Float64
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate investment path windows: %w", err)
	}
	return out, nil
}
