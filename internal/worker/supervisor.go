package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	taskcore "github.com/fireman/fireman/internal/task"
)

const (
	workerPollInterval = 500 * time.Millisecond
	heartbeatInterval  = 10 * time.Second
)

var errProcessorDidNotComplete = errors.New("processor returned without atomically completing its task")

// Supervisor is the go_worker runtime. Lifecycle writes are delegated to the
// coordinator; processors only calculate and commit typed results.
type Supervisor struct {
	coordinator *taskcore.Coordinator
	processors  *ProcessorSet
	logger      *slog.Logger
	workerID    string
	maintenance func() bool
}

func NewSupervisor(
	coordinator *taskcore.Coordinator,
	processors *ProcessorSet,
	logger *slog.Logger,
	maintenance func() bool,
) *Supervisor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Supervisor{
		coordinator: coordinator, processors: processors, logger: logger,
		workerID: "go_worker:" + uuid.NewString(), maintenance: maintenance,
	}
}

func (s *Supervisor) Start(ctx context.Context, concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}
	var group sync.WaitGroup
	for range concurrency {
		group.Add(1)
		go func() {
			defer group.Done()
			s.loop(ctx)
		}()
	}
	group.Wait()
}

func (s *Supervisor) loop(ctx context.Context) {
	ticker := time.NewTicker(workerPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.maintenance != nil && s.maintenance() {
				continue
			}
			items, err := s.coordinator.ListClaimable(ctx, repository.WorkerTypeGo, nil,
				20, nil, nil, "")
			if err != nil {
				s.logger.Error("list go worker tasks failed", "error", err)
				continue
			}
			for _, candidate := range items {
				if s.tryExecute(ctx, candidate.ID) {
					break
				}
			}
		}
	}
}

func (s *Supervisor) tryExecute(parent context.Context, taskID string) bool {
	token := uuid.NewString()
	item, err := s.coordinator.Claim(parent, taskID, taskcore.ClaimRequest{
		WorkerType: repository.WorkerTypeGo, WorkerID: s.workerID, ClaimToken: token,
	})
	if err != nil {
		var taskErr *taskcore.Error
		if errors.As(err, &taskErr) && taskErr.Code == taskcore.ErrClaimConflict {
			return false
		}
		s.logger.Error("claim go worker task failed", "task_id", taskID, "error", err)
		return false
	}
	s.execute(parent, item, token)
	return true
}

//nolint:gocyclo // Attempt execution explicitly covers every lease, cancel and processor terminal path.
func (s *Supervisor) execute(parent context.Context, item repository.WorkerTask, token string) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	state := &attemptState{
		coordinator: s.coordinator, taskID: item.ID, workerID: s.workerID, token: token,
		current: item.ProgressCurrent, total: item.ProgressTotal, phase: item.Phase,
		cancel: cancel,
	}
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				state.heartbeat(ctx)
			}
		}
	}()

	err := s.processors.Execute(ctx, item, Attempt{
		WorkerID: s.workerID, Token: token, Progress: state.progress,
		Canceled: func() bool { return ctx.Err() != nil || state.cancelRequested.Load() },
	})
	cancel()
	<-heartbeatDone

	if state.leaseLost.Load() {
		return
	}
	current, getErr := s.coordinator.Get(context.WithoutCancel(parent), item.ID)
	if getErr == nil && current.Status == repository.WorkerTaskStatusComplete {
		return
	}
	owned := taskcore.OwnedRequest{
		WorkerType: repository.WorkerTypeGo, WorkerID: s.workerID, ClaimToken: token,
	}
	writeCtx, writeCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer writeCancel()
	if parent.Err() != nil && !state.cancelRequested.Load() {
		if _, releaseErr := s.coordinator.Release(writeCtx, item.ID, owned); releaseErr != nil {
			s.logger.Error("release go worker task failed", "task_id", item.ID, "error", releaseErr)
		}
		return
	}
	if state.cancelRequested.Load() || errors.Is(err, context.Canceled) {
		_, reportErr := s.coordinator.Report(writeCtx, item.ID, taskcore.ResultRequest{
			WorkerType: repository.WorkerTypeGo, WorkerID: s.workerID, ClaimToken: token,
			Outcome: "canceled", ErrorCode: repository.WorkerTaskErrorCanceled,
			ErrorMessage: "task canceled by user",
		})
		if reportErr != nil {
			s.logger.Error("report canceled go worker task failed", "task_id", item.ID, "error", reportErr)
		}
		return
	}
	if err == nil {
		err = errProcessorDidNotComplete
	}
	code, message, classifiedRetryable := classifyProcessorError(err)
	definition, definitionErr := s.coordinator.Registry().Require(item.WorkerType, item.Type)
	retryable := classifiedRetryable && definitionErr == nil && definition.RetryClassifier(err)
	_, reportErr := s.coordinator.Report(writeCtx, item.ID, taskcore.ResultRequest{
		WorkerType: repository.WorkerTypeGo, WorkerID: s.workerID, ClaimToken: token,
		Outcome: "failed", Retryable: retryable, ErrorCode: code, ErrorMessage: message,
	})
	if reportErr != nil {
		s.logger.Error("report failed go worker task failed", "task_id", item.ID, "error", reportErr)
	}
}

type attemptState struct {
	mu              sync.Mutex
	coordinator     *taskcore.Coordinator
	taskID          string
	workerID        string
	token           string
	current         int
	total           int
	phase           string
	cancel          context.CancelFunc
	cancelRequested atomic.Bool
	leaseLost       atomic.Bool
}

func (s *attemptState) progress(done, total int, phase string) {
	s.mu.Lock()
	if done > s.current {
		s.current = done
	}
	if total > s.total {
		s.total = total
	}
	if phase != "" {
		s.phase = phase
	}
	s.mu.Unlock()
	s.heartbeat(context.Background())
}

func (s *attemptState) heartbeat(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaseLost.Load() {
		return
	}
	item, err := s.coordinator.Heartbeat(ctx, s.taskID, taskcore.HeartbeatRequest{
		WorkerType: repository.WorkerTypeGo, WorkerID: s.workerID, ClaimToken: s.token,
		ProgressCurrent: s.current, ProgressTotal: s.total, Phase: s.phase,
	})
	if err != nil {
		s.leaseLost.Store(true)
		s.cancel()
		return
	}
	if item.CancelRequested {
		s.cancelRequested.Store(true)
		s.cancel()
	}
}

func classifyProcessorError(err error) (string, string, bool) {
	var taskErr *taskcore.Error
	if errors.As(err, &taskErr) {
		retryable := taskErr.Code != taskcore.ErrPayloadInvalid && taskErr.Code != taskcore.ErrTypeUnsupported
		return taskErr.Code, taskErr.Message, retryable
	}
	var appErr *service.AppError
	if errors.As(err, &appErr) {
		return appErr.Code, appErr.Error(), false
	}
	return "worker_internal_error", fmt.Sprintf("%v", err), true
}
