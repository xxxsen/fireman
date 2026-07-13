package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrFirePlanImprovementNotFound = errors.New("fire plan improvement run not found")

type FirePlanImprovementRun struct {
	ID                    string          `json:"id"`
	TaskID                string          `json:"task_id"`
	PlanID                string          `json:"plan_id"`
	SourceSimulationRunID string          `json:"source_simulation_run_id"`
	InputHash             string          `json:"input_hash"`
	AlgorithmVersion      string          `json:"algorithm_version"`
	SourceEngineVersion   string          `json:"source_engine_version"`
	SourceConfigHash      string          `json:"source_config_hash"`
	SourceMarketHash      string          `json:"source_market_hash"`
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

type FirePlanImprovementApplication struct {
	ID                  string `json:"id"`
	ImprovementRunID    string `json:"improvement_run_id"`
	ProposalID          string `json:"proposal_id"`
	PlanID              string `json:"plan_id"`
	BeforeConfigVersion int    `json:"before_config_version"`
	AfterConfigVersion  int    `json:"after_config_version"`
	PreviewHash         string `json:"preview_hash"`
	BeforeJSON          string `json:"before_json"`
	AfterJSON           string `json:"after_json"`
	AppliedAt           int64  `json:"applied_at"`
}

type FirePlanImprovementRepo struct{ db *sql.DB }

func NewFirePlanImprovementRepo(db *sql.DB) *FirePlanImprovementRepo {
	return &FirePlanImprovementRepo{db: db}
}

func (r *FirePlanImprovementRepo) CreateTx(
	ctx context.Context, tx *sql.Tx, run *FirePlanImprovementRun,
) error {
	if run.CreatedAt == 0 {
		run.CreatedAt = time.Now().UnixMilli()
	}
	if len(run.ResultJSON) == 0 {
		run.ResultJSON = json.RawMessage(`{}`)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO fire_plan_improvement_runs (
		id,task_id,plan_id,source_simulation_run_id,input_hash,algorithm_version,
		source_engine_version,source_config_hash,source_market_hash,config_json,
		input_snapshot_json,result_json,created_at,completed_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.TaskID, run.PlanID, run.SourceSimulationRunID, run.InputHash,
		run.AlgorithmVersion, run.SourceEngineVersion, run.SourceConfigHash,
		run.SourceMarketHash, run.ConfigJSON, run.InputSnapshotJSON,
		string(run.ResultJSON), run.CreatedAt, run.CompletedAt)
	return wrapSQL("create fire plan improvement run", err)
}

func (r *FirePlanImprovementRepo) CompleteTx(
	ctx context.Context, tx *sql.Tx, taskID string, result json.RawMessage, completedAt int64,
) error {
	res, err := tx.ExecContext(ctx, `UPDATE fire_plan_improvement_runs
		SET result_json=?,completed_at=? WHERE task_id=? AND completed_at IS NULL`,
		string(result), completedAt, taskID)
	if err != nil {
		return wrapSQL("complete fire plan improvement run", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return wrapSQL("count completed fire plan improvement run", err)
	}
	if affected != 1 {
		return ErrFirePlanImprovementNotFound
	}
	return nil
}

const firePlanImprovementSelect = `SELECT r.id,r.task_id,r.plan_id,r.source_simulation_run_id,
	r.input_hash,r.algorithm_version,r.source_engine_version,r.source_config_hash,
	r.source_market_hash,r.config_json,r.input_snapshot_json,r.result_json,r.created_at,r.completed_at,
	COALESCE(t.status,'unknown'),COALESCE(t.progress_current,0),COALESCE(t.progress_total,0),
	COALESCE(t.phase,''),COALESCE(t.attempt_count,0),COALESCE(t.error_code,''),COALESCE(t.error_message,'')
	FROM fire_plan_improvement_runs r LEFT JOIN worker_tasks t ON t.id=r.task_id`

func (r *FirePlanImprovementRepo) GetByID(ctx context.Context, id string) (FirePlanImprovementRun, error) {
	return scanFirePlanImprovement(r.db.QueryRowContext(ctx, firePlanImprovementSelect+` WHERE r.id=?`, id))
}

func (r *FirePlanImprovementRepo) GetByTaskID(ctx context.Context, taskID string) (FirePlanImprovementRun, error) {
	return scanFirePlanImprovement(r.db.QueryRowContext(ctx, firePlanImprovementSelect+` WHERE r.task_id=?`, taskID))
}

func (r *FirePlanImprovementRepo) FindReusable(
	ctx context.Context, planID, inputHash string,
) (FirePlanImprovementRun, error) {
	return scanFirePlanImprovement(r.db.QueryRowContext(ctx, firePlanImprovementSelect+`
		WHERE r.plan_id=? AND r.input_hash=? AND t.status IN ('pending','running','pre_complete','complete')
		ORDER BY r.created_at DESC LIMIT 1`, planID, inputHash))
}

func (r *FirePlanImprovementRepo) ListByPlan(
	ctx context.Context, planID string, limit, offset int,
) ([]FirePlanImprovementRun, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, total, err := queryPage(ctx, r.db,
		`SELECT COUNT(*) FROM fire_plan_improvement_runs WHERE plan_id=?`,
		firePlanImprovementSelect+` WHERE r.plan_id=? ORDER BY r.created_at DESC,r.id DESC LIMIT ? OFFSET ?`,
		[]any{planID}, limit, offset, scanFirePlanImprovementRows,
		"count improvement runs", "list improvement runs", "scan improvement run", "iterate improvement runs")
	return rows, total, err
}

func (r *FirePlanImprovementRepo) CreateApplicationTx(
	ctx context.Context, tx *sql.Tx, app FirePlanImprovementApplication,
) error {
	if app.AppliedAt == 0 {
		app.AppliedAt = time.Now().UnixMilli()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO fire_plan_improvement_applications (
		id,improvement_run_id,proposal_id,plan_id,before_config_version,after_config_version,
		preview_hash,before_json,after_json,applied_at
	) VALUES (?,?,?,?,?,?,?,?,?,?)`, app.ID, app.ImprovementRunID, app.ProposalID, app.PlanID,
		app.BeforeConfigVersion, app.AfterConfigVersion, app.PreviewHash, app.BeforeJSON,
		app.AfterJSON, app.AppliedAt)
	return wrapSQL("create fire plan improvement application", err)
}

func (r *FirePlanImprovementRepo) GetApplication(
	ctx context.Context, runID string,
) (FirePlanImprovementApplication, error) {
	var app FirePlanImprovementApplication
	err := r.db.QueryRowContext(ctx, `SELECT id,improvement_run_id,proposal_id,plan_id,
		before_config_version,after_config_version,preview_hash,before_json,after_json,applied_at
		FROM fire_plan_improvement_applications WHERE improvement_run_id=? ORDER BY applied_at DESC LIMIT 1`,
		runID).Scan(&app.ID, &app.ImprovementRunID, &app.ProposalID, &app.PlanID,
		&app.BeforeConfigVersion, &app.AfterConfigVersion, &app.PreviewHash, &app.BeforeJSON,
		&app.AfterJSON, &app.AppliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FirePlanImprovementApplication{}, ErrFirePlanImprovementNotFound
	}
	return app, wrapSQL("get fire plan improvement application", err)
}

func (r *FirePlanImprovementRepo) PruneTx(ctx context.Context, tx *sql.Tx, planID string, keep int) error {
	if keep < 0 {
		keep = 0
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM fire_plan_improvement_runs WHERE id IN (
		SELECT r.id FROM fire_plan_improvement_runs r
		JOIN worker_tasks t ON t.id=r.task_id
		LEFT JOIN fire_plan_improvement_applications a ON a.improvement_run_id=r.id
		WHERE r.plan_id=? AND t.status IN ('complete','failed','canceled') AND a.id IS NULL
		ORDER BY r.created_at DESC,r.id DESC LIMIT -1 OFFSET ?
	)`, planID, keep)
	return wrapSQL("prune fire plan improvement runs", err)
}

type improvementRow interface{ Scan(...any) error }

func scanFirePlanImprovement(row improvementRow) (FirePlanImprovementRun, error) {
	var run FirePlanImprovementRun
	var result string
	err := row.Scan(&run.ID, &run.TaskID, &run.PlanID, &run.SourceSimulationRunID,
		&run.InputHash, &run.AlgorithmVersion, &run.SourceEngineVersion, &run.SourceConfigHash,
		&run.SourceMarketHash, &run.ConfigJSON, &run.InputSnapshotJSON, &result, &run.CreatedAt,
		&run.CompletedAt, &run.TaskStatus, &run.TaskProgressCurrent, &run.TaskProgressTotal,
		&run.TaskPhase, &run.TaskAttemptCount, &run.TaskErrorCode, &run.TaskErrorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return FirePlanImprovementRun{}, ErrFirePlanImprovementNotFound
	}
	if err != nil {
		return FirePlanImprovementRun{}, wrapSQL("scan fire plan improvement run", err)
	}
	run.ResultJSON = json.RawMessage(result)
	return run, nil
}

func scanFirePlanImprovementRows(rows *sql.Rows) (FirePlanImprovementRun, error) {
	return scanFirePlanImprovement(rows)
}
