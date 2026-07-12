package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	JobTypeSimulation           = "simulation"
	JobTypeStress               = "stress"
	JobTypeSensitivity          = "sensitivity"
	JobTypeResearchBacktest     = "research_backtest"
	JobTypeResearchOptimization = "research_optimization_backtest"
	JobStatusQueued             = "queued"
	JobStatusRunning            = "running"
	JobStatusSucceeded          = "succeeded"
	JobStatusFailed             = "failed"
	JobStatusCanceled           = "canceled"
	JobErrorWorkerInterrupted   = "worker_interrupted"
)

const (
	InterruptedActionRequeued = "requeued"
	InterruptedActionFailed   = "failed"
	InterruptedActionCanceled = "canceled"
)

// InterruptedJobAction is the committed transition for one orphaned running
// job. Job contains the row as it existed before reconciliation.
type InterruptedJobAction struct {
	Action string
	Job    Job
}

// JobErrSupersededByNewerAnalysis marks an attached-analysis job canceled because
// the same analysis type was re-run against the same Monte Carlo run. Shared by
// the service supersede path and the worker's cancel-aware terminal convergence.
const JobErrSupersededByNewerAnalysis = "superseded_by_newer_analysis"

// Job is a queued background task.
type Job struct {
	ID              string `json:"id"`
	PlanID          string `json:"plan_id"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	InputHash       string `json:"input_hash"`
	PayloadJSON     string `json:"payload_json,omitempty"`
	ProgressCurrent int    `json:"progress_current"`
	ProgressTotal   int    `json:"progress_total"`
	Phase           string `json:"phase"`
	CancelRequested bool   `json:"cancel_requested"`
	RetryCount      int    `json:"retry_count"`
	HeartbeatAt     *int64 `json:"heartbeat_at,omitempty"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	CreatedAt       int64  `json:"created_at"`
	StartedAt       *int64 `json:"started_at,omitempty"`
	FinishedAt      *int64 `json:"finished_at,omitempty"`
}

// JobRepo manages the jobs table.
type JobRepo struct {
	db *sql.DB
}

type jobRowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func NewJobRepo(db *sql.DB) *JobRepo {
	return &JobRepo{db: db}
}

func (r *JobRepo) Create(ctx context.Context, tx *sql.Tx, job Job) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	if job.CreatedAt == 0 {
		job.CreatedAt = now
	}
	var planID any
	if job.PlanID != "" {
		planID = job.PlanID
	}
	_, err := exec.ExecContext(ctx, `
		INSERT INTO jobs (
			id, plan_id, type, status, input_hash, payload_json,
			progress_current, progress_total, phase,
			cancel_requested, retry_count, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, planID, job.Type, job.Status, job.InputHash, job.PayloadJSON,
		job.ProgressCurrent, job.ProgressTotal, job.Phase,
		boolToInt(job.CancelRequested), job.RetryCount, job.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

func (r *JobRepo) GetByID(ctx context.Context, id string) (Job, error) {
	return getJobByID(ctx, r.db, id)
}

func getJobByID(ctx context.Context, q jobRowQuerier, id string) (Job, error) {
	row := q.QueryRowContext(ctx, `
			SELECT id, plan_id, type, status, input_hash, payload_json,
				progress_current, progress_total, phase, cancel_requested, retry_count,
				heartbeat_at, error_code, error_message, created_at, started_at, finished_at
			FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

func (r *JobRepo) ClaimNextQueued(ctx context.Context) (Job, error) {
	now := time.Now().UnixMilli()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, fmt.Errorf("begin claim job tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM jobs WHERE status=? ORDER BY created_at LIMIT 1`, JobStatusQueued).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("select queued job: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status=?, started_at=?, heartbeat_at=?, phase=?
		WHERE id=? AND status=?`,
		JobStatusRunning, now, now, "starting", id, JobStatusQueued)
	if err != nil {
		return Job{}, fmt.Errorf("mark job running: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Job{}, ErrJobNotFound
	}
	if err := tx.Commit(); err != nil {
		return Job{}, fmt.Errorf("commit claimed job: %w", err)
	}
	return r.GetByID(ctx, id)
}

func (r *JobRepo) UpdateProgress(ctx context.Context, id string, current, total int, phase string) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET progress_current=?, progress_total=?, phase=?, heartbeat_at=?
		WHERE id=?`, current, total, phase, now, id)
	if err != nil {
		return fmt.Errorf("update job progress: %w", err)
	}
	return nil
}

func (r *JobRepo) Heartbeat(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `UPDATE jobs SET heartbeat_at=? WHERE id=?`, now, id)
	return wrapSQL("update job heartbeat", err)
}

func (r *JobRepo) RequestCancel(ctx context.Context, id string) error {
	return r.RequestCancelTx(ctx, nil, id)
}

// RequestCancelTx sets cancel_requested on a job, optionally within a tx. Safe to
// call on terminal jobs (no-op effect); the worker's cancelCheck honors the flag
// for running jobs.
func (r *JobRepo) RequestCancelTx(ctx context.Context, tx *sql.Tx, id string) error {
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx, `UPDATE jobs SET cancel_requested=1 WHERE id=?`, id)
	return wrapSQL("request job cancel", err)
}

// RequestCancelRunningWithErrorTx flags a still-running job for cancellation and
// records the cancel reason, only when status='running'. Returns whether the row
// was updated. Used to supersede an in-flight analysis job so its terminal
// convergence becomes canceled (carrying the supersede error code) rather than
// succeeded. No-op for queued/terminal jobs (callers handle queued separately).
func (r *JobRepo) RequestCancelRunningWithErrorTx(
	ctx context.Context, tx *sql.Tx, id, errCode, errMsg string,
) (bool, error) {
	exec := r.exec(tx)
	res, err := exec.ExecContext(ctx, `
		UPDATE jobs SET cancel_requested=1, error_code=?, error_message=?
		WHERE id=? AND status=?`,
		errCode, errMsg, id, JobStatusRunning)
	if err != nil {
		return false, wrapSQL("request cancel running job", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// FinishRunningIfNotCanceled marks a running job succeeded only when no cancel was
// requested. Returns true when the row was actually updated, letting the worker
// fall back to a canceled convergence on a concurrent supersede.
func (r *JobRepo) FinishRunningIfNotCanceled(ctx context.Context, id string) (bool, error) {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status=?, finished_at=?, phase=''
		WHERE id=? AND status=? AND cancel_requested=0`,
		JobStatusSucceeded, now, id, JobStatusRunning)
	if err != nil {
		return false, wrapSQL("finish running job", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// FinishCanceledIfRequested marks a running job canceled only when a cancel was
// requested, preserving any error_code/error_message already recorded (e.g. the
// supersede reason). Returns true when the row was actually updated.
func (r *JobRepo) FinishCanceledIfRequested(ctx context.Context, id string) (bool, error) {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status=?, finished_at=?, phase=''
		WHERE id=? AND status=? AND cancel_requested=1`,
		JobStatusCanceled, now, id, JobStatusRunning)
	if err != nil {
		return false, wrapSQL("finish canceled job", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// CancelQueued marks a queued job as canceled immediately.
func (r *JobRepo) CancelQueued(ctx context.Context, id string) error {
	return r.CancelQueuedWithError(ctx, nil, id, "", "")
}

// CancelQueuedWithError marks a queued job canceled with optional error metadata.
func (r *JobRepo) CancelQueuedWithError(ctx context.Context, tx *sql.Tx, id, errCode, errMsg string) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	res, err := exec.ExecContext(ctx, `
		UPDATE jobs SET status=?, finished_at=?, cancel_requested=1, phase='',
			error_code=?, error_message=?
		WHERE id=? AND status=?`,
		JobStatusCanceled, now, errCode, errMsg, id, JobStatusQueued)
	if err != nil {
		return wrapSQL("cancel queued job", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}

func (r *JobRepo) IsCancelRequested(ctx context.Context, id string) (bool, error) {
	var v int
	err := r.db.QueryRowContext(ctx, `SELECT cancel_requested FROM jobs WHERE id=?`, id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrJobNotFound
	}
	return v == 1, wrapSQL("query cancel_requested", err)
}

func (r *JobRepo) Finish(ctx context.Context, id, status, errCode, errMsg string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, wrapSQL("begin finish job", err)
	}
	defer func() { _ = tx.Rollback() }()
	job, err := getJobByID(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if job.Status != JobStatusRunning {
		return false, nil
	}
	if status == JobStatusSucceeded {
		if err := requireDependentRunSucceededTx(ctx, tx, job); err != nil {
			return false, err
		}
	}
	now := time.Now().UnixMilli()
	res, err := tx.ExecContext(ctx, `UPDATE jobs
		SET status=?, finished_at=?, heartbeat_at=NULL, error_code=?, error_message=?, phase=''
		WHERE id=? AND status=?`, status, now, errCode, errMsg, id, JobStatusRunning)
	if err != nil {
		return false, wrapSQL("finish job", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return false, nil
	}
	if status != JobStatusSucceeded {
		if err := syncDependentJobStatusTx(ctx, tx, job, status, errCode, errMsg, now); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, wrapSQL("commit finish job", err)
	}
	return true, nil
}

func (r *JobRepo) FinishTx(ctx context.Context, tx *sql.Tx, id, status, errCode, errMsg string) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	_, err := exec.ExecContext(ctx, `
		UPDATE jobs SET status=?, finished_at=?, error_code=?, error_message=?, phase=''
		WHERE id=?`, status, now, errCode, errMsg, id)
	return wrapSQL("finish job", err)
}

func (r *JobRepo) RequeueIfRunning(ctx context.Context, id string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, wrapSQL("begin graceful requeue", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
			UPDATE jobs SET status=?, progress_current=0, started_at=NULL,
				heartbeat_at=NULL, finished_at=NULL, phase='', cancel_requested=0,
				error_code='', error_message=''
			WHERE id=? AND status=?`,
		JobStatusQueued, id, JobStatusRunning)
	if err != nil {
		return false, wrapSQL("requeue running job", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	job, err := getJobByID(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if err := syncDependentJobStatusTx(ctx, tx, job, JobStatusQueued, "", "", 0); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, wrapSQL("commit graceful requeue", err)
	}
	return true, nil
}

// ListRunningForReconcile lists every running job at startup, or only jobs with
// a missing/stale heartbeat for periodic reconciliation.
func (r *JobRepo) ListRunningForReconcile(ctx context.Context, staleBefore *int64) ([]Job, error) {
	query := `SELECT id, plan_id, type, status, input_hash, payload_json,
		progress_current, progress_total, phase, cancel_requested, retry_count,
		heartbeat_at, error_code, error_message, created_at, started_at, finished_at
		FROM jobs WHERE status=?`
	args := []any{JobStatusRunning}
	if staleBefore != nil {
		query += ` AND (heartbeat_at IS NULL OR heartbeat_at < ?)`
		args = append(args, *staleBefore)
	}
	query += ` ORDER BY created_at, id`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapSQL("list running jobs for reconcile", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, wrapSQL("iterate running jobs for reconcile", rows.Err())
}

// ConvergeInterrupted atomically requeues, fails, or cancels one orphaned job
// and its dependent research run. periodicStaleBefore is nil during startup;
// otherwise the heartbeat predicate is rechecked in the transaction so a live
// worker heartbeat wins the race.
func (r *JobRepo) ConvergeInterrupted(
	ctx context.Context, id string, periodicStaleBefore *int64, maxRetries int, now int64,
) (InterruptedJobAction, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return InterruptedJobAction{}, false, wrapSQL("begin interrupted job convergence", err)
	}
	defer func() { _ = tx.Rollback() }()
	job, err := getJobByID(ctx, tx, id)
	if err != nil {
		if errors.Is(err, ErrJobNotFound) {
			return InterruptedJobAction{}, false, nil
		}
		return InterruptedJobAction{}, false, err
	}
	if job.Status != JobStatusRunning {
		return InterruptedJobAction{}, false, nil
	}
	if periodicStaleBefore != nil && job.HeartbeatAt != nil && *job.HeartbeatAt >= *periodicStaleBefore {
		return InterruptedJobAction{}, false, nil
	}

	action, targetStatus, code, message := InterruptedActionRequeued, JobStatusQueued, "", ""
	if job.CancelRequested {
		action, targetStatus = InterruptedActionCanceled, JobStatusCanceled
		code, message = job.ErrorCode, job.ErrorMessage
		if code == "" {
			code, message = "canceled_by_user", "canceled by user"
		}
	} else if job.RetryCount >= maxRetries {
		action, targetStatus = InterruptedActionFailed, JobStatusFailed
		code = JobErrorWorkerInterrupted
		message = "执行进程中断，自动重试次数已用尽，请重新运行"
	}

	var res sql.Result
	if action == InterruptedActionRequeued {
		res, err = tx.ExecContext(ctx, `UPDATE jobs
			SET status=?, retry_count=retry_count+1, progress_current=0,
				started_at=NULL, heartbeat_at=NULL, finished_at=NULL, phase='retrying',
				cancel_requested=0, error_code='', error_message=''
			WHERE id=? AND status=?`, JobStatusQueued, id, JobStatusRunning)
	} else {
		res, err = tx.ExecContext(ctx, `UPDATE jobs
			SET status=?, finished_at=?, heartbeat_at=NULL, phase='',
				error_code=?, error_message=?
			WHERE id=? AND status=?`, targetStatus, now, code, message, id, JobStatusRunning)
	}
	if err != nil {
		return InterruptedJobAction{}, false, wrapSQL("converge interrupted job", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return InterruptedJobAction{}, false, nil
	}
	if err := syncDependentJobStatusTx(ctx, tx, job, targetStatus, code, message, now); err != nil {
		return InterruptedJobAction{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return InterruptedJobAction{}, false, wrapSQL("commit interrupted job convergence", err)
	}
	return InterruptedJobAction{Action: action, Job: job}, true, nil
}

func syncDependentJobStatusTx(
	ctx context.Context, tx *sql.Tx, job Job, status, code, message string, completedAt int64,
) error {
	switch job.Type {
	case JobTypeResearchOptimization:
		if status == JobStatusQueued {
			res, err := tx.ExecContext(ctx, `UPDATE research_optimization_runs
				SET status=?, evaluated_count=0, result_json='{}', error_code='',
					error_message='', completed_at=NULL WHERE job_id=?`, ResearchRunStatusQueued, job.ID)
			return requireDependentUpdate("requeue optimization run", res, err)
		}
		res, err := tx.ExecContext(ctx, `UPDATE research_optimization_runs
			SET status=?, error_code=?, error_message=?, completed_at=? WHERE job_id=?`,
			status, code, message, completedAt, job.ID)
		return requireDependentUpdate("finish optimization run with job", res, err)
	case JobTypeResearchBacktest:
		if status == JobStatusQueued {
			res, err := tx.ExecContext(ctx, `UPDATE research_backtest_runs
				SET status=?, summary_json='{}', data_quality_json='{}', completed_at=NULL
				WHERE job_id=?`, ResearchRunStatusQueued, job.ID)
			return requireDependentUpdate("requeue research run", res, err)
		}
		res, err := tx.ExecContext(ctx, `UPDATE research_backtest_runs
			SET status=?, completed_at=? WHERE job_id=?`, status, completedAt, job.ID)
		return requireDependentUpdate("finish research run with job", res, err)
	default:
		return nil
	}
}

func requireDependentUpdate(operation string, result sql.Result, err error) error {
	if err != nil {
		return wrapSQL(operation, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return wrapSQL(operation+" rows affected", err)
	}
	if affected != 1 {
		return fmt.Errorf("%s: dependent run count=%d", operation, affected)
	}
	return nil
}

func requireDependentRunSucceededTx(ctx context.Context, tx *sql.Tx, job Job) error {
	var status string
	var err error
	switch job.Type {
	case JobTypeResearchOptimization:
		err = tx.QueryRowContext(ctx,
			`SELECT status FROM research_optimization_runs WHERE job_id=?`, job.ID).Scan(&status)
	case JobTypeResearchBacktest:
		err = tx.QueryRowContext(ctx,
			`SELECT status FROM research_backtest_runs WHERE job_id=?`, job.ID).Scan(&status)
	default:
		return nil
	}
	if err != nil {
		return wrapSQL("load dependent run status", err)
	}
	if status != ResearchRunStatusSucceeded {
		return fmt.Errorf("dependent run not completed: job=%s run_status=%s", job.ID, status)
	}
	return nil
}

func (r *JobRepo) FindIdempotency(ctx context.Context, planID, jobType, key string) (Job, string, error) {
	var jobID, inputHash string
	err := r.db.QueryRowContext(ctx, `
		SELECT job_id, input_hash FROM job_idempotency_keys
		WHERE plan_id=? AND job_type=? AND idempotency_key=?`,
		planID, jobType, key).Scan(&jobID, &inputHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, "", ErrJobNotFound
	}
	if err != nil {
		return Job{}, "", wrapSQL("lookup idempotency key", err)
	}
	job, err := r.GetByID(ctx, jobID)
	if err != nil {
		return Job{}, "", wrapSQL("load idempotent job", err)
	}
	return job, inputHash, nil
}

func (r *JobRepo) SaveIdempotency(ctx context.Context, tx *sql.Tx, planID, jobType, key, jobID,
	inputHash string,
) error {
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx, `
		INSERT INTO job_idempotency_keys (plan_id, job_type, idempotency_key, job_id, input_hash, created_at)
		VALUES (?,?,?,?,?,?)`,
		planID, jobType, key, jobID, inputHash, time.Now().UnixMilli())
	return wrapSQL("save idempotency key", err)
}

func (r *JobRepo) ListByPlanAndType(ctx context.Context, planID, jobType string, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, type, status, input_hash, payload_json,
			progress_current, progress_total, phase, cancel_requested, retry_count,
			heartbeat_at, error_code, error_message, created_at, started_at, finished_at
		FROM jobs WHERE plan_id=? AND type=? ORDER BY created_at DESC LIMIT ?`,
		planID, jobType, limit)
	if err != nil {
		return nil, fmt.Errorf("query jobs by plan: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Job
	for rows.Next() {
		j, err := scanJobRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job row: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return out, nil
}

// --- admin listing & aggregation ---

// JobFilter narrows the admin job listing. New filter dimensions are added
// as fields; the List signature stays stable.
type JobFilter struct {
	Type     string
	Statuses []string
	PlanID   string
	Limit    int
	Offset   int
}

// JobWithPlan is one admin listing row: the job plus the (possibly deleted)
// plan's display name. payload_json is intentionally not read.
type JobWithPlan struct {
	Job
	PlanName string `json:"plan_name"`
}

func buildJobWhere(f JobFilter) (string, []any) {
	var (
		conds []string
		args  []any
	)
	if f.Type != "" {
		conds = append(conds, "j.type = ?")
		args = append(args, f.Type)
	}
	if len(f.Statuses) > 0 {
		ph := make([]string, len(f.Statuses))
		for i, s := range f.Statuses {
			ph[i] = "?"
			args = append(args, s)
		}
		conds = append(conds, "j.status IN ("+strings.Join(ph, ",")+")")
	}
	if f.PlanID != "" {
		conds = append(conds, "j.plan_id = ?")
		args = append(args, f.PlanID)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	return where, args
}

// List returns one filtered page of jobs (created_at DESC) with the plan name
// attached via LEFT JOIN (plans can be deleted; plan_id can be empty for
// system jobs), plus the filtered total count.
func (r *JobRepo) List(ctx context.Context, f JobFilter) ([]JobWithPlan, int, error) {
	where, args := buildJobWhere(f)
	return queryPage(ctx, r.db,
		`SELECT COUNT(*) FROM jobs j `+where,
		`SELECT j.id, j.plan_id, j.type, j.status, j.input_hash, '' AS payload_json,
			j.progress_current, j.progress_total, j.phase, j.cancel_requested, j.retry_count,
			j.heartbeat_at, j.error_code, j.error_message, j.created_at, j.started_at, j.finished_at,
			COALESCE(p.name, '') AS plan_name
		FROM jobs j
		LEFT JOIN plans p ON p.id = j.plan_id
		`+where+`
		ORDER BY j.created_at DESC, j.id DESC
		LIMIT ? OFFSET ?`,
		args, f.Limit, f.Offset,
		scanJobWithPlan,
		"count jobs", "query jobs", "scan job list row", "iterate job list rows",
	)
}

func scanJobWithPlan(rows *sql.Rows) (JobWithPlan, error) {
	var item JobWithPlan
	var cancel int
	var hb, started, finished sql.NullInt64
	var payload, planID sql.NullString
	if err := rows.Scan(
		&item.ID, &planID, &item.Type, &item.Status, &item.InputHash, &payload,
		&item.ProgressCurrent, &item.ProgressTotal, &item.Phase, &cancel, &item.RetryCount,
		&hb, &item.ErrorCode, &item.ErrorMessage, &item.CreatedAt, &started, &finished,
		&item.PlanName,
	); err != nil {
		return JobWithPlan{}, fmt.Errorf("scan job list row: %w", err)
	}
	if planID.Valid {
		item.PlanID = planID.String
	}
	item.CancelRequested = cancel == 1
	if hb.Valid {
		v := hb.Int64
		item.HeartbeatAt = &v
	}
	if started.Valid {
		v := started.Int64
		item.StartedAt = &v
	}
	if finished.Valid {
		v := finished.Int64
		item.FinishedAt = &v
	}
	return item, nil
}

// CountByStatus returns job counts grouped by status.
func (r *JobRepo) CountByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("count jobs by status: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("scan job status count: %w", err)
		}
		out[status] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job status counts: %w", err)
	}
	return out, nil
}

// CountFinishedSince counts jobs of one terminal status finished at or after
// since (epoch ms).
func (r *JobRepo) CountFinishedSince(ctx context.Context, status string, since int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs
		WHERE status=? AND finished_at IS NOT NULL AND finished_at >= ?`,
		status, since).Scan(&n)
	return n, wrapSQL("count finished jobs", err)
}

func scanJob(row rowScanner) (Job, error) {
	var j Job
	var cancel int
	var hb, started, finished sql.NullInt64
	var payload sql.NullString
	var planID sql.NullString
	err := row.Scan(
		&j.ID, &planID, &j.Type, &j.Status, &j.InputHash, &payload,
		&j.ProgressCurrent, &j.ProgressTotal, &j.Phase, &cancel, &j.RetryCount,
		&hb, &j.ErrorCode, &j.ErrorMessage, &j.CreatedAt, &started, &finished,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("scan job: %w", err)
	}
	if payload.Valid {
		j.PayloadJSON = payload.String
	}
	if planID.Valid {
		j.PlanID = planID.String
	}
	j.CancelRequested = cancel == 1
	if hb.Valid {
		v := hb.Int64
		j.HeartbeatAt = &v
	}
	if started.Valid {
		v := started.Int64
		j.StartedAt = &v
	}
	if finished.Valid {
		v := finished.Int64
		j.FinishedAt = &v
	}
	return j, nil
}

func scanJobRows(rows *sql.Rows) (Job, error) {
	var j Job
	var cancel int
	var hb, started, finished sql.NullInt64
	var payload sql.NullString
	var planID sql.NullString
	err := rows.Scan(
		&j.ID, &planID, &j.Type, &j.Status, &j.InputHash, &payload,
		&j.ProgressCurrent, &j.ProgressTotal, &j.Phase, &cancel, &j.RetryCount,
		&hb, &j.ErrorCode, &j.ErrorMessage, &j.CreatedAt, &started, &finished,
	)
	if err != nil {
		return Job{}, fmt.Errorf("scan job row: %w", err)
	}
	if payload.Valid {
		j.PayloadJSON = payload.String
	}
	if planID.Valid {
		j.PlanID = planID.String
	}
	j.CancelRequested = cancel == 1
	if hb.Valid {
		v := hb.Int64
		j.HeartbeatAt = &v
	}
	if started.Valid {
		v := started.Int64
		j.StartedAt = &v
	}
	if finished.Valid {
		v := finished.Int64
		j.FinishedAt = &v
	}
	return j, nil
}

func (r *JobRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

var ErrJobNotFound = errors.New("job not found")
