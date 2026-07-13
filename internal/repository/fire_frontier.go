package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrFireFrontierNotFound = errors.New("fire frontier run not found")

type FireFrontierRun struct {
	ID                    string          `json:"id"`
	TaskID                string          `json:"task_id"`
	PlanID                string          `json:"plan_id"`
	SourceSimulationRunID string          `json:"source_simulation_run_id"`
	InputHash             string          `json:"input_hash"`
	AlgorithmVersion      string          `json:"algorithm_version"`
	FrontierType          string          `json:"frontier_type"`
	SourceEngineVersion   string          `json:"source_engine_version"`
	SourceConfigHash      string          `json:"source_config_hash"`
	SourceMarketHash      string          `json:"source_market_hash"`
	EvaluationRuns        int             `json:"evaluation_runs"`
	ConfigJSON            string          `json:"config_json"`
	InputSnapshotJSON     string          `json:"input_snapshot_json"`
	ResultJSON            json.RawMessage `json:"result_json"`
	CreatedAt             int64           `json:"created_at"`
	CompletedAt           *int64          `json:"completed_at,omitempty"`
	TaskStatus            string          `json:"task_status"`
	TaskProgressCurrent   int             `json:"task_progress_current"`
	TaskProgressTotal     int             `json:"task_progress_total"`
	TaskPhase             string          `json:"task_phase"`
	TaskAttemptCount      int             `json:"task_attempt_count"`
	TaskErrorCode         string          `json:"task_error_code,omitempty"`
	TaskErrorMessage      string          `json:"task_error_message,omitempty"`
}

type FireFrontierApplication struct {
	ID                  string `json:"id"`
	FrontierRunID       string `json:"frontier_run_id"`
	PointID             string `json:"point_id"`
	PlanID              string `json:"plan_id"`
	BeforeConfigVersion int    `json:"before_config_version"`
	AfterConfigVersion  int    `json:"after_config_version"`
	PreviewHash         string `json:"preview_hash"`
	BeforeJSON          string `json:"before_json"`
	AfterJSON           string `json:"after_json"`
	AppliedAt           int64  `json:"applied_at"`
}

type FireFrontierRepo struct{ db *sql.DB }

func NewFireFrontierRepo(db *sql.DB) *FireFrontierRepo { return &FireFrontierRepo{db: db} }

func (r *FireFrontierRepo) CreateTx(ctx context.Context, tx *sql.Tx, run *FireFrontierRun) error {
	if run.CreatedAt == 0 {
		run.CreatedAt = time.Now().UnixMilli()
	}
	if len(run.ResultJSON) == 0 {
		run.ResultJSON = json.RawMessage(`{}`)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO fire_frontier_runs (
		id,task_id,plan_id,source_simulation_run_id,input_hash,algorithm_version,frontier_type,
		source_engine_version,source_config_hash,source_market_hash,evaluation_runs,config_json,
		input_snapshot_json,result_json,created_at,completed_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.TaskID, run.PlanID,
		run.SourceSimulationRunID, run.InputHash, run.AlgorithmVersion, run.FrontierType,
		run.SourceEngineVersion, run.SourceConfigHash, run.SourceMarketHash, run.EvaluationRuns,
		run.ConfigJSON, run.InputSnapshotJSON, string(run.ResultJSON), run.CreatedAt, run.CompletedAt)
	return wrapSQL("create fire frontier run", err)
}

func (r *FireFrontierRepo) CompleteTx(ctx context.Context, tx *sql.Tx, taskID string,
	result json.RawMessage, completedAt int64,
) error {
	res, err := tx.ExecContext(ctx, `UPDATE fire_frontier_runs SET result_json=?,completed_at=?
		WHERE task_id=? AND completed_at IS NULL`, string(result), completedAt, taskID)
	if err != nil {
		return wrapSQL("complete fire frontier run", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return wrapSQL("count completed fire frontier run", err)
	}
	if affected != 1 {
		return ErrFireFrontierNotFound
	}
	return nil
}

func (r *FireFrontierRepo) MarkCanceledByTaskTx(ctx context.Context, tx *sql.Tx, taskID string,
	completedAt int64,
) error {
	_, err := tx.ExecContext(ctx, `UPDATE fire_frontier_runs
		SET completed_at=COALESCE(completed_at,?) WHERE task_id=?`, completedAt, taskID)
	return wrapSQL("mark canceled fire frontier run", err)
}

func (r *FireFrontierRepo) MarkTerminalAndPruneByTaskTx(ctx context.Context, tx *sql.Tx,
	taskID string, completedAt int64, keep int,
) error {
	var planID string
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM fire_frontier_runs WHERE task_id=?`, taskID).
		Scan(&planID); err != nil {
		return wrapSQL("load terminal fire frontier plan", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fire_frontier_runs
		SET completed_at=COALESCE(completed_at,?) WHERE task_id=?`, completedAt, taskID); err != nil {
		return wrapSQL("mark terminal fire frontier run", err)
	}
	return r.PruneTx(ctx, tx, planID, keep)
}

func (r *FireFrontierRepo) MarkCanceledAndPruneByTaskTx(ctx context.Context, tx *sql.Tx,
	taskID string, completedAt int64, keep int,
) error {
	var planID string
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM fire_frontier_runs WHERE task_id=?`, taskID).
		Scan(&planID); err != nil {
		return wrapSQL("load canceled fire frontier plan", err)
	}
	if err := r.MarkCanceledByTaskTx(ctx, tx, taskID, completedAt); err != nil {
		return err
	}
	return r.PruneTx(ctx, tx, planID, keep)
}

const fireFrontierSelect = `SELECT r.id,r.task_id,r.plan_id,r.source_simulation_run_id,
	r.input_hash,r.algorithm_version,r.frontier_type,r.source_engine_version,r.source_config_hash,
	r.source_market_hash,r.evaluation_runs,r.config_json,r.input_snapshot_json,r.result_json,
	r.created_at,r.completed_at,COALESCE(t.status,'unknown'),COALESCE(t.progress_current,0),
	COALESCE(t.progress_total,0),COALESCE(t.phase,''),COALESCE(t.attempt_count,0),
	COALESCE(t.error_code,''),COALESCE(t.error_message,'')
	FROM fire_frontier_runs r LEFT JOIN worker_tasks t ON t.id=r.task_id`

func (r *FireFrontierRepo) GetByID(ctx context.Context, id string) (FireFrontierRun, error) {
	return scanFireFrontier(r.db.QueryRowContext(ctx, fireFrontierSelect+` WHERE r.id=?`, id))
}

func (r *FireFrontierRepo) GetByTaskID(ctx context.Context, taskID string) (FireFrontierRun, error) {
	return scanFireFrontier(r.db.QueryRowContext(ctx, fireFrontierSelect+` WHERE r.task_id=?`, taskID))
}

func (r *FireFrontierRepo) FindReusable(ctx context.Context, planID, inputHash string) (FireFrontierRun, error) {
	return scanFireFrontier(r.db.QueryRowContext(ctx, fireFrontierSelect+`
		WHERE r.plan_id=? AND r.input_hash=? AND t.status IN ('pending','running','pre_complete','complete')
		ORDER BY r.created_at DESC,r.id DESC LIMIT 1`, planID, inputHash))
}

func (r *FireFrontierRepo) ListByPlan(ctx context.Context, planID string, limit, offset int) (
	[]FireFrontierRun, int, error,
) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return queryPage(ctx, r.db, `SELECT COUNT(*) FROM fire_frontier_runs WHERE plan_id=?`,
		fireFrontierSelect+` WHERE r.plan_id=? ORDER BY r.created_at DESC,r.id DESC LIMIT ? OFFSET ?`,
		[]any{planID}, limit, offset, scanFireFrontierRows,
		"count frontier runs", "list frontier runs", "scan frontier run", "iterate frontier runs")
}

func (r *FireFrontierRepo) CreateApplicationTx(ctx context.Context, tx *sql.Tx,
	app FireFrontierApplication,
) error {
	if app.AppliedAt == 0 {
		app.AppliedAt = time.Now().UnixMilli()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO fire_frontier_applications (
		id,frontier_run_id,point_id,plan_id,before_config_version,after_config_version,
		preview_hash,before_json,after_json,applied_at
	) VALUES (?,?,?,?,?,?,?,?,?,?)`, app.ID, app.FrontierRunID, app.PointID, app.PlanID,
		app.BeforeConfigVersion, app.AfterConfigVersion, app.PreviewHash, app.BeforeJSON,
		app.AfterJSON, app.AppliedAt)
	return wrapSQL("create fire frontier application", err)
}

func (r *FireFrontierRepo) GetApplication(ctx context.Context, runID string) (FireFrontierApplication, error) {
	var app FireFrontierApplication
	err := r.db.QueryRowContext(ctx, `SELECT id,frontier_run_id,point_id,plan_id,
		before_config_version,after_config_version,preview_hash,before_json,after_json,applied_at
		FROM fire_frontier_applications WHERE frontier_run_id=?`, runID).Scan(
		&app.ID, &app.FrontierRunID, &app.PointID, &app.PlanID, &app.BeforeConfigVersion,
		&app.AfterConfigVersion, &app.PreviewHash, &app.BeforeJSON, &app.AfterJSON, &app.AppliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FireFrontierApplication{}, ErrFireFrontierNotFound
	}
	return app, wrapSQL("get fire frontier application", err)
}

// PruneTx retains the newest terminal, unapplied runs. Active and applied
// runs are excluded from both the count and deletion set.
func (r *FireFrontierRepo) PruneTx(ctx context.Context, tx *sql.Tx, planID string, keep int) error {
	if keep < 0 {
		keep = 0
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM fire_frontier_runs WHERE id IN (
		SELECT r.id FROM fire_frontier_runs r
		JOIN worker_tasks t ON t.id=r.task_id
		LEFT JOIN fire_frontier_applications a ON a.frontier_run_id=r.id
		WHERE r.plan_id=? AND t.status IN ('complete','failed','canceled') AND a.id IS NULL
		ORDER BY r.created_at DESC,r.id DESC LIMIT -1 OFFSET ?
	)`, planID, keep)
	return wrapSQL("prune fire frontier runs", err)
}

type frontierRow interface{ Scan(...any) error }

func scanFireFrontier(row frontierRow) (FireFrontierRun, error) {
	var run FireFrontierRun
	var result string
	err := row.Scan(&run.ID, &run.TaskID, &run.PlanID, &run.SourceSimulationRunID,
		&run.InputHash, &run.AlgorithmVersion, &run.FrontierType, &run.SourceEngineVersion,
		&run.SourceConfigHash, &run.SourceMarketHash, &run.EvaluationRuns, &run.ConfigJSON,
		&run.InputSnapshotJSON, &result, &run.CreatedAt, &run.CompletedAt, &run.TaskStatus,
		&run.TaskProgressCurrent, &run.TaskProgressTotal, &run.TaskPhase, &run.TaskAttemptCount,
		&run.TaskErrorCode, &run.TaskErrorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return FireFrontierRun{}, ErrFireFrontierNotFound
	}
	if err != nil {
		return FireFrontierRun{}, wrapSQL("scan fire frontier run", err)
	}
	run.ResultJSON = json.RawMessage(result)
	return run, nil
}

func scanFireFrontierRows(rows *sql.Rows) (FireFrontierRun, error) { return scanFireFrontier(rows) }
