package task

import (
	"encoding/json"
	"errors"
)

var (
	errResultKeyRequired = errors.New("result_key is required for success")
	errOutcomeInvalid    = errors.New("outcome must be success, failed or canceled")
)

type Error struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func NewError(code, message string, details map[string]any) *Error {
	return &Error{Code: code, Message: message, Details: details}
}

const (
	ErrNotFound           = "task_not_found"
	ErrClaimConflict      = "task_claim_conflict"
	ErrWorkerTypeMismatch = "task_worker_type_mismatch"
	ErrLeaseLost          = "task_lease_lost"
	ErrAlreadyTerminal    = "task_already_terminal"
	ErrCancelRequested    = "task_cancel_requested"
	ErrPayloadInvalid     = "task_payload_invalid"
	ErrTypeUnsupported    = "task_type_unsupported"
	ErrResultKeyInvalid   = "task_result_key_invalid"
	ErrResultConflict     = "task_result_conflict"
	ErrRetryExhausted     = "task_retry_exhausted"
	ErrFinalizeTimeout    = "task_finalize_timeout"
	ErrFinalizeFailed     = "task_finalize_failed"
)

type ClaimRequest struct {
	WorkerType string `json:"worker_type"`
	WorkerID   string `json:"worker_id"`
	ClaimToken string `json:"claim_token"`
}

type HeartbeatRequest struct {
	WorkerType      string `json:"worker_type"`
	WorkerID        string `json:"worker_id"`
	ClaimToken      string `json:"claim_token"`
	ProgressCurrent int    `json:"progress_current"`
	ProgressTotal   int    `json:"progress_total"`
	Phase           string `json:"phase"`
}

type OwnedRequest struct {
	WorkerType string `json:"worker_type"`
	WorkerID   string `json:"worker_id"`
	ClaimToken string `json:"claim_token"`
}

type ResultRequest struct {
	WorkerType   string          `json:"worker_type"`
	WorkerID     string          `json:"worker_id"`
	ClaimToken   string          `json:"claim_token"`
	Outcome      string          `json:"outcome"`
	ResultKey    string          `json:"result_key,omitempty"`
	ResultMeta   json.RawMessage `json:"result_meta,omitempty"`
	Retryable    bool            `json:"retryable,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
}

func (r ResultRequest) Validate() error {
	switch r.Outcome {
	case "success":
		if r.ResultKey == "" {
			return errResultKeyRequired
		}
	case "failed", "canceled":
	default:
		return errOutcomeInvalid
	}
	return nil
}

type Event struct {
	TaskID          string `json:"task_id"`
	Status          string `json:"status"`
	Phase           string `json:"phase,omitempty"`
	ProgressCurrent int    `json:"progress_current"`
	ProgressTotal   int    `json:"progress_total"`
	AttemptCount    int    `json:"attempt_count"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	ResultKey       string `json:"result_key,omitempty"`
}
