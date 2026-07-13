package service

import (
	"context"
	"errors"
	"strings"

	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
)

type TaskService struct {
	coordinator *taskcore.Coordinator
}

// TaskView is the public task projection. Payload and ownership credentials
// are deliberately excluded from the public API.
type TaskView struct {
	ID               string `json:"id"`
	WorkerType       string `json:"worker_type"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	Priority         int    `json:"priority"`
	ScopeType        string `json:"scope_type"`
	ScopeID          string `json:"scope_id"`
	ProgressCurrent  int    `json:"progress_current"`
	ProgressTotal    int    `json:"progress_total"`
	Phase            string `json:"phase"`
	CancelRequested  bool   `json:"cancel_requested"`
	AttemptCount     int    `json:"attempt_count"`
	MaxAttempts      int    `json:"max_attempts"`
	ClaimedBy        string `json:"claimed_by,omitempty"`
	HeartbeatAt      *int64 `json:"heartbeat_at,omitempty"`
	LeaseExpiresAt   *int64 `json:"lease_expires_at,omitempty"`
	FinalizeAttempts int    `json:"finalize_attempts"`
	ErrorCode        string `json:"error_code,omitempty"`
	ErrorMessage     string `json:"error_message,omitempty"`
	ResultKey        string `json:"result_key,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	StartedAt        *int64 `json:"started_at,omitempty"`
	FinishedAt       *int64 `json:"finished_at,omitempty"`
	UpdatedAt        int64  `json:"updated_at"`
}

type TaskListParams struct {
	WorkerType string
	Type       string
	Status     string
	ScopeType  string
	ScopeID    string
	Query      string
	Limit      int
	Offset     int
}

func NewTaskService(coordinator *taskcore.Coordinator) *TaskService {
	return &TaskService{coordinator: coordinator}
}

func (s *TaskService) Get(ctx context.Context, taskID string) (TaskView, error) {
	item, err := s.coordinator.Get(ctx, taskID)
	if err != nil {
		return TaskView{}, mapTaskError(err)
	}
	return publicTaskView(item), nil
}

func (s *TaskService) Cancel(ctx context.Context, taskID string) (TaskView, error) {
	item, err := s.coordinator.RequestCancel(ctx, taskID)
	if err != nil {
		return TaskView{}, mapTaskError(err)
	}
	return publicTaskView(item), nil
}

func (s *TaskService) List(ctx context.Context, params TaskListParams) ([]TaskView, int, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 100 {
		params.Limit = 100
	}
	var statuses []string
	switch status := strings.TrimSpace(params.Status); status {
	case "":
	case "active":
		statuses = []string{
			repository.WorkerTaskStatusPending,
			repository.WorkerTaskStatusRunning,
			repository.WorkerTaskStatusPreComplete,
		}
	case repository.WorkerTaskStatusPending,
		repository.WorkerTaskStatusRunning,
		repository.WorkerTaskStatusPreComplete,
		repository.WorkerTaskStatusComplete,
		repository.WorkerTaskStatusFailed,
		repository.WorkerTaskStatusCanceled:
		statuses = []string{status}
	default:
		return nil, 0, newErr("invalid_request",
			"status must be a worker task status or active", nil)
	}
	items, total, err := s.coordinator.List(ctx, repository.WorkerTaskFilter{
		WorkerType: params.WorkerType, Type: params.Type, Statuses: statuses,
		ScopeType: params.ScopeType, ScopeID: params.ScopeID, Query: params.Query,
		Limit: params.Limit, Offset: max(0, params.Offset),
	})
	if err != nil {
		return nil, 0, wrapRepo("list worker tasks", err)
	}
	out := make([]TaskView, len(items))
	for i := range items {
		out[i] = publicTaskView(items[i])
	}
	return out, total, nil
}

func publicTaskView(item repository.WorkerTask) TaskView {
	return TaskView{
		ID: item.ID, WorkerType: item.WorkerType, Type: item.Type, Status: item.Status,
		Priority: item.Priority, ScopeType: item.ScopeType, ScopeID: item.ScopeID,
		ProgressCurrent: item.ProgressCurrent, ProgressTotal: item.ProgressTotal,
		Phase: item.Phase, CancelRequested: item.CancelRequested,
		AttemptCount: item.AttemptCount, MaxAttempts: item.MaxAttempts,
		ClaimedBy: item.ClaimedBy, HeartbeatAt: item.HeartbeatAt,
		LeaseExpiresAt: item.LeaseExpiresAt, FinalizeAttempts: item.FinalizeAttempts,
		ErrorCode: item.ErrorCode, ErrorMessage: item.ErrorMessage, ResultKey: item.ResultKey,
		CreatedAt: item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func (s *TaskService) EventsHub() *taskcore.EventHub { return s.coordinator.Events() }

func mapTaskError(err error) error {
	var taskErr *taskcore.Error
	if errors.As(err, &taskErr) {
		return newErr(taskErr.Code, taskErr.Message, taskErr.Details)
	}
	return wrapRepo("worker task operation", err)
}
