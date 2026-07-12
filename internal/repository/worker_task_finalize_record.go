package repository

import (
	"context"
	"database/sql"
	"strings"
)

// WorkerTaskFinalizeRecord mirrors one worker_task_finalize_records row: a single
// finalization Go finalizer received from the sidecar. The table is append-only
// and observational; it never participates in business decisions.
type WorkerTaskFinalizeRecord struct {
	ID           int64  `json:"id"`
	TaskID       string `json:"task_id"`
	TaskType     string `json:"task_type"`
	AttemptNo    int    `json:"attempt_no"`
	Result       string `json:"result"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	DurationMs   int64  `json:"duration_ms"`
	CreatedAt    int64  `json:"created_at"`
}

// WorkerTaskFinalizeRecordFilter narrows the admin finalization listing. New filter
// dimensions are added as fields; the List signature stays stable.
type WorkerTaskFinalizeRecordFilter struct {
	TaskID   string
	Result   string
	TaskType string
	Limit    int
	Offset   int
}

// WorkerTaskFinalizeRecordRepo manages the worker_task_finalize_records observation table.
type WorkerTaskFinalizeRecordRepo struct {
	db *sql.DB
}

func NewWorkerTaskFinalizeRecordRepo(db *sql.DB) *WorkerTaskFinalizeRecordRepo {
	return &WorkerTaskFinalizeRecordRepo{db: db}
}

// Insert appends one finalization record.
func (r *WorkerTaskFinalizeRecordRepo) Insert(ctx context.Context, rec WorkerTaskFinalizeRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO worker_task_finalize_records
			(task_id, task_type, attempt_no, result, error_code, error_message, duration_ms, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		rec.TaskID, rec.TaskType, rec.AttemptNo, rec.Result,
		rec.ErrorCode, rec.ErrorMessage, rec.DurationMs, rec.CreatedAt)
	return wrapSQL("insert finalization record", err)
}

const workerTaskFinalizeRecordColumns = `
	id, task_id, task_type, attempt_no, result, error_code, error_message, duration_ms, created_at`

func scanWorkerTaskFinalizeRecord(row rowScanner) (WorkerTaskFinalizeRecord, error) {
	var rec WorkerTaskFinalizeRecord
	err := row.Scan(
		&rec.ID, &rec.TaskID, &rec.TaskType, &rec.AttemptNo, &rec.Result,
		&rec.ErrorCode, &rec.ErrorMessage, &rec.DurationMs, &rec.CreatedAt,
	)
	if err != nil {
		return WorkerTaskFinalizeRecord{}, wrapSQL("scan finalization record", err)
	}
	return rec, nil
}

// ListByTask returns every finalization record of one task, newest first. Volume
// per task is bounded by the sidecar's retry budget (<=10 Go finalizers).
func (r *WorkerTaskFinalizeRecordRepo) ListByTask(
	ctx context.Context,
	taskID string,
) ([]WorkerTaskFinalizeRecord, error) {
	return queryCollect(
		ctx, r.db, `
		SELECT `+workerTaskFinalizeRecordColumns+`
		FROM worker_task_finalize_records
		WHERE task_id=?
		ORDER BY created_at DESC, id DESC`,
		[]any{taskID},
		func(rows *sql.Rows) (WorkerTaskFinalizeRecord, error) { return scanWorkerTaskFinalizeRecord(rows) },
		"query finalization records", "scan finalization record", "iterate finalization records",
	)
}

func buildWorkerTaskFinalizeRecordWhere(f WorkerTaskFinalizeRecordFilter) (string, []any) {
	var (
		conds []string
		args  []any
	)
	if f.TaskID != "" {
		conds = append(conds, "task_id = ?")
		args = append(args, f.TaskID)
	}
	if f.Result != "" {
		conds = append(conds, "result = ?")
		args = append(args, f.Result)
	}
	if f.TaskType != "" {
		conds = append(conds, "task_type = ?")
		args = append(args, f.TaskType)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	return where, args
}

// List returns one filtered page of finalization records (newest first) plus the
// filtered total count.
func (r *WorkerTaskFinalizeRecordRepo) List(
	ctx context.Context, f WorkerTaskFinalizeRecordFilter,
) ([]WorkerTaskFinalizeRecord, int, error) {
	where, args := buildWorkerTaskFinalizeRecordWhere(f)
	return queryPage(
		ctx, r.db,
		`SELECT COUNT(*) FROM worker_task_finalize_records `+where,
		`SELECT `+workerTaskFinalizeRecordColumns+`
		FROM worker_task_finalize_records `+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`,
		args, f.Limit, f.Offset,
		func(rows *sql.Rows) (WorkerTaskFinalizeRecord, error) { return scanWorkerTaskFinalizeRecord(rows) },
		"count finalization records", "query finalization records",
		"scan finalization record", "iterate finalization records",
	)
}

// DeleteBefore removes records created strictly before cutoff (epoch ms).
// Used for retention cleanup after each insert; walks
// idx_worker_task_finalize_records_created.
func (r *WorkerTaskFinalizeRecordRepo) DeleteBefore(ctx context.Context, cutoff int64) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM worker_task_finalize_records WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, wrapSQL("delete old finalization records", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CountSince returns (total, failed) finalization counts with created_at >= since.
// failed counts every non-success result.
func (r *WorkerTaskFinalizeRecordRepo) CountSince(ctx context.Context, since int64) (int, int, error) {
	var total, failed int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN result <> 'success' THEN 1 ELSE 0 END), 0)
		FROM worker_task_finalize_records WHERE created_at >= ?`, since).Scan(&total, &failed)
	return total, failed, wrapSQL("count finalization records since", err)
}
