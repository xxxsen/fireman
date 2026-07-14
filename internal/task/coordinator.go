package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

const maxErrorMessage = 2000

var (
	errOwnerInvalid      = errors.New("worker_type, worker_id and a claim_token of at least 16 characters are required")
	errProgressInvalid   = errors.New("invalid progress or phase")
	errProgressRegressed = errors.New("task progress cannot regress")
	errRequiresFinalizer = errors.New("task requires finalizer completion")
)

type Coordinator struct {
	db       *sql.DB
	repo     *repository.WorkerTaskRepo
	registry *Registry
	events   *EventHub
	now      func() time.Time
}

// TerminalHook participates in the same transaction that changes an owned
// attempt to failed/canceled. Business tables use it to avoid a terminal task
// with still-active metadata (or the reverse) after a process crash.
type TerminalHook func(context.Context, *sql.Tx, repository.WorkerTask, int64) error

func NewCoordinator(db *sql.DB, repo *repository.WorkerTaskRepo, registry *Registry, events *EventHub) *Coordinator {
	if registry == nil {
		registry = DefaultRegistry()
	}
	if events == nil {
		events = NewEventHub()
	}
	return &Coordinator{db: db, repo: repo, registry: registry, events: events, now: time.Now}
}

func (c *Coordinator) Registry() *Registry { return c.registry }
func (c *Coordinator) Events() *EventHub   { return c.events }

//nolint:wrapcheck // Repository errors are preserved for transaction conflict classification.
func (c *Coordinator) CreateTx(ctx context.Context, tx *sql.Tx, task *repository.WorkerTask) error {
	definition, err := c.registry.Require(task.WorkerType, task.Type)
	if err != nil {
		return err
	}
	if err := definition.ValidatePayload(json.RawMessage(task.PayloadJSON)); err != nil {
		return NewError(ErrPayloadInvalid, err.Error(), nil)
	}
	if task.MaxAttempts == 0 {
		task.MaxAttempts = definition.MaxAttempts
	}
	if task.Priority == 0 {
		task.Priority = definition.DefaultPriority
	}
	if task.ProgressTotal == 0 {
		task.ProgressTotal = definition.InitialProgressTotal
	}
	return c.repo.CreateTx(ctx, tx, task)
}

//nolint:wrapcheck // Not-found is mapped here; other storage errors retain their cause.
func (c *Coordinator) Get(ctx context.Context, id string) (repository.WorkerTask, error) {
	task, err := c.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return repository.WorkerTask{}, NewError(ErrNotFound, "worker task not found", nil)
	}
	return task, err
}

//nolint:lll,wrapcheck // The pass-through preserves repository paging and error identity.
func (c *Coordinator) List(ctx context.Context, filter repository.WorkerTaskFilter) ([]repository.WorkerTask, int, error) {
	return c.repo.List(ctx, filter)
}

//nolint:wrapcheck // Claim-list storage errors are returned intact to the worker protocol.
func (c *Coordinator) ListClaimable(
	ctx context.Context, workerType string, types []string, limit int,
	afterPriority *int, afterCreatedAt *int64, afterID string,
) ([]repository.WorkerTask, error) {
	if !c.registry.SupportsWorkerType(workerType) {
		return nil, NewError(ErrWorkerTypeMismatch, "unsupported worker_type", nil)
	}
	for _, taskType := range types {
		if _, err := c.registry.Require(workerType, taskType); err != nil {
			return nil, err
		}
	}
	return c.repo.ListClaimable(ctx, workerType, types, c.now().UnixMilli(), limit,
		afterPriority, afterCreatedAt, afterID)
}

func validateOwner(workerType, workerID, claimToken string) error {
	if workerType == "" || workerID == "" || len(claimToken) < 16 {
		return errOwnerInvalid
	}
	return nil
}

//nolint:gocyclo,lll,wrapcheck // Claim is one auditable CAS transaction with typed conflicts.
func (c *Coordinator) Claim(ctx context.Context, id string, req ClaimRequest) (repository.WorkerTask, error) {
	if err := validateOwner(req.WorkerType, req.WorkerID, req.ClaimToken); err != nil {
		return repository.WorkerTask{}, err
	}
	tokenHash := repository.HashClaimToken(req.ClaimToken)
	now := c.now().UnixMilli()
	var claimed repository.WorkerTask
	err := fdb.WithTx(ctx, c.db, func(tx *sql.Tx) error {
		current, err := c.repo.GetByIDTx(ctx, tx, id)
		if err != nil {
			if errors.Is(err, repository.ErrWorkerTaskNotFound) {
				return NewError(ErrNotFound, "worker task not found", nil)
			}
			return err
		}
		if current.WorkerType != req.WorkerType {
			return NewError(ErrWorkerTypeMismatch, "task belongs to another worker type", map[string]any{"worker_type": current.WorkerType})
		}
		definition, err := c.registry.Require(current.WorkerType, current.Type)
		if err != nil {
			return err
		}
		if current.Status == repository.WorkerTaskStatusRunning && current.ClaimedBy == req.WorkerID &&
			current.ClaimTokenHash == tokenHash && leaseActive(current, now) {
			claimed = current
			return nil
		}
		if current.Status != repository.WorkerTaskStatusPending || current.AvailableAt > now || current.CancelRequested || current.AttemptCount >= current.MaxAttempts {
			return NewError(ErrClaimConflict, "task is not claimable", map[string]any{"status": current.Status})
		}
		lease := now + definition.LeaseDuration.Milliseconds()
		result, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=?,attempt_count=attempt_count+1,
			claimed_by=?,claim_token_hash=?,attempt_started_at=?,heartbeat_at=?,lease_expires_at=?,
			started_at=COALESCE(started_at,?),updated_at=?,error_code='',error_message=''
			WHERE id=? AND worker_type=? AND status=? AND available_at<=? AND cancel_requested=0 AND attempt_count<max_attempts`,
			repository.WorkerTaskStatusRunning, req.WorkerID, tokenHash, now, now, lease,
			now, now, id, req.WorkerType, repository.WorkerTaskStatusPending, now)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return NewError(ErrClaimConflict, "another worker claimed the task", nil)
		}
		claimed, err = c.repo.GetByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO worker_task_attempts
			(task_id,attempt_no,worker_type,worker_id,claim_token_hash,claimed_at,last_heartbeat_at)
			VALUES (?,?,?,?,?,?,?)`, id, claimed.AttemptCount, req.WorkerType, req.WorkerID, tokenHash, now, now)
		return err
	})
	if err != nil {
		return repository.WorkerTask{}, err
	}
	c.publish(claimed)
	return claimed, nil
}

//nolint:gocyclo,lll,wrapcheck // Heartbeat validates ownership, progress and lease in one CAS transaction.
func (c *Coordinator) Heartbeat(ctx context.Context, id string, req HeartbeatRequest) (repository.WorkerTask, error) {
	if err := validateOwner(req.WorkerType, req.WorkerID, req.ClaimToken); err != nil {
		return repository.WorkerTask{}, err
	}
	if req.ProgressCurrent < 0 || req.ProgressTotal < 0 || len(req.Phase) > 64 {
		return repository.WorkerTask{}, errProgressInvalid
	}
	tokenHash := repository.HashClaimToken(req.ClaimToken)
	now := c.now().UnixMilli()
	var updated repository.WorkerTask
	err := fdb.WithTx(ctx, c.db, func(tx *sql.Tx) error {
		current, err := c.repo.GetByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !owned(current, req.WorkerType, req.WorkerID, tokenHash) || !leaseActive(current, now) {
			return NewError(ErrLeaseLost, "task lease is no longer owned by this worker", nil)
		}
		if req.ProgressCurrent < current.ProgressCurrent || (current.ProgressTotal > 0 && req.ProgressTotal < current.ProgressTotal) {
			return errProgressRegressed
		}
		definition, err := c.registry.Require(current.WorkerType, current.Type)
		if err != nil {
			return err
		}
		lease := now + definition.LeaseDuration.Milliseconds()
		result, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET progress_current=?,progress_total=?,phase=?,
			heartbeat_at=?,lease_expires_at=?,updated_at=? WHERE id=? AND status=? AND claimed_by=? AND claim_token_hash=?`,
			req.ProgressCurrent, req.ProgressTotal, req.Phase, now, lease, now, id,
			repository.WorkerTaskStatusRunning, req.WorkerID, tokenHash)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return NewError(ErrLeaseLost, "task lease was lost", nil)
		}
		attemptResult, err := tx.ExecContext(ctx, `UPDATE worker_task_attempts SET last_heartbeat_at=? WHERE task_id=? AND attempt_no=?`, now, id, current.AttemptCount)
		if err := requireOne(attemptResult, err, ErrLeaseLost, "task attempt heartbeat was not found"); err != nil {
			return err
		}
		updated, err = c.repo.GetByIDTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return repository.WorkerTask{}, err
	}
	c.publish(updated)
	return updated, nil
}

func owned(current repository.WorkerTask, workerType, workerID, tokenHash string) bool {
	return current.Status == repository.WorkerTaskStatusRunning && current.WorkerType == workerType &&
		current.ClaimedBy == workerID && current.ClaimTokenHash == tokenHash
}

func leaseActive(current repository.WorkerTask, now int64) bool {
	return current.LeaseExpiresAt != nil && *current.LeaseExpiresAt >= now
}

// CheckOwned validates an active attempt without extending its lease. It is
// used before accepting a task-bound resource upload.
func (c *Coordinator) CheckOwned(ctx context.Context, id string, req OwnedRequest) (repository.WorkerTask, error) {
	if err := validateOwner(req.WorkerType, req.WorkerID, req.ClaimToken); err != nil {
		return repository.WorkerTask{}, err
	}
	current, err := c.Get(ctx, id)
	if err != nil {
		return repository.WorkerTask{}, err
	}
	if !owned(current, req.WorkerType, req.WorkerID, repository.HashClaimToken(req.ClaimToken)) ||
		!leaseActive(current, c.now().UnixMilli()) {
		return repository.WorkerTask{}, NewError(ErrLeaseLost, "task lease is no longer owned by this worker", nil)
	}
	if current.CancelRequested {
		return repository.WorkerTask{}, NewError(ErrCancelRequested, "task cancellation was requested", nil)
	}
	return current, nil
}

func (c *Coordinator) Release(ctx context.Context, id string, req OwnedRequest) (repository.WorkerTask, error) {
	if err := validateOwner(req.WorkerType, req.WorkerID, req.ClaimToken); err != nil {
		return repository.WorkerTask{}, err
	}
	return c.finishOrRetryOwned(ctx, id, req, "released", "worker_shutdown", "worker released task", true, nil)
}

func (c *Coordinator) Report(ctx context.Context, id string, req ResultRequest) (repository.WorkerTask, error) {
	return c.ReportWithTerminalHook(ctx, id, req, nil)
}

// ReportWithTerminalHook is Report plus an atomic business-metadata hook for
// failed/canceled terminal transitions. Retry scheduling never invokes it.
//
//nolint:gocognit,gocyclo,lll,wrapcheck // Acceptance keeps validation, idempotency and CAS together.
func (c *Coordinator) ReportWithTerminalHook(ctx context.Context, id string, req ResultRequest,
	hook TerminalHook,
) (repository.WorkerTask, error) {
	if err := validateOwner(req.WorkerType, req.WorkerID, req.ClaimToken); err != nil {
		return repository.WorkerTask{}, err
	}
	if req.Outcome == "failed" || req.Outcome == "canceled" {
		if err := req.Validate(); err != nil {
			return repository.WorkerTask{}, err
		}
		return c.finishOrRetryOwned(ctx, id, OwnedRequest{WorkerType: req.WorkerType, WorkerID: req.WorkerID, ClaimToken: req.ClaimToken},
			req.Outcome, req.ErrorCode, req.ErrorMessage, req.Retryable, hook)
	}
	tokenHash := repository.HashClaimToken(req.ClaimToken)
	now := c.now().UnixMilli()
	meta := "{}"
	if len(req.ResultMeta) > 0 {
		meta = string(req.ResultMeta)
	}
	var updated repository.WorkerTask
	err := fdb.WithTx(ctx, c.db, func(tx *sql.Tx) error {
		current, err := c.repo.GetByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if (current.Status == repository.WorkerTaskStatusPreComplete || current.Status == repository.WorkerTaskStatusComplete) && current.ClaimTokenHash == tokenHash {
			if current.ResultKey != req.ResultKey {
				return NewError(ErrResultConflict, "a different result was already accepted", nil)
			}
			updated = current
			return nil
		}
		if !owned(current, req.WorkerType, req.WorkerID, tokenHash) || !leaseActive(current, now) {
			return NewError(ErrLeaseLost, "task lease was lost", nil)
		}
		if current.CancelRequested {
			return NewError(ErrCancelRequested, "task cancellation was requested", nil)
		}
		definition, err := c.registry.Require(current.WorkerType, current.Type)
		if err != nil {
			return err
		}
		if err := definition.ValidateResult(req); err != nil {
			return NewError(ErrResultKeyInvalid, err.Error(), nil)
		}
		if err := definition.ValidateResultKey(req.ResultKey); err != nil {
			return err
		}
		if definition.CompletionMode != CompletionFinalizer {
			return NewError(ErrResultKeyInvalid, "direct Go task results must be committed by their registered result handler", nil)
		}
		result, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=?,result_key=?,result_meta_json=?,
			result_reported_at=?,pre_completed_at=?,next_finalize_at=?,heartbeat_at=NULL,lease_expires_at=NULL,
			phase='finalizing',updated_at=? WHERE id=? AND status=? AND claimed_by=? AND claim_token_hash=? AND cancel_requested=0`,
			repository.WorkerTaskStatusPreComplete, req.ResultKey, meta, now, now, now, now,
			id, repository.WorkerTaskStatusRunning, req.WorkerID, tokenHash)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return NewError(ErrLeaseLost, "task result CAS failed", nil)
		}
		attemptResult, err := tx.ExecContext(ctx, `UPDATE worker_task_attempts SET released_at=?,outcome='result_accepted',
			report_outcome='success',result_key=? WHERE task_id=? AND attempt_no=?`,
			now, req.ResultKey, id, current.AttemptCount)
		if err := requireOne(attemptResult, err, ErrLeaseLost, "task result attempt was not found"); err != nil {
			return err
		}
		updated, err = c.repo.GetByIDTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return repository.WorkerTask{}, err
	}
	c.publish(updated)
	return updated, nil
}

//nolint:gocognit,gocyclo,lll,wrapcheck // Terminal and retry states commit atomically.
func (c *Coordinator) finishOrRetryOwned(ctx context.Context, id string, req OwnedRequest,
	outcome, code, message string, retryable bool, hook TerminalHook,
) (repository.WorkerTask, error) {
	tokenHash := repository.HashClaimToken(req.ClaimToken)
	now := c.now().UnixMilli()
	var updated repository.WorkerTask
	err := fdb.WithTx(ctx, c.db, func(tx *sql.Tx) error {
		current, err := c.repo.GetByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if !owned(current, req.WorkerType, req.WorkerID, tokenHash) {
			attempt, findErr := c.repo.FindAttemptByTokenTx(ctx, tx, id, tokenHash)
			if findErr == nil && attempt.WorkerType == req.WorkerType && attempt.WorkerID == req.WorkerID &&
				attempt.ReportOutcome == outcome {
				updated = current
				return nil
			}
			if findErr == nil && attempt.ReportOutcome != "" {
				return NewError(ErrResultConflict, "a different outcome was already accepted for this attempt", nil)
			}
			return NewError(ErrLeaseLost, "task lease was lost", nil)
		}
		if !leaseActive(current, now) {
			return NewError(ErrLeaseLost, "task lease was lost", nil)
		}
		decision := decideOwnedFinish(current, outcome, code, message, retryable, now)
		result, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=?,available_at=?,claimed_by=?,claim_token_hash=?,
			attempt_started_at=NULL,heartbeat_at=NULL,lease_expires_at=NULL,progress_current=0,phase='',
			error_code=?,error_message=?,finished_at=?,updated_at=? WHERE id=? AND status=? AND claimed_by=? AND claim_token_hash=?`,
			decision.status, decision.availableAt, "", "", decision.code, decision.message,
			decision.finishedAt, now, id,
			repository.WorkerTaskStatusRunning, req.WorkerID, tokenHash)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return NewError(ErrLeaseLost, "task result CAS failed", nil)
		}
		attemptResult, err := tx.ExecContext(ctx, `UPDATE worker_task_attempts SET released_at=?,outcome=?,report_outcome=?,error_code=?,error_message=?
			WHERE task_id=? AND attempt_no=?`, now, decision.attemptOutcome, outcome,
			decision.code, decision.message, id, current.AttemptCount)
		if err := requireOne(attemptResult, err, ErrLeaseLost, "task result attempt was not found"); err != nil {
			return err
		}
		updated, err = c.repo.GetByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if hook != nil && repository.IsTerminalWorkerTaskStatus(updated.Status) {
			return hook(ctx, tx, updated, now)
		}
		return nil
	})
	if err != nil {
		return repository.WorkerTask{}, err
	}
	c.publish(updated)
	return updated, nil
}

type ownedFinishDecision struct {
	status         string
	availableAt    int64
	attemptOutcome string
	code           string
	message        string
	finishedAt     any
}

func decideOwnedFinish(current repository.WorkerTask, outcome, code, message string,
	retryable bool, now int64,
) ownedFinishDecision {
	decision := ownedFinishDecision{
		status:         repository.WorkerTaskStatusFailed,
		availableAt:    current.AvailableAt,
		attemptOutcome: outcome,
		code:           code,
		message:        message,
	}
	switch {
	case outcome == "canceled" || current.CancelRequested:
		decision.status = repository.WorkerTaskStatusCanceled
		if decision.code == "" {
			decision.code = repository.WorkerTaskErrorCanceled
			decision.message = "task canceled by user"
		}
	case retryable && current.AttemptCount < current.MaxAttempts:
		decision.status = repository.WorkerTaskStatusPending
		decision.availableAt = now + retryBackoff(current.AttemptCount).Milliseconds()
		decision.attemptOutcome = "retry_scheduled"
	case retryable:
		if decision.code != "" {
			decision.message = decision.code + ": " + decision.message
		}
		decision.code = ErrRetryExhausted
		decision.attemptOutcome = "retry_exhausted"
	}
	if len(decision.message) > maxErrorMessage {
		decision.message = decision.message[:maxErrorMessage]
	}
	if repository.IsTerminalWorkerTaskStatus(decision.status) {
		decision.finishedAt = now
	}
	return decision
}

func retryBackoff(attempt int) time.Duration {
	seconds := math.Pow(2, float64(max(1, attempt)))
	return time.Duration(min(seconds, 300)) * time.Second
}

//nolint:wrapcheck // Cancellation and attempt fencing commit in one transaction.
func (c *Coordinator) RequestCancel(ctx context.Context, id string) (repository.WorkerTask, error) {
	now := c.now().UnixMilli()
	var updated repository.WorkerTask
	err := fdb.WithTx(ctx, c.db, func(tx *sql.Tx) error {
		var err error
		updated, err = c.CancelImmediateTx(ctx, tx, id, repository.WorkerTaskErrorCanceled,
			"task canceled by user", now)
		return err
	})
	if err != nil {
		return repository.WorkerTask{}, err
	}
	c.publish(updated)
	return updated, nil
}

// CancelImmediateTx makes every active task terminal and fences its current
// attempt before returning. It deliberately does not publish an event because
// the caller still owns the surrounding transaction.
//
//nolint:lll,wrapcheck // Task and attempt state are deliberately updated together.
func (c *Coordinator) CancelImmediateTx(
	ctx context.Context, tx *sql.Tx, id, code, message string, now int64,
) (repository.WorkerTask, error) {
	current, err := c.repo.GetByIDTx(ctx, tx, id)
	if err != nil {
		return repository.WorkerTask{}, err
	}
	if current.Status == repository.WorkerTaskStatusCanceled {
		return current, nil
	}
	if repository.IsTerminalWorkerTaskStatus(current.Status) {
		return repository.WorkerTask{}, NewError(ErrAlreadyTerminal, "task already finished",
			map[string]any{"status": current.Status})
	}
	if code == "" {
		code = repository.WorkerTaskErrorCanceled
	}
	if len(message) > maxErrorMessage {
		message = message[:maxErrorMessage]
	}
	result, updateErr := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=?,cancel_requested=1,finished_at=?,
		error_code=?,error_message=?,next_finalize_at=NULL,claimed_by='',claim_token_hash='',
		attempt_started_at=NULL,heartbeat_at=NULL,lease_expires_at=NULL,updated_at=?
		WHERE id=? AND status=?`, repository.WorkerTaskStatusCanceled, now, code, message, now, id, current.Status)
	if err := requireOne(result, updateErr, ErrAlreadyTerminal, "task changed while cancellation was requested"); err != nil {
		return repository.WorkerTask{}, err
	}
	if current.Status == repository.WorkerTaskStatusRunning {
		_, err = tx.ExecContext(ctx, `UPDATE worker_task_attempts SET released_at=?,outcome='canceled',
			report_outcome='canceled',error_code=?,error_message=?
			WHERE task_id=? AND attempt_no=? AND released_at IS NULL`,
			now, code, message, id, current.AttemptCount)
		if err != nil {
			return repository.WorkerTask{}, err
		}
	}
	updated, err := c.repo.GetByIDTx(ctx, tx, id)
	return updated, err
}

// RequestCancelTx applies cancellation inside a producer transaction. It is
// used when a newer analysis supersedes an older task and the cancellation,
// old result removal and replacement task creation must commit together.
func (c *Coordinator) RequestCancelTx(
	ctx context.Context, tx *sql.Tx, id, code, message string, now int64,
) error {
	_, err := c.CancelImmediateTx(ctx, tx, id, code, message, now)
	if errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return nil
	}
	var taskErr *Error
	if errors.As(err, &taskErr) && taskErr.Code == ErrAlreadyTerminal {
		return nil
	}
	return err
}

func (c *Coordinator) publish(value repository.WorkerTask) {
	c.events.Publish(Event{
		TaskID: value.ID, Status: value.Status, Phase: value.Phase,
		ProgressCurrent: value.ProgressCurrent, ProgressTotal: value.ProgressTotal,
		AttemptCount: value.AttemptCount, ErrorCode: value.ErrorCode,
		ErrorMessage: value.ErrorMessage, ResultKey: value.ResultKey,
	})
}

func (c *Coordinator) PublishCurrent(ctx context.Context, id string) error {
	item, err := c.Get(ctx, id)
	if err != nil {
		return err
	}
	c.publish(item)
	return nil
}

//nolint:lll,wrapcheck // Direct completion atomically commits task and attempt state.
func (c *Coordinator) CompleteOwnedTx(ctx context.Context, tx *sql.Tx, id, workerID, tokenHash, resultKey string, resultMeta any, now int64) error {
	current, err := c.repo.GetByIDTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if !owned(current, repository.WorkerTypeGo, workerID, tokenHash) || !leaseActive(current, now) {
		return NewError(ErrLeaseLost, "task lease was lost", nil)
	}
	if current.CancelRequested {
		return NewError(ErrCancelRequested, "task cancellation was requested", nil)
	}
	definition, err := c.registry.Require(current.WorkerType, current.Type)
	if err != nil {
		return err
	}
	if definition.CompletionMode != CompletionDirect {
		return errRequiresFinalizer
	}
	if err := definition.ValidateResultKey(resultKey); err != nil {
		return err
	}
	meta, err := json.Marshal(resultMeta)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=?,result_key=?,result_meta_json=?,
		result_reported_at=?,finished_at=?,progress_current=progress_total,
		heartbeat_at=NULL,lease_expires_at=NULL,phase='',updated_at=?
		WHERE id=? AND status=? AND claimed_by=? AND claim_token_hash=? AND cancel_requested=0`,
		repository.WorkerTaskStatusComplete, resultKey, string(meta), now, now, now,
		id, repository.WorkerTaskStatusRunning, workerID, tokenHash)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return NewError(ErrLeaseLost, "complete task CAS failed", nil)
	}
	attemptResult, err := tx.ExecContext(ctx, `UPDATE worker_task_attempts SET released_at=?,outcome='complete',
		report_outcome='success',result_key=? WHERE task_id=? AND attempt_no=?`,
		now, resultKey, id, current.AttemptCount)
	return requireOne(attemptResult, err, ErrLeaseLost, "task completion attempt was not found")
}

//nolint:wrapcheck // SQL errors stay rollback causes; zero rows become a typed protocol error.
func requireOne(result sql.Result, err error, code, message string) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return NewError(code, message, nil)
	}
	return nil
}

func SanitizeError(code, message string) (string, string) {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if code == "" {
		code = "worker_failed"
	}
	if len(message) > maxErrorMessage {
		message = message[:maxErrorMessage]
	}
	return code, message
}
