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
	JobID      string `json:"job_id"`
	PlanID     string `json:"plan_id"`
	Type       string `json:"type"`
	InputHash  string `json:"input_hash"`
	ResultJSON string `json:"result_json"`
	CreatedAt  int64  `json:"created_at"`
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
		INSERT INTO analysis_results (job_id, plan_id, type, input_hash, result_json, created_at)
		VALUES (?,?,?,?,?,?)`,
		rec.JobID, rec.PlanID, rec.Type, rec.InputHash, rec.ResultJSON, rec.CreatedAt)
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
		SELECT job_id, plan_id, type, input_hash, result_json, created_at
		FROM analysis_results WHERE job_id=?`, jobID)
	var rec AnalysisResult
	err := row.Scan(&rec.JobID, &rec.PlanID, &rec.Type, &rec.InputHash, &rec.ResultJSON, &rec.CreatedAt)
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
		SELECT job_id, plan_id, type, input_hash, result_json, created_at
		FROM analysis_results WHERE plan_id=? AND type=? ORDER BY created_at DESC LIMIT ?`,
		planID, typ, limit)
	if err != nil {
		return nil, fmt.Errorf("query analysis results: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AnalysisResult
	for rows.Next() {
		var rec AnalysisResult
		if err := rows.Scan(
			&rec.JobID, &rec.PlanID, &rec.Type, &rec.InputHash, &rec.ResultJSON, &rec.CreatedAt,
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
