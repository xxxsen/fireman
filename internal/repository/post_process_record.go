package repository

import (
	"context"
	"database/sql"
	"strings"
)

// PostProcessRecord mirrors one post_process_records row: a single
// post-process callback received from the sidecar. The table is append-only
// and observational; it never participates in business decisions.
type PostProcessRecord struct {
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

// PostProcessRecordFilter narrows the admin callback listing. New filter
// dimensions are added as fields; the List signature stays stable.
type PostProcessRecordFilter struct {
	TaskID   string
	Result   string
	TaskType string
	Limit    int
	Offset   int
}

// PostProcessRecordRepo manages the post_process_records observation table.
type PostProcessRecordRepo struct {
	db *sql.DB
}

func NewPostProcessRecordRepo(db *sql.DB) *PostProcessRecordRepo {
	return &PostProcessRecordRepo{db: db}
}

// Insert appends one callback record.
func (r *PostProcessRecordRepo) Insert(ctx context.Context, rec PostProcessRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO post_process_records
			(task_id, task_type, attempt_no, result, error_code, error_message, duration_ms, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		rec.TaskID, rec.TaskType, rec.AttemptNo, rec.Result,
		rec.ErrorCode, rec.ErrorMessage, rec.DurationMs, rec.CreatedAt)
	return wrapSQL("insert post process record", err)
}

const postProcessRecordColumns = `
	id, task_id, task_type, attempt_no, result, error_code, error_message, duration_ms, created_at`

func scanPostProcessRecord(row rowScanner) (PostProcessRecord, error) {
	var rec PostProcessRecord
	err := row.Scan(
		&rec.ID, &rec.TaskID, &rec.TaskType, &rec.AttemptNo, &rec.Result,
		&rec.ErrorCode, &rec.ErrorMessage, &rec.DurationMs, &rec.CreatedAt,
	)
	if err != nil {
		return PostProcessRecord{}, wrapSQL("scan post process record", err)
	}
	return rec, nil
}

// ListByTask returns every callback record of one task, newest first. Volume
// per task is bounded by the sidecar's retry budget (<=10 callbacks).
func (r *PostProcessRecordRepo) ListByTask(ctx context.Context, taskID string) ([]PostProcessRecord, error) {
	return queryCollect(ctx, r.db, `
		SELECT `+postProcessRecordColumns+`
		FROM post_process_records
		WHERE task_id=?
		ORDER BY created_at DESC, id DESC`,
		[]any{taskID},
		func(rows *sql.Rows) (PostProcessRecord, error) { return scanPostProcessRecord(rows) },
		"query post process records", "scan post process record", "iterate post process records",
	)
}

func buildPostProcessRecordWhere(f PostProcessRecordFilter) (string, []any) {
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

// List returns one filtered page of callback records (newest first) plus the
// filtered total count.
func (r *PostProcessRecordRepo) List(
	ctx context.Context, f PostProcessRecordFilter,
) ([]PostProcessRecord, int, error) {
	where, args := buildPostProcessRecordWhere(f)
	return queryPage(ctx, r.db,
		`SELECT COUNT(*) FROM post_process_records `+where,
		`SELECT `+postProcessRecordColumns+`
		FROM post_process_records `+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`,
		args, f.Limit, f.Offset,
		func(rows *sql.Rows) (PostProcessRecord, error) { return scanPostProcessRecord(rows) },
		"count post process records", "query post process records",
		"scan post process record", "iterate post process records",
	)
}

// DeleteBefore removes records created strictly before cutoff (epoch ms).
// Used for retention cleanup after each insert; walks
// idx_post_process_records_created.
func (r *PostProcessRecordRepo) DeleteBefore(ctx context.Context, cutoff int64) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM post_process_records WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, wrapSQL("delete old post process records", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CountSince returns (total, failed) callback counts with created_at >= since.
// failed counts every non-success result.
func (r *PostProcessRecordRepo) CountSince(ctx context.Context, since int64) (int, int, error) {
	var total, failed int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN result <> 'success' THEN 1 ELSE 0 END), 0)
		FROM post_process_records WHERE created_at >= ?`, since).Scan(&total, &failed)
	return total, failed, wrapSQL("count post process records since", err)
}
