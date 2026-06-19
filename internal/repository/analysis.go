package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	AnalysisTypeStress      = "stress"
	AnalysisTypeSensitivity = "sensitivity"
)

// AnalysisResult stores stress/sensitivity job output.
type AnalysisResult struct {
	JobID           string `json:"job_id"`
	PlanID          string `json:"plan_id"`
	Type            string `json:"type"`
	InputHash       string `json:"input_hash"`
	SimulationRunID string `json:"simulation_run_id"`
	ResultJSON      string `json:"result_json"`
	CreatedAt       int64  `json:"created_at"`
}

// AnalysisRepo manages analysis_results.
type AnalysisRepo struct {
	db *sql.DB
}

func NewAnalysisRepo(db *sql.DB) *AnalysisRepo {
	return &AnalysisRepo{db: db}
}

func (r *AnalysisRepo) CreatePending(ctx context.Context, tx *sql.Tx, rec AnalysisResult) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	_, err := exec.ExecContext(ctx, `
		INSERT INTO analysis_results (job_id, plan_id, type, input_hash, simulation_run_id, result_json, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		rec.JobID, rec.PlanID, rec.Type, rec.InputHash, rec.SimulationRunID, rec.ResultJSON, rec.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert analysis result: %w", err)
	}
	return nil
}

func (r *AnalysisRepo) Complete(ctx context.Context, jobID, resultJSON string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE analysis_results SET result_json=? WHERE job_id=?`, resultJSON, jobID)
	if err != nil {
		return fmt.Errorf("complete analysis result: %w", err)
	}
	return nil
}

func (r *AnalysisRepo) GetByJobID(ctx context.Context, jobID string) (AnalysisResult, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT job_id, plan_id, type, input_hash, simulation_run_id, result_json, created_at
		FROM analysis_results WHERE job_id=?`, jobID)
	var rec AnalysisResult
	err := row.Scan(&rec.JobID, &rec.PlanID, &rec.Type, &rec.InputHash, &rec.SimulationRunID,
		&rec.ResultJSON, &rec.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AnalysisResult{}, ErrAnalysisNotFound
	}
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("scan analysis result: %w", err)
	}
	return rec, nil
}

func (r *AnalysisRepo) ListByPlan(
	ctx context.Context,
	planID, typ string,
	limit int,
) ([]AnalysisResult, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT job_id, plan_id, type, input_hash, simulation_run_id, result_json, created_at
		FROM analysis_results WHERE plan_id=? AND type=? ORDER BY created_at DESC LIMIT ?`,
		planID, typ, limit)
	if err != nil {
		return nil, fmt.Errorf("query analysis results: %w", err)
	}
	return scanAnalysisRows(rows)
}

// ListBySimulationRun returns analysis results of a type bound to one simulation run.
func (r *AnalysisRepo) ListBySimulationRun(
	ctx context.Context,
	runID, typ string,
	limit int,
) ([]AnalysisResult, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT job_id, plan_id, type, input_hash, simulation_run_id, result_json, created_at
		FROM analysis_results WHERE simulation_run_id=? AND type=? ORDER BY created_at DESC LIMIT ?`,
		runID, typ, limit)
	if err != nil {
		return nil, fmt.Errorf("query analysis results by run: %w", err)
	}
	return scanAnalysisRows(rows)
}

// DeleteBySimulationRunAndType removes existing analysis of one type for a run,
// enforcing "keep only the latest stress/sensitivity per Monte Carlo run".
func (r *AnalysisRepo) DeleteBySimulationRunAndType(ctx context.Context, tx *sql.Tx, runID, typ string) error {
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM analysis_results WHERE simulation_run_id=? AND type=?`, runID, typ)
	if err != nil {
		return fmt.Errorf("delete analysis by run and type: %w", err)
	}
	return nil
}

// DeleteBySimulationRunIDs removes all analysis results attached to pruned runs.
func (r *AnalysisRepo) DeleteBySimulationRunIDs(ctx context.Context, tx *sql.Tx, runIDs []string) error {
	if len(runIDs) == 0 {
		return nil
	}
	exec := r.exec(tx)
	placeholders := make([]byte, 0, len(runIDs)*2)
	args := make([]any, 0, len(runIDs))
	for i, id := range runIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, id)
	}
	query := `DELETE FROM analysis_results WHERE simulation_run_id IN (` + string(placeholders) + `)`
	if _, err := exec.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete analysis by run ids: %w", err)
	}
	return nil
}

func scanAnalysisRows(rows *sql.Rows) ([]AnalysisResult, error) {
	defer func() { _ = rows.Close() }()
	var out []AnalysisResult
	for rows.Next() {
		var rec AnalysisResult
		if err := rows.Scan(
			&rec.JobID, &rec.PlanID, &rec.Type, &rec.InputHash, &rec.SimulationRunID,
			&rec.ResultJSON, &rec.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan analysis result row: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate analysis results: %w", err)
	}
	return out, nil
}

func (r *AnalysisRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

var ErrAnalysisNotFound = errors.New("analysis result not found")
