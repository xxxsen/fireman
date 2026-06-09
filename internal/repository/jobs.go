package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	JobTypeSimulation  = "simulation"
	JobTypeStress      = "stress"
	JobTypeSensitivity = "sensitivity"
	JobStatusQueued    = "queued"
	JobStatusRunning   = "running"
	JobStatusSucceeded = "succeeded"
	JobStatusFailed    = "failed"
	JobStatusCanceled  = "canceled"
)

// Job is a queued background task.
type Job struct {
	ID              string `json:"id"`
	PlanID          string `json:"plan_id"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	InputHash       string `json:"input_hash"`
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

func NewJobRepo(db *sql.DB) *JobRepo {
	return &JobRepo{db: db}
}

func (r *JobRepo) Create(ctx context.Context, tx *sql.Tx, job Job) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	if job.CreatedAt == 0 {
		job.CreatedAt = now
	}
	_, err := exec.ExecContext(ctx, `
		INSERT INTO jobs (
			id, plan_id, type, status, input_hash,
			progress_current, progress_total, phase,
			cancel_requested, retry_count, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.PlanID, job.Type, job.Status, job.InputHash,
		job.ProgressCurrent, job.ProgressTotal, job.Phase,
		boolToInt(job.CancelRequested), job.RetryCount, job.CreatedAt)
	return err
}

func (r *JobRepo) GetByID(ctx context.Context, id string) (Job, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, plan_id, type, status, input_hash,
			progress_current, progress_total, phase, cancel_requested, retry_count,
			heartbeat_at, error_code, error_message, created_at, started_at, finished_at
		FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

func (r *JobRepo) ClaimNextQueued(ctx context.Context) (Job, error) {
	now := time.Now().UnixMilli()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var id string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM jobs WHERE status=? ORDER BY created_at LIMIT 1`, JobStatusQueued).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE jobs SET status=?, started_at=?, heartbeat_at=?, phase=?
		WHERE id=? AND status=?`,
		JobStatusRunning, now, now, "starting", id, JobStatusQueued)
	if err != nil {
		return Job{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Job{}, ErrJobNotFound
	}
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return r.GetByID(ctx, id)
}

func (r *JobRepo) UpdateProgress(ctx context.Context, id string, current, total int, phase string) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET progress_current=?, progress_total=?, phase=?, heartbeat_at=?
		WHERE id=?`, current, total, phase, now, id)
	return err
}

func (r *JobRepo) Heartbeat(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `UPDATE jobs SET heartbeat_at=? WHERE id=?`, now, id)
	return err
}

func (r *JobRepo) RequestCancel(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE jobs SET cancel_requested=1 WHERE id=?`, id)
	return err
}

// CancelQueued marks a queued job as canceled immediately.
func (r *JobRepo) CancelQueued(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status=?, finished_at=?, cancel_requested=1, phase=''
		WHERE id=? AND status=?`,
		JobStatusCanceled, now, id, JobStatusQueued)
	if err != nil {
		return err
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
	return v == 1, err
}

func (r *JobRepo) Finish(ctx context.Context, id, status, errCode, errMsg string) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status=?, finished_at=?, error_code=?, error_message=?, phase=''
		WHERE id=?`, status, now, errCode, errMsg, id)
	return err
}

func (r *JobRepo) RequeueStaleRunning(ctx context.Context, staleBefore int64, maxRetries int) (int, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status=?, retry_count=retry_count+1, started_at=NULL, heartbeat_at=NULL, phase=''
		WHERE status=? AND heartbeat_at IS NOT NULL AND heartbeat_at < ? AND retry_count < ?`,
		JobStatusQueued, JobStatusRunning, staleBefore, maxRetries)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
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
		return Job{}, "", err
	}
	job, err := r.GetByID(ctx, jobID)
	return job, inputHash, err
}

func (r *JobRepo) SaveIdempotency(ctx context.Context, tx *sql.Tx, planID, jobType, key, jobID, inputHash string) error {
	exec := r.exec(tx)
	_, err := exec.ExecContext(ctx, `
		INSERT INTO job_idempotency_keys (plan_id, job_type, idempotency_key, job_id, input_hash, created_at)
		VALUES (?,?,?,?,?,?)`,
		planID, jobType, key, jobID, inputHash, time.Now().UnixMilli())
	return err
}

func (r *JobRepo) ListByPlanAndType(ctx context.Context, planID, jobType string, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, type, status, input_hash,
			progress_current, progress_total, phase, cancel_requested, retry_count,
			heartbeat_at, error_code, error_message, created_at, started_at, finished_at
		FROM jobs WHERE plan_id=? AND type=? ORDER BY created_at DESC LIMIT ?`,
		planID, jobType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func scanJob(row *sql.Row) (Job, error) {
	var j Job
	var cancel int
	var hb, started, finished sql.NullInt64
	err := row.Scan(
		&j.ID, &j.PlanID, &j.Type, &j.Status, &j.InputHash,
		&j.ProgressCurrent, &j.ProgressTotal, &j.Phase, &cancel, &j.RetryCount,
		&hb, &j.ErrorCode, &j.ErrorMessage, &j.CreatedAt, &started, &finished,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}
	if err != nil {
		return Job{}, err
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
	err := rows.Scan(
		&j.ID, &j.PlanID, &j.Type, &j.Status, &j.InputHash,
		&j.ProgressCurrent, &j.ProgressTotal, &j.Phase, &cancel, &j.RetryCount,
		&hb, &j.ErrorCode, &j.ErrorMessage, &j.CreatedAt, &started, &finished,
	)
	if err != nil {
		return Job{}, err
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
