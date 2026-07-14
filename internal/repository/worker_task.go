package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	WorkerTypeGo      = "go_worker"
	WorkerTypeSidecar = "sidecar_worker"
)

const (
	WorkerTaskTypeSimulation           = "simulation"
	WorkerTaskTypeStress               = "stress"
	WorkerTaskTypeSensitivity          = "sensitivity"
	WorkerTaskTypeFirePlanImprovement  = "fire_plan_improvement"
	WorkerTaskTypeFireFrontier         = "fire_frontier"
	WorkerTaskTypeResearchBacktest     = "research_backtest"
	WorkerTaskTypeResearchOptimization = "research_optimization_backtest"
	WorkerTaskTypeInvestmentPath       = "single_asset_investment_path_backtest"
	WorkerTaskTypeAutoUpdateScan       = "market_data_auto_update_scan"
	WorkerTaskTypeAssetDirectorySync   = "asset_directory_sync"
	WorkerTaskTypeAssetHistorySync     = "asset_history_sync"
	WorkerTaskTypeFXRateSync           = "fx_rate_sync"
)

const (
	WorkerTaskStatusPending     = "pending"
	WorkerTaskStatusRunning     = "running"
	WorkerTaskStatusPreComplete = "pre_complete"
	WorkerTaskStatusComplete    = "complete"
	WorkerTaskStatusFailed      = "failed"
	WorkerTaskStatusCanceled    = "canceled"
)

const (
	WorkerTaskErrorHeartbeatTimeout = "worker_heartbeat_timeout"
	WorkerTaskErrorInterrupted      = "worker_interrupted"
	WorkerTaskErrorCanceled         = "canceled_by_user"
	WorkerTaskErrorCanceledByAdmin  = "canceled_by_admin"
	WorkerTaskErrorCanceledPlan     = "canceled_by_plan_delete"
)

var ErrWorkerTaskNotFound = errors.New("worker task not found")

type WorkerTask struct {
	ID               string `json:"id"`
	VersionNo        int64  `json:"version_no"`
	WorkerType       string `json:"worker_type"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	Priority         int    `json:"priority"`
	ScopeType        string `json:"scope_type"`
	ScopeID          string `json:"scope_id"`
	DedupeKey        string `json:"dedupe_key"`
	InputHash        string `json:"input_hash"`
	PayloadJSON      string `json:"payload_json,omitempty"`
	ResultKey        string `json:"result_key,omitempty"`
	ResultMetaJSON   string `json:"result_meta_json,omitempty"`
	ProgressCurrent  int    `json:"progress_current"`
	ProgressTotal    int    `json:"progress_total"`
	Phase            string `json:"phase"`
	CancelRequested  bool   `json:"cancel_requested"`
	AttemptCount     int    `json:"attempt_count"`
	MaxAttempts      int    `json:"max_attempts"`
	AvailableAt      int64  `json:"available_at"`
	ClaimedBy        string `json:"claimed_by,omitempty"`
	ClaimTokenHash   string `json:"-"`
	AttemptStartedAt *int64 `json:"attempt_started_at,omitempty"`
	HeartbeatAt      *int64 `json:"heartbeat_at,omitempty"`
	LeaseExpiresAt   *int64 `json:"lease_expires_at,omitempty"`
	FinalizeAttempts int    `json:"finalize_attempts"`
	NextFinalizeAt   *int64 `json:"next_finalize_at,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
	ErrorMessage     string `json:"error_message,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	StartedAt        *int64 `json:"started_at,omitempty"`
	ResultReportedAt *int64 `json:"result_reported_at,omitempty"`
	PreCompletedAt   *int64 `json:"pre_completed_at,omitempty"`
	FinishedAt       *int64 `json:"finished_at,omitempty"`
	UpdatedAt        int64  `json:"updated_at"`
}

type WorkerTaskAttempt struct {
	TaskID          string `json:"task_id"`
	AttemptNo       int    `json:"attempt_no"`
	WorkerType      string `json:"worker_type"`
	WorkerID        string `json:"worker_id"`
	ClaimTokenHash  string `json:"-"`
	ClaimedAt       int64  `json:"claimed_at"`
	LastHeartbeatAt *int64 `json:"last_heartbeat_at,omitempty"`
	ReleasedAt      *int64 `json:"released_at,omitempty"`
	Outcome         string `json:"outcome"`
	ReportOutcome   string `json:"-"`
	ResultKey       string `json:"-"`
	ErrorCode       string `json:"error_code"`
	ErrorMessage    string `json:"error_message"`
}

func (r *WorkerTaskRepo) FindAttemptByTokenTx(
	ctx context.Context, tx *sql.Tx, taskID, tokenHash string,
) (WorkerTaskAttempt, error) {
	var attempt WorkerTaskAttempt
	err := tx.QueryRowContext(ctx, `SELECT task_id,attempt_no,worker_type,worker_id,claim_token_hash,
		claimed_at,last_heartbeat_at,released_at,outcome,report_outcome,result_key,error_code,error_message
		FROM worker_task_attempts WHERE task_id=? AND claim_token_hash=?
		ORDER BY attempt_no DESC LIMIT 1`, taskID, tokenHash).Scan(
		&attempt.TaskID, &attempt.AttemptNo, &attempt.WorkerType, &attempt.WorkerID,
		&attempt.ClaimTokenHash, &attempt.ClaimedAt, &attempt.LastHeartbeatAt,
		&attempt.ReleasedAt, &attempt.Outcome, &attempt.ReportOutcome, &attempt.ResultKey,
		&attempt.ErrorCode, &attempt.ErrorMessage,
	)
	return attempt, wrapSQL("find task attempt by token", err)
}

func IsActiveWorkerTaskStatus(status string) bool {
	switch status {
	case WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete:
		return true
	}
	return false
}

func IsTerminalWorkerTaskStatus(status string) bool {
	switch status {
	case WorkerTaskStatusComplete, WorkerTaskStatusFailed, WorkerTaskStatusCanceled:
		return true
	}
	return false
}

func HashClaimToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type WorkerTaskRepo struct{ db *sql.DB }

func NewWorkerTaskRepo(db *sql.DB) *WorkerTaskRepo { return &WorkerTaskRepo{db: db} }

func (r *WorkerTaskRepo) DB() *sql.DB { return r.db }

func (r *WorkerTaskRepo) CreateTx(ctx context.Context, tx *sql.Tx, task *WorkerTask) error {
	now := time.Now().UnixMilli()
	if task.CreatedAt == 0 {
		task.CreatedAt = now
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = task.CreatedAt
	}
	if task.AvailableAt == 0 {
		task.AvailableAt = task.CreatedAt
	}
	if task.Status == "" {
		task.Status = WorkerTaskStatusPending
	}
	if task.Priority == 0 {
		task.Priority = 100
	}
	if task.MaxAttempts <= 0 {
		task.MaxAttempts = 2
	}
	if task.PayloadJSON == "" {
		task.PayloadJSON = "{}"
	}
	if task.ResultMetaJSON == "" {
		task.ResultMetaJSON = "{}"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO worker_task_versions DEFAULT VALUES`); err != nil {
		return fmt.Errorf("allocate worker task version: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT last_insert_rowid()`).Scan(&task.VersionNo); err != nil {
		return fmt.Errorf("read worker task version: %w", err)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO worker_tasks (
		id,version_no,worker_type,type,status,priority,scope_type,scope_id,dedupe_key,
		input_hash,payload_json,result_key,result_meta_json,progress_current,progress_total,
		phase,cancel_requested,attempt_count,max_attempts,available_at,claimed_by,
		claim_token_hash,finalize_attempts,error_code,error_message,created_at,updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		task.ID, task.VersionNo, task.WorkerType, task.Type, task.Status, task.Priority,
		task.ScopeType, task.ScopeID, task.DedupeKey, task.InputHash, task.PayloadJSON,
		task.ResultKey, task.ResultMetaJSON, task.ProgressCurrent, task.ProgressTotal,
		task.Phase, boolToInt(task.CancelRequested), task.AttemptCount, task.MaxAttempts,
		task.AvailableAt, task.ClaimedBy, task.ClaimTokenHash, task.FinalizeAttempts,
		task.ErrorCode, task.ErrorMessage, task.CreatedAt, task.UpdatedAt)
	return wrapSQL("insert worker task", err)
}

func IsWorkerTaskUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "constraint failed: UNIQUE")
}

const workerTaskColumns = `id,version_no,worker_type,type,status,priority,scope_type,scope_id,
	dedupe_key,input_hash,payload_json,result_key,result_meta_json,progress_current,progress_total,
	phase,cancel_requested,attempt_count,max_attempts,available_at,claimed_by,claim_token_hash,
	attempt_started_at,heartbeat_at,lease_expires_at,finalize_attempts,next_finalize_at,
	error_code,error_message,created_at,started_at,result_reported_at,pre_completed_at,finished_at,updated_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanWorkerTask(row rowScanner) (WorkerTask, error) {
	var t WorkerTask
	var cancel int
	err := row.Scan(&t.ID, &t.VersionNo, &t.WorkerType, &t.Type, &t.Status, &t.Priority,
		&t.ScopeType, &t.ScopeID, &t.DedupeKey, &t.InputHash, &t.PayloadJSON, &t.ResultKey,
		&t.ResultMetaJSON, &t.ProgressCurrent, &t.ProgressTotal, &t.Phase, &cancel,
		&t.AttemptCount, &t.MaxAttempts, &t.AvailableAt, &t.ClaimedBy, &t.ClaimTokenHash,
		&t.AttemptStartedAt, &t.HeartbeatAt, &t.LeaseExpiresAt, &t.FinalizeAttempts,
		&t.NextFinalizeAt, &t.ErrorCode, &t.ErrorMessage, &t.CreatedAt, &t.StartedAt,
		&t.ResultReportedAt, &t.PreCompletedAt, &t.FinishedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkerTask{}, ErrWorkerTaskNotFound
	}
	if err != nil {
		return WorkerTask{}, wrapSQL("scan worker task", err)
	}
	t.CancelRequested = cancel == 1
	return t, nil
}

func (r *WorkerTaskRepo) GetByID(ctx context.Context, id string) (WorkerTask, error) {
	return scanWorkerTask(r.db.QueryRowContext(ctx, `SELECT `+workerTaskColumns+` FROM worker_tasks WHERE id=?`, id))
}

func (r *WorkerTaskRepo) GetByIDTx(ctx context.Context, tx *sql.Tx, id string) (WorkerTask, error) {
	return scanWorkerTask(tx.QueryRowContext(ctx, `SELECT `+workerTaskColumns+` FROM worker_tasks WHERE id=?`, id))
}

func (r *WorkerTaskRepo) FindActiveByDedupe(ctx context.Context, workerType, taskType, key string) (WorkerTask, error) {
	return scanWorkerTask(r.db.QueryRowContext(ctx, `SELECT `+workerTaskColumns+` FROM worker_tasks
		WHERE worker_type=? AND type=? AND dedupe_key=? AND status IN (?,?,?)
		ORDER BY created_at DESC LIMIT 1`, workerType, taskType, key,
		WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete))
}

func (r *WorkerTaskRepo) FindLatestByDedupe(ctx context.Context, workerType, taskType, key string) (WorkerTask, error) {
	return scanWorkerTask(r.db.QueryRowContext(ctx, `SELECT `+workerTaskColumns+` FROM worker_tasks
		WHERE worker_type=? AND type=? AND dedupe_key=? ORDER BY created_at DESC LIMIT 1`,
		workerType, taskType, key))
}

func (r *WorkerTaskRepo) FindActiveByDedupeTx(
	ctx context.Context,
	tx *sql.Tx,
	workerType, taskType, key string,
) (WorkerTask, error) {
	return scanWorkerTask(tx.QueryRowContext(ctx, `SELECT `+workerTaskColumns+` FROM worker_tasks
		WHERE worker_type=? AND type=? AND dedupe_key=? AND status IN (?,?,?)
		ORDER BY created_at DESC LIMIT 1`, workerType, taskType, key,
		WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete))
}

type WorkerTaskFilter struct {
	WorkerType  string
	Type        string
	Types       []string
	Statuses    []string
	ScopeType   string
	ScopeID     string
	Query       string
	Limit       int
	Offset      int
	AvailableAt *int64
}

// ListClaimable returns a stable priority-ordered page without reserving rows.
// after* describes the last row of the previous page.
//
//nolint:gosec // SQL shape is fixed; only placeholder counts and allowlisted ordering are composed.
func (r *WorkerTaskRepo) ListClaimable(
	ctx context.Context, workerType string, types []string, now int64, limit int,
	afterPriority *int, afterCreatedAt *int64, afterID string,
) ([]WorkerTask, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := []string{"worker_type=?", "status=?", "available_at<=?", "cancel_requested=0", "attempt_count<max_attempts"}
	args := []any{workerType, WorkerTaskStatusPending, now}
	if len(types) > 0 {
		marks := make([]string, len(types))
		for i, typ := range types {
			marks[i] = "?"
			args = append(args, typ)
		}
		where = append(where, "type IN ("+strings.Join(marks, ",")+")")
	}
	if afterPriority != nil && afterCreatedAt != nil && afterID != "" {
		where = append(where, "(priority<? OR (priority=? AND (created_at>? OR (created_at=? AND id>?))))")
		args = append(args, *afterPriority, *afterPriority, *afterCreatedAt, *afterCreatedAt, afterID)
	}
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, `SELECT `+workerTaskColumns+` FROM worker_tasks WHERE `+
		strings.Join(where, " AND ")+` ORDER BY priority DESC,created_at,id LIMIT ?`, args...)
	if err != nil {
		return nil, wrapSQL("list claimable worker tasks", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WorkerTask
	for rows.Next() {
		item, err := scanWorkerTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, wrapSQL("iterate claimable worker tasks", rows.Err())
}

func buildWorkerTaskWhere(f WorkerTaskFilter) (string, []any) {
	var conds []string
	var args []any
	if f.WorkerType != "" {
		conds = append(conds, "worker_type=?")
		args = append(args, f.WorkerType)
	}
	if f.Type != "" {
		conds = append(conds, "type=?")
		args = append(args, f.Type)
	}
	if len(f.Types) > 0 {
		marks := make([]string, len(f.Types))
		for i, taskType := range f.Types {
			marks[i] = "?"
			args = append(args, taskType)
		}
		conds = append(conds, "type IN ("+strings.Join(marks, ",")+")")
	}
	if f.ScopeType != "" {
		conds = append(conds, "scope_type=?")
		args = append(args, f.ScopeType)
	}
	if f.ScopeID != "" {
		conds = append(conds, "scope_id=?")
		args = append(args, f.ScopeID)
	}
	if f.AvailableAt != nil {
		conds = append(conds, "available_at<=?")
		args = append(args, *f.AvailableAt)
	}
	if len(f.Statuses) > 0 {
		p := make([]string, len(f.Statuses))
		for i, status := range f.Statuses {
			p[i] = "?"
			args = append(args, status)
		}
		conds = append(conds, "status IN ("+strings.Join(p, ",")+")")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		escaped := escapeLike(q)
		conds = append(conds, `(id LIKE ? ESCAPE '\' OR dedupe_key LIKE ? ESCAPE '\')`)
		args = append(args, escaped+"%", "%"+escaped+"%")
	}
	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

func (r *WorkerTaskRepo) List(ctx context.Context, f WorkerTaskFilter) ([]WorkerTask, int, error) {
	where, args := buildWorkerTaskWhere(f)
	order := "created_at DESC, id DESC"
	if len(f.Statuses) == 1 && f.Statuses[0] == WorkerTaskStatusPending {
		order = "priority DESC, created_at, id"
	}
	return queryPage(ctx, r.db, `SELECT COUNT(*) FROM worker_tasks `+where,
		`SELECT `+workerTaskColumns+` FROM worker_tasks `+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		args, f.Limit, f.Offset, func(rows *sql.Rows) (WorkerTask, error) { return scanWorkerTask(rows) },
		"count worker tasks", "query worker tasks", "scan worker task", "iterate worker tasks")
}

func (r *WorkerTaskRepo) CountByStatus(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT status,COUNT(*) FROM worker_tasks GROUP BY status`)
	if err != nil {
		return nil, wrapSQL("count worker tasks", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("scan worker task status count: %w", err)
		}
		out[status] = n
	}
	return out, wrapSQL("iterate worker task status counts", rows.Err())
}

func (r *WorkerTaskRepo) CountFinishedSince(ctx context.Context, status string, since int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM worker_tasks WHERE status=? AND finished_at>=?`,
		status,
		since,
	).Scan(&n)
	return n, wrapSQL("count finished worker tasks", err)
}

func (r *WorkerTaskRepo) CountStaleRunning(ctx context.Context, before int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM worker_tasks WHERE status=? AND lease_expires_at<?`,
		WorkerTaskStatusRunning,
		before,
	).Scan(&n)
	return n, wrapSQL("count stale worker tasks", err)
}

func (r *WorkerTaskRepo) ListAttempts(ctx context.Context, taskID string) ([]WorkerTaskAttempt, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT task_id,attempt_no,worker_type,worker_id,claimed_at,
		last_heartbeat_at,released_at,outcome,error_code,error_message FROM worker_task_attempts
		WHERE task_id=? ORDER BY attempt_no`, taskID)
	if err != nil {
		return nil, wrapSQL("list task attempts", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WorkerTaskAttempt
	for rows.Next() {
		var a WorkerTaskAttempt
		if err := rows.Scan(&a.TaskID, &a.AttemptNo, &a.WorkerType, &a.WorkerID, &a.ClaimedAt,
			&a.LastHeartbeatAt, &a.ReleasedAt, &a.Outcome, &a.ErrorCode, &a.ErrorMessage); err != nil {
			return nil, fmt.Errorf("scan worker task attempt: %w", err)
		}
		out = append(out, a)
	}
	return out, wrapSQL("iterate worker task attempts", rows.Err())
}

func (r *WorkerTaskRepo) FindIdempotency(
	ctx context.Context, scopeType, scopeID, taskType, key string,
) (WorkerTask, string, error) {
	var taskID, inputHash string
	err := r.db.QueryRowContext(ctx, `SELECT task_id,input_hash FROM worker_task_idempotency_keys
		WHERE scope_type=? AND scope_id=? AND task_type=? AND idempotency_key=?`,
		scopeType, scopeID, taskType, key).Scan(&taskID, &inputHash)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkerTask{}, "", ErrWorkerTaskNotFound
	}
	if err != nil {
		return WorkerTask{}, "", wrapSQL("find task idempotency", err)
	}
	task, err := r.GetByID(ctx, taskID)
	return task, inputHash, err
}

func (r *WorkerTaskRepo) SaveIdempotency(
	ctx context.Context, tx *sql.Tx, scopeType, scopeID, taskType, key, taskID, inputHash string,
) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO worker_task_idempotency_keys
		(scope_type,scope_id,task_type,idempotency_key,task_id,input_hash,created_at)
		VALUES (?,?,?,?,?,?,?)`, scopeType, scopeID, taskType, key, taskID, inputHash, time.Now().UnixMilli())
	return wrapSQL("save task idempotency", err)
}
