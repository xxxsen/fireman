package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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

// PostProcessService applies pre_complete worker task results to business
// tables. Every handler is re-entrant: the market_data_versions table gates
// writes so repeated notifications (or lost success responses) are safe.
type PostProcessService struct {
	sql        *sql.DB
	tasks      *repository.WorkerTaskRepo
	assets     *repository.MarketAssetRepo
	instRepo   *repository.InstrumentRepo
	marketRepo *repository.MarketDataRepo
	annualRepo *repository.AnnualReturnsRepo
	libMetrics *repository.InstrumentLibraryMetricsRepo
	resources  *resourcedb.DB
}

func NewPostProcessService(
	sqlDB *sql.DB,
	tasks *repository.WorkerTaskRepo,
	assets *repository.MarketAssetRepo,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
	annualRepo *repository.AnnualReturnsRepo,
	libMetrics *repository.InstrumentLibraryMetricsRepo,
	resources *resourcedb.DB,
) *PostProcessService {
	return &PostProcessService{
		sql: sqlDB, tasks: tasks, assets: assets,
		instRepo: instRepo, marketRepo: marketRepo, annualRepo: annualRepo,
		libMetrics: libMetrics, resources: resources,
	}
}

// Process runs the post-process pipeline for one task id and returns the
// outcome classification. It never mutates worker_tasks.status; terminal
// transitions belong to the sidecar.
func (s *PostProcessService) Process(ctx context.Context, taskID string) PostProcessResult {
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
	return err
}
