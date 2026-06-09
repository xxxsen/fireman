package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// SimulationRun is a persisted Monte Carlo result.
type SimulationRun struct {
	ID                 string          `json:"id"`
	JobID              string          `json:"job_id"`
	PlanID             string          `json:"plan_id"`
	InputHash          string          `json:"input_hash"`
	InputSnapshotJSON  string          `json:"input_snapshot_json"`
	MarketSnapshotHash string          `json:"market_snapshot_hash"`
	EngineVersion      string          `json:"engine_version"`
	Runs               int             `json:"runs"`
	Seed               int64           `json:"seed"`
	HorizonMonths      int             `json:"horizon_months"`
	SuccessCount       int             `json:"success_count"`
	FailureCount       int             `json:"failure_count"`
	SummaryJSON        json.RawMessage `json:"summary_json"`
	CreatedAt          int64           `json:"created_at"`
}

// PathIndexRow is one path summary in simulation_path_index.
type PathIndexRow struct {
	RunID                    string  `json:"run_id"`
	PathNo                   int     `json:"path_no"`
	PathSeed                 int64   `json:"path_seed"`
	Succeeded                bool    `json:"succeeded"`
	FailureMonth             *int    `json:"failure_month,omitempty"`
	TerminalWealthMinor      int64   `json:"terminal_wealth_minor"`
	MaxDrawdown              float64 `json:"max_drawdown"`
	RepresentativePercentile string  `json:"representative_percentile,omitempty"`
}

// QuantileSeriesRow is one month in simulation_quantile_series.
type QuantileSeriesRow struct {
	RunID       string
	MonthOffset int
	P00Minor    int64
	P05Minor    int64
	P25Minor    int64
	P50Minor    int64
	P75Minor    int64
	P95Minor    int64
}

// SimulationRepo persists simulation outputs.
type SimulationRepo struct {
	db *sql.DB
}

func NewSimulationRepo(db *sql.DB) *SimulationRepo {
	return &SimulationRepo{db: db}
}

func (r *SimulationRepo) CreatePending(ctx context.Context, tx *sql.Tx, run SimulationRun) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	if run.CreatedAt == 0 {
		run.CreatedAt = now
	}
	if len(run.SummaryJSON) == 0 {
		run.SummaryJSON = json.RawMessage(`{}`)
	}
	_, err := exec.ExecContext(ctx, `
		INSERT INTO simulation_runs (
			id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count,
			summary_json, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.JobID, run.PlanID, run.InputHash, run.InputSnapshotJSON, run.MarketSnapshotHash,
		run.EngineVersion, run.Runs, run.Seed, run.HorizonMonths, run.SuccessCount, run.FailureCount,
		string(run.SummaryJSON), run.CreatedAt)
	return err
}

func (r *SimulationRepo) Complete(ctx context.Context, tx *sql.Tx, runID string, success, failure int, summary json.RawMessage) error {
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx, `
		UPDATE simulation_runs SET success_count=?, failure_count=?, summary_json=? WHERE id=?`,
		success, failure, string(summary), runID)
	return err
}

func (r *SimulationRepo) GetByJobID(ctx context.Context, jobID string) (SimulationRun, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count,
			summary_json, created_at
		FROM simulation_runs WHERE job_id=?`, jobID)
	return scanSimulationRun(row)
}

func (r *SimulationRepo) GetByID(ctx context.Context, id string) (SimulationRun, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count,
			summary_json, created_at
		FROM simulation_runs WHERE id=?`, id)
	return scanSimulationRun(row)
}

func (r *SimulationRepo) ListByPlan(ctx context.Context, planID string, limit int) ([]SimulationRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count,
			summary_json, created_at
		FROM simulation_runs WHERE plan_id=? ORDER BY created_at DESC LIMIT ?`, planID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SimulationRun
	for rows.Next() {
		run, err := scanSimulationRunRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *SimulationRepo) ReplacePathIndex(ctx context.Context, tx *sql.Tx, runID string, paths []PathIndexRow) error {
	exec := r.exec(tx)
	if _, err := exec.ExecContext(ctx, `DELETE FROM simulation_path_index WHERE run_id=?`, runID); err != nil {
		return err
	}
	for _, p := range paths {
		var failMonth any
		if p.FailureMonth != nil {
			failMonth = *p.FailureMonth
		}
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO simulation_path_index (
				run_id, path_no, path_seed, succeeded, failure_month,
				terminal_wealth_minor, max_drawdown, representative_percentile
			) VALUES (?,?,?,?,?,?,?,?)`,
			runID, p.PathNo, p.PathSeed, boolToInt(p.Succeeded), failMonth,
			p.TerminalWealthMinor, p.MaxDrawdown, p.RepresentativePercentile); err != nil {
			return err
		}
	}
	return nil
}

func (r *SimulationRepo) ReplaceQuantileSeries(ctx context.Context, tx *sql.Tx, runID string, series []QuantileSeriesRow) error {
	exec := r.exec(tx)
	if _, err := exec.ExecContext(ctx, `DELETE FROM simulation_quantile_series WHERE run_id=?`, runID); err != nil {
		return err
	}
	for _, q := range series {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO simulation_quantile_series (
				run_id, month_offset, p00_minor, p05_minor, p25_minor, p50_minor, p75_minor, p95_minor
			) VALUES (?,?,?,?,?,?,?,?)`,
			runID, q.MonthOffset, q.P00Minor, q.P05Minor, q.P25Minor, q.P50Minor, q.P75Minor, q.P95Minor); err != nil {
			return err
		}
	}
	return nil
}

func (r *SimulationRepo) ListQuantileSeries(ctx context.Context, runID string) ([]QuantileSeriesRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT run_id, month_offset, p00_minor, p05_minor, p25_minor, p50_minor, p75_minor, p95_minor
		FROM simulation_quantile_series WHERE run_id=? ORDER BY month_offset`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuantileSeriesRow
	for rows.Next() {
		var q QuantileSeriesRow
		if err := rows.Scan(&q.RunID, &q.MonthOffset, &q.P00Minor, &q.P05Minor, &q.P25Minor, &q.P50Minor, &q.P75Minor, &q.P95Minor); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (r *SimulationRepo) ListPathIndex(ctx context.Context, runID string) ([]PathIndexRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT run_id, path_no, path_seed, succeeded, failure_month,
			terminal_wealth_minor, max_drawdown, representative_percentile
		FROM simulation_path_index WHERE run_id=? ORDER BY path_no`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PathIndexRow
	for rows.Next() {
		var p PathIndexRow
		var ok int
		var fail sql.NullInt64
		if err := rows.Scan(&p.RunID, &p.PathNo, &p.PathSeed, &ok, &fail,
			&p.TerminalWealthMinor, &p.MaxDrawdown, &p.RepresentativePercentile); err != nil {
			return nil, err
		}
		p.Succeeded = ok == 1
		if fail.Valid {
			v := int(fail.Int64)
			p.FailureMonth = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *SimulationRepo) GetPathIndex(ctx context.Context, runID string, pathNo int) (PathIndexRow, error) {
	var p PathIndexRow
	var ok int
	var fail sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT run_id, path_no, path_seed, succeeded, failure_month,
			terminal_wealth_minor, max_drawdown, representative_percentile
		FROM simulation_path_index WHERE run_id=? AND path_no=?`, runID, pathNo).Scan(
		&p.RunID, &p.PathNo, &p.PathSeed, &ok, &fail,
		&p.TerminalWealthMinor, &p.MaxDrawdown, &p.RepresentativePercentile)
	if errors.Is(err, sql.ErrNoRows) {
		return PathIndexRow{}, ErrSimulationNotFound
	}
	if err != nil {
		return PathIndexRow{}, err
	}
	p.Succeeded = ok == 1
	if fail.Valid {
		v := int(fail.Int64)
		p.FailureMonth = &v
	}
	return p, nil
}

func scanSimulationRun(row *sql.Row) (SimulationRun, error) {
	var run SimulationRun
	var summary string
	err := row.Scan(
		&run.ID, &run.JobID, &run.PlanID, &run.InputHash, &run.InputSnapshotJSON, &run.MarketSnapshotHash,
		&run.EngineVersion, &run.Runs, &run.Seed, &run.HorizonMonths, &run.SuccessCount, &run.FailureCount,
		&summary, &run.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SimulationRun{}, ErrSimulationNotFound
	}
	if err != nil {
		return SimulationRun{}, err
	}
	run.SummaryJSON = json.RawMessage(summary)
	return run, nil
}

func scanSimulationRunRows(rows *sql.Rows) (SimulationRun, error) {
	var run SimulationRun
	var summary string
	err := rows.Scan(
		&run.ID, &run.JobID, &run.PlanID, &run.InputHash, &run.InputSnapshotJSON, &run.MarketSnapshotHash,
		&run.EngineVersion, &run.Runs, &run.Seed, &run.HorizonMonths, &run.SuccessCount, &run.FailureCount,
		&summary, &run.CreatedAt)
	if err != nil {
		return SimulationRun{}, err
	}
	run.SummaryJSON = json.RawMessage(summary)
	return run, nil
}

func (r *SimulationRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

var ErrSimulationNotFound = errors.New("simulation not found")
