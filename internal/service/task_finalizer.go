package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	taskcore "github.com/fireman/fireman/internal/task"
)

var errTaskCoordinatorNotConfigured = errors.New("task coordinator is not configured")

// Finalization outcome classes drive Go maintenance retry and terminal policy.
const (
	TaskFinalizeSuccess        = "success"
	TaskFinalizeRetryableError = "retryable_error"
	TaskFinalizePermanentError = "permanent_error"
)

// TaskFinalizeResult is the response body of the internal finalization API.
type TaskFinalizeResult struct {
	Result       string `json:"result"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// taskFinalizeError carries an explicit outcome classification through handler
// call chains. Errors that are not taskFinalizeError are classified retryable
// (bounded by the sidecar's retry budget and hard timeout).
type taskFinalizeError struct {
	class   string
	code    string
	message string
}

func (e *taskFinalizeError) Error() string { return e.code + ": " + e.message }

func permanentErr(code, message string) *taskFinalizeError {
	return &taskFinalizeError{class: TaskFinalizePermanentError, code: code, message: message}
}

func retryableErr(code, message string) *taskFinalizeError {
	return &taskFinalizeError{class: TaskFinalizeRetryableError, code: code, message: message}
}

// taskFinalizeRecordStore is the observation sink for finalization records. It is
// an interface so tests can inject failing fakes and prove that recording
// failures never change the returned classification.
type taskFinalizeRecordStore interface {
	Insert(ctx context.Context, rec repository.WorkerTaskFinalizeRecord) error
	DeleteBefore(ctx context.Context, cutoff int64) (int64, error)
}

// taskFinalizeRecordRetention is how long finalization records stay before the
// per-insert cleanup removes them. Kept as a service constant so it can be
// promoted to configuration later.
const taskFinalizeRecordRetention = 30 * 24 * time.Hour

// TaskFinalizer applies pre_complete worker task results to business
// tables. Every handler is re-entrant: the market_data_versions table gates
// writes so repeated notifications (or lost success responses) are safe.
type TaskFinalizer struct {
	sql         *sql.DB
	tasks       *repository.WorkerTaskRepo
	assets      *repository.MarketAssetRepo
	instRepo    *repository.InstrumentRepo
	marketRepo  *repository.MarketDataRepo
	research    *repository.ResearchRepo
	resources   *resourcedb.DB
	records     taskFinalizeRecordStore
	autoUpdates *repository.MarketDataAutoUpdateRepo
	coordinator *taskcore.Coordinator
	finalizers  map[string]func(context.Context, repository.WorkerTask, []byte) error
	now         func() time.Time
}

type finalizeContextKey struct{}

type finalizeContext struct {
	taskID          string
	reservationEnds int64
}

// SetAutoUpdateRepo adds best-effort automatic-update observability. It is
// deliberately optional so finalization correctness never depends on it.
func (s *TaskFinalizer) SetAutoUpdateRepo(repo *repository.MarketDataAutoUpdateRepo) {
	s.autoUpdates = repo
}

func (s *TaskFinalizer) SetCoordinator(coordinator *taskcore.Coordinator) {
	s.coordinator = coordinator
	definitions := coordinator.Registry().DefinitionsFor(repository.WorkerTypeSidecar)
	if len(definitions) != len(s.finalizers) {
		panic("finalizer registry does not match TaskDefinition registry")
	}
	for _, definition := range definitions {
		if _, ok := s.finalizers[definition.FinalizerKey]; !ok {
			panic("missing finalizer for " + definition.Type)
		}
	}
}

func NewTaskFinalizer(
	sqlDB *sql.DB,
	tasks *repository.WorkerTaskRepo,
	assets *repository.MarketAssetRepo,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
	resources *resourcedb.DB,
	records taskFinalizeRecordStore,
) *TaskFinalizer {
	service := &TaskFinalizer{
		sql: sqlDB, tasks: tasks, assets: assets,
		instRepo: instRepo, marketRepo: marketRepo,
		research:  repository.NewResearchRepo(sqlDB),
		resources: resources, records: records,
		now: time.Now,
	}
	service.finalizers = map[string]func(context.Context, repository.WorkerTask, []byte) error{
		repository.WorkerTaskTypeAssetDirectorySync: service.processDirectory,
		repository.WorkerTaskTypeAssetHistorySync:   service.processHistory,
		repository.WorkerTaskTypeFXRateSync:         service.processFXRates,
	}
	return service
}

// processAndRecord runs the finalization pipeline for one task id and returns the
// outcome classification. It never mutates worker_tasks.status; terminal
// transitions belong to TaskCoordinator. Every Go finalizer is appended to
// worker_task_finalize_records for admin observability; recording failures only warn
// and never change the classification (observation faults must not amplify
// into business faults).
func (s *TaskFinalizer) processAndRecord(ctx context.Context, taskID string) TaskFinalizeResult {
	start := s.now()
	res := s.classify(ctx, taskID)
	if res.Result == TaskFinalizeSuccess && s.autoUpdates != nil {
		if err := s.autoUpdates.MarkTaskSuccess(ctx, taskID, s.now().UnixMilli()); err != nil {
			slog.WarnContext(ctx, "mark automatic update success failed", "task_id", taskID, "error", err)
		}
	}
	s.recordFinalization(ctx, taskID, res, s.now().Sub(start))
	return res
}

// Finalize applies one reserved pre_complete result. Every business handler
// reaches withTaskFinalizeTx, which commits the business writes and task
// terminal transition atomically.
func (s *TaskFinalizer) Finalize(
	ctx context.Context, taskID string, reservationEnds int64,
) TaskFinalizeResult {
	ctx = context.WithValue(ctx, finalizeContextKey{}, finalizeContext{
		taskID: taskID, reservationEnds: reservationEnds,
	})
	return s.processAndRecord(ctx, taskID)
}

// classify runs the pipeline and maps errors onto the outcome classes.
func (s *TaskFinalizer) classify(ctx context.Context, taskID string) TaskFinalizeResult {
	res, err := s.process(ctx, taskID)
	if err == nil {
		return res
	}
	var ppe *taskFinalizeError
	if errors.As(err, &ppe) {
		slog.WarnContext(ctx, "finalization classified failure",
			"task_id", taskID, "class", ppe.class, "code", ppe.code, "message", ppe.message)
		return TaskFinalizeResult{Result: ppe.class, ErrorCode: ppe.code, ErrorMessage: ppe.message}
	}
	// Unclassified failures (DB busy, transient IO) are retryable; the
	// Go maintenance bounds retries with backoff and a hard timeout.
	slog.ErrorContext(ctx, "finalization internal failure", "task_id", taskID, "error", err)
	return TaskFinalizeResult{
		Result:       TaskFinalizeRetryableError,
		ErrorCode:    "internal_error",
		ErrorMessage: err.Error(),
	}
}

// recordFinalization appends one observation row and runs retention cleanup.
// task_type / attempt_no are snapshots from the task row; a missing task
// (task_not_found permanent branch) still gets a record — receiving an
// invalid Go finalizer is itself an observable fact.
func (s *TaskFinalizer) recordFinalization(
	ctx context.Context, taskID string, res TaskFinalizeResult, took time.Duration,
) {
	if s.records == nil {
		return
	}
	rec := repository.WorkerTaskFinalizeRecord{
		TaskID:       taskID,
		Result:       res.Result,
		ErrorCode:    res.ErrorCode,
		ErrorMessage: res.ErrorMessage,
		DurationMs:   took.Milliseconds(),
		CreatedAt:    s.now().UnixMilli(),
	}
	if task, err := s.tasks.GetByID(ctx, taskID); err == nil {
		rec.TaskType = task.Type
		rec.AttemptNo = task.FinalizeAttempts
	}
	if err := s.records.Insert(ctx, rec); err != nil {
		slog.WarnContext(ctx, "finalization finalization record insert failed",
			"task_id", taskID, "error", err)
		return
	}
	cutoff := s.now().Add(-taskFinalizeRecordRetention).UnixMilli()
	if _, err := s.records.DeleteBefore(ctx, cutoff); err != nil {
		slog.WarnContext(ctx, "finalization finalization record cleanup failed", "error", err)
	}
}

func (s *TaskFinalizer) process(ctx context.Context, taskID string) (TaskFinalizeResult, error) {
	task, err := s.tasks.GetByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, repository.ErrWorkerTaskNotFound) {
			return TaskFinalizeResult{}, permanentErr("task_not_found", "worker task not found")
		}
		return TaskFinalizeResult{}, fmt.Errorf("load worker task: %w", err)
	}

	switch task.Status {
	case repository.WorkerTaskStatusComplete:
		// A previous success response was lost; re-notification is safe.
		return TaskFinalizeResult{Result: TaskFinalizeSuccess}, nil
	case repository.WorkerTaskStatusPreComplete:
		// proceed
	default:
		return TaskFinalizeResult{}, permanentErr("task_status_invalid",
			fmt.Sprintf("task status is %s, expected pre_complete", task.Status))
	}

	resourceKey := strings.TrimPrefix(strings.TrimSpace(task.ResultKey), "resource:")
	if resourceKey == "" {
		return TaskFinalizeResult{}, permanentErr("invalid_result_data",
			"task result_data has no resource_key")
	}

	raw, _, err := s.resources.ReadByKey(ctx, resourceKey)
	if err != nil {
		return TaskFinalizeResult{}, classifyResourceError(err)
	}

	finalizer, ok := s.finalizers[task.Type]
	if !ok {
		return TaskFinalizeResult{}, permanentErr("unsupported_task_type",
			"unsupported task type "+task.Type)
	}
	err = finalizer(ctx, task, raw)
	if err != nil {
		return TaskFinalizeResult{}, err
	}
	return TaskFinalizeResult{Result: TaskFinalizeSuccess}, nil
}

func classifyResourceError(err error) error {
	switch {
	case errors.Is(err, resourcedb.ErrResourceNotFound):
		return permanentErr("resource_not_found", "result resource not found (possibly expired)")
	case errors.Is(err, resourcedb.ErrChecksumMismatch):
		return permanentErr("resource_checksum_mismatch", err.Error())
	case errors.Is(err, resourcedb.ErrSizeMismatch):
		return permanentErr("resource_size_mismatch", err.Error())
	case errors.Is(err, resourcedb.ErrSchemaVersion):
		return permanentErr("resource_schema_unsupported", err.Error())
	case errors.Is(err, resourcedb.ErrUnsupportedEncoing):
		return permanentErr("resource_encoding_unsupported", err.Error())
	case errors.Is(err, resourcedb.ErrPayloadTooLarge):
		return permanentErr("resource_payload_too_large", err.Error())
	default:
		return retryableErr("resource_read_failed", err.Error())
	}
}

// withTaskFinalizeTx runs fn in a transaction, keeping taskFinalizeError
// classification intact across the transaction boundary.
func (s *TaskFinalizer) withTaskFinalizeTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		finalize, ok := ctx.Value(finalizeContextKey{}).(finalizeContext)
		if !ok {
			return nil
		}
		if s.coordinator == nil {
			return errTaskCoordinatorNotConfigured
		}
		return s.coordinator.CompleteFinalizationTx(
			ctx, tx, finalize.taskID, finalize.reservationEnds, s.now().UnixMilli(),
		)
	})
	if err == nil {
		return nil
	}
	var ppe *taskFinalizeError
	if errors.As(err, &ppe) {
		return ppe
	}
	return fmt.Errorf("finalization transaction: %w", err)
}
