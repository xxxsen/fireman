package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Worker task types shared between Go (creator) and the sidecar worker.
const (
	WorkerTaskTypeAssetDirectorySync = "asset_directory_sync"
	WorkerTaskTypeAssetHistorySync   = "asset_history_sync"
	WorkerTaskTypeFXRateSync         = "fx_rate_sync"
)

// Worker task statuses. Terminal transitions are owned by the sidecar; Go only
// creates pending tasks and reads statuses.
const (
	WorkerTaskStatusPending     = "pending"
	WorkerTaskStatusRunning     = "running"
	WorkerTaskStatusPreComplete = "pre_complete"
	WorkerTaskStatusComplete    = "complete"
	WorkerTaskStatusFailed      = "failed"
	WorkerTaskStatusCanceled    = "canceled"
)

// ErrWorkerTaskNotFound is returned when a task id cannot be found.
var ErrWorkerTaskNotFound = errors.New("worker task not found")

// WorkerTask mirrors a worker_tasks row.
type WorkerTask struct {
	ID                  string `json:"id"`
	VersionNo           int64  `json:"version_no"`
	Type                string `json:"type"`
	Status              string `json:"status"`
	DedupeKey           string `json:"dedupe_key"`
	PayloadJSON         string `json:"payload_json"`
	ResultData          string `json:"result_data"`
	HeartbeatAt         *int64 `json:"heartbeat_at,omitempty"`
	ErrorCode           string `json:"error_code"`
	ErrorMessage        string `json:"error_message"`
	PostProcessAttempts int    `json:"post_process_attempts"`
	NextPostProcessAt   *int64 `json:"next_post_process_at,omitempty"`
	CreatedAt           int64  `json:"created_at"`
	StartedAt           *int64 `json:"started_at,omitempty"`
	PreCompletedAt      *int64 `json:"pre_completed_at,omitempty"`
	FinishedAt          *int64 `json:"finished_at,omitempty"`
}

// IsActiveWorkerTaskStatus reports whether the status blocks duplicate task
// creation for the same (type, dedupe_key).
func IsActiveWorkerTaskStatus(status string) bool {
	switch status {
	case WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete:
		return true
	}
	return false
}

// WorkerTaskRepo manages the worker_tasks scheduling table.
type WorkerTaskRepo struct {
	db *sql.DB
}

func NewWorkerTaskRepo(db *sql.DB) *WorkerTaskRepo {
	return &WorkerTaskRepo{db: db}
}

// CreateTx inserts a pending worker task, allocating its monotonic version_no
// from worker_task_versions inside the caller's transaction.
func (r *WorkerTaskRepo) CreateTx(ctx context.Context, tx *sql.Tx, task *WorkerTask) error {
	if task.CreatedAt == 0 {
		task.CreatedAt = time.Now().UnixMilli()
	}
	if task.Status == "" {
		task.Status = WorkerTaskStatusPending
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO worker_task_versions DEFAULT VALUES`); err != nil {
		return fmt.Errorf("allocate worker task version: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT last_insert_rowid()`).Scan(&task.VersionNo); err != nil {
		return fmt.Errorf("read worker task version: %w", err)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO worker_tasks (id, version_no, type, status, dedupe_key, payload_json, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		task.ID, task.VersionNo, task.Type, task.Status, task.DedupeKey, task.PayloadJSON, task.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert worker task: %w", err)
	}
	return nil
}

// IsWorkerTaskUniqueConstraint reports whether err is the partial unique index
// violation on (type, dedupe_key) for active tasks.
func IsWorkerTaskUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

const workerTaskColumns = `
	id, version_no, type, status, dedupe_key, payload_json, result_data,
	heartbeat_at, error_code, error_message,
	post_process_attempts, next_post_process_at,
	created_at, started_at, pre_completed_at, finished_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWorkerTask(row rowScanner) (WorkerTask, error) {
	var t WorkerTask
	err := row.Scan(
		&t.ID, &t.VersionNo, &t.Type, &t.Status, &t.DedupeKey, &t.PayloadJSON, &t.ResultData,
		&t.HeartbeatAt, &t.ErrorCode, &t.ErrorMessage,
		&t.PostProcessAttempts, &t.NextPostProcessAt,
		&t.CreatedAt, &t.StartedAt, &t.PreCompletedAt, &t.FinishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkerTask{}, ErrWorkerTaskNotFound
	}
	if err != nil {
		return WorkerTask{}, wrapSQL("scan worker task", err)
	}
	return t, nil
}

func (r *WorkerTaskRepo) GetByID(ctx context.Context, id string) (WorkerTask, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+workerTaskColumns+` FROM worker_tasks WHERE id=?`, id)
	return scanWorkerTask(row)
}

// GetByIDTx reads a task inside a transaction (used by post-process).
func (r *WorkerTaskRepo) GetByIDTx(ctx context.Context, tx *sql.Tx, id string) (WorkerTask, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT `+workerTaskColumns+` FROM worker_tasks WHERE id=?`, id)
	return scanWorkerTask(row)
}

// FindActiveByDedupe returns the pending/running/pre_complete task for the
// given (type, dedupe_key) pair, if one exists.
func (r *WorkerTaskRepo) FindActiveByDedupe(ctx context.Context, taskType, dedupeKey string) (WorkerTask, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+workerTaskColumns+`
		FROM worker_tasks
		WHERE type=? AND dedupe_key=? AND status IN (?,?,?)
		ORDER BY created_at DESC LIMIT 1`,
		taskType, dedupeKey,
		WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete)
	return scanWorkerTask(row)
}

// FindActiveByDedupeTx is FindActiveByDedupe inside a transaction.
func (r *WorkerTaskRepo) FindActiveByDedupeTx(
	ctx context.Context, tx *sql.Tx, taskType, dedupeKey string,
) (WorkerTask, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT `+workerTaskColumns+`
		FROM worker_tasks
		WHERE type=? AND dedupe_key=? AND status IN (?,?,?)
		ORDER BY created_at DESC LIMIT 1`,
		taskType, dedupeKey,
		WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete)
	return scanWorkerTask(row)
}
