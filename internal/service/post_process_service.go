package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
)

// Post-process outcome classes. The sidecar drives the task terminal state
// from these: success -> complete, permanent_error -> failed,
// retryable_error -> exponential backoff retry.
const (
	PostProcessSuccess        = "success"
	PostProcessRetryableError = "retryable_error"
	PostProcessPermanentError = "permanent_error"
)

// PostProcessResult is the response body of the internal post-process API.
type PostProcessResult struct {
	Result       string `json:"result"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// postProcessError carries an explicit outcome classification through handler
// call chains. Errors that are not postProcessError are classified retryable
// (bounded by the sidecar's retry budget and hard timeout).
type postProcessError struct {
	class   string
	code    string
	message string
}

func (e *postProcessError) Error() string { return e.code + ": " + e.message }

func permanentErr(code, message string) *postProcessError {
	return &postProcessError{class: PostProcessPermanentError, code: code, message: message}
}

func retryableErr(code, message string) *postProcessError {
	return &postProcessError{class: PostProcessRetryableError, code: code, message: message}
}

// postProcessRecordStore is the observation sink for callback records. It is
// an interface so tests can inject failing fakes and prove that recording
// failures never change the returned classification.
type postProcessRecordStore interface {
	Insert(ctx context.Context, rec repository.PostProcessRecord) error
	DeleteBefore(ctx context.Context, cutoff int64) (int64, error)
}

// postProcessRecordRetention is how long callback records stay before the
// per-insert cleanup removes them. Kept as a service constant so it can be
// promoted to configuration later.
const postProcessRecordRetention = 30 * 24 * time.Hour

// PostProcessService applies pre_complete worker task results to business
// tables. Every handler is re-entrant: the market_data_versions table gates
// writes so repeated notifications (or lost success responses) are safe.
type PostProcessService struct {
	sql        *sql.DB
	tasks      *repository.WorkerTaskRepo
	assets     *repository.MarketAssetRepo
	instRepo   *repository.InstrumentRepo
	marketRepo *repository.MarketDataRepo
	resources  *resourcedb.DB
	records    postProcessRecordStore
	now        func() time.Time
}

func NewPostProcessService(
	sqlDB *sql.DB,
	tasks *repository.WorkerTaskRepo,
	assets *repository.MarketAssetRepo,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
	resources *resourcedb.DB,
	records postProcessRecordStore,
) *PostProcessService {
	return &PostProcessService{
		sql: sqlDB, tasks: tasks, assets: assets,
		instRepo: instRepo, marketRepo: marketRepo,
		resources: resources, records: records,
		now: time.Now,
	}
}

// Process runs the post-process pipeline for one task id and returns the
// outcome classification. It never mutates worker_tasks.status; terminal
// transitions belong to the sidecar. Every callback is appended to
// post_process_records for admin observability; recording failures only warn
// and never change the classification (observation faults must not amplify
// into business faults).
func (s *PostProcessService) Process(ctx context.Context, taskID string) PostProcessResult {
	start := s.now()
	res := s.classify(ctx, taskID)
	s.recordCallback(ctx, taskID, res, s.now().Sub(start))
	return res
}

// classify runs the pipeline and maps errors onto the outcome classes.
func (s *PostProcessService) classify(ctx context.Context, taskID string) PostProcessResult {
	res, err := s.process(ctx, taskID)
	if err == nil {
		return res
	}
	var ppe *postProcessError
	if errors.As(err, &ppe) {
		slog.WarnContext(ctx, "post-process classified failure",
			"task_id", taskID, "class", ppe.class, "code", ppe.code, "message", ppe.message)
		return PostProcessResult{Result: ppe.class, ErrorCode: ppe.code, ErrorMessage: ppe.message}
	}
	// Unclassified failures (DB busy, transient IO) are retryable; the
	// sidecar bounds retries with backoff and a hard timeout.
	slog.ErrorContext(ctx, "post-process internal failure", "task_id", taskID, "error", err)
	return PostProcessResult{
		Result:       PostProcessRetryableError,
		ErrorCode:    "internal_error",
		ErrorMessage: err.Error(),
	}
}

// recordCallback appends one observation row and runs retention cleanup.
// task_type / attempt_no are snapshots from the task row; a missing task
// (task_not_found permanent branch) still gets a record — receiving an
// invalid callback is itself an observable fact.
func (s *PostProcessService) recordCallback(
	ctx context.Context, taskID string, res PostProcessResult, took time.Duration,
) {
	if s.records == nil {
		return
	}
	rec := repository.PostProcessRecord{
		TaskID:       taskID,
		Result:       res.Result,
		ErrorCode:    res.ErrorCode,
		ErrorMessage: res.ErrorMessage,
		DurationMs:   took.Milliseconds(),
		CreatedAt:    s.now().UnixMilli(),
	}
	if task, err := s.tasks.GetByID(ctx, taskID); err == nil {
		rec.TaskType = task.Type
		rec.AttemptNo = task.PostProcessAttempts
	}
	if err := s.records.Insert(ctx, rec); err != nil {
		slog.WarnContext(ctx, "post-process callback record insert failed",
			"task_id", taskID, "error", err)
		return
	}
	cutoff := s.now().Add(-postProcessRecordRetention).UnixMilli()
	if _, err := s.records.DeleteBefore(ctx, cutoff); err != nil {
		slog.WarnContext(ctx, "post-process callback record cleanup failed", "error", err)
	}
}

func (s *PostProcessService) process(ctx context.Context, taskID string) (PostProcessResult, error) {
	task, err := s.tasks.GetByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, repository.ErrWorkerTaskNotFound) {
			return PostProcessResult{}, permanentErr("task_not_found", "worker task not found")
		}
		return PostProcessResult{}, fmt.Errorf("load worker task: %w", err)
	}

	switch task.Status {
	case repository.WorkerTaskStatusComplete:
		// A previous success response was lost; re-notification is safe.
		return PostProcessResult{Result: PostProcessSuccess}, nil
	case repository.WorkerTaskStatusPreComplete:
		// proceed
	default:
		return PostProcessResult{}, permanentErr("task_status_invalid",
			fmt.Sprintf("task status is %s, expected pre_complete", task.Status))
	}

	if strings.TrimSpace(task.ResultData) == "" {
		return PostProcessResult{}, permanentErr("invalid_result_data", "task result_data is empty")
	}
	var env resourcedb.Envelope
	if err := json.Unmarshal([]byte(task.ResultData), &env); err != nil {
		return PostProcessResult{}, permanentErr("invalid_result_data",
			"task result_data is not a valid resource envelope: "+err.Error())
	}
	if env.ResourceKey == "" {
		return PostProcessResult{}, permanentErr("invalid_result_data",
			"task result_data has no resource_key")
	}

	raw, err := s.resources.Read(ctx, env)
	if err != nil {
		return PostProcessResult{}, classifyResourceError(err)
	}

	switch task.Type {
	case repository.WorkerTaskTypeAssetDirectorySync:
		err = s.processDirectory(ctx, task, raw)
	case repository.WorkerTaskTypeAssetHistorySync:
		err = s.processHistory(ctx, task, raw)
	case repository.WorkerTaskTypeFXRateSync:
		err = s.processFXRates(ctx, task, raw)
	default:
		return PostProcessResult{}, permanentErr("unsupported_task_type",
			"unsupported task type "+task.Type)
	}
	if err != nil {
		return PostProcessResult{}, err
	}
	return PostProcessResult{Result: PostProcessSuccess}, nil
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

// withPostProcessTx runs fn in a transaction, keeping postProcessError
// classification intact across the transaction boundary.
func (s *PostProcessService) withPostProcessTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	err := fdb.WithTx(ctx, s.sql, fn)
	if err == nil {
		return nil
	}
	var ppe *postProcessError
	if errors.As(err, &ppe) {
		return ppe
	}
	return fmt.Errorf("post-process transaction: %w", err)
}
