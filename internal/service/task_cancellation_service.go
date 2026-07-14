package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
)

var errTaskCancellationHandlerMissing = errors.New("task cancellation handler is not registered")

type TaskCancellationSource string

const (
	TaskCancellationUser  TaskCancellationSource = "user"
	TaskCancellationAdmin TaskCancellationSource = "admin"
)

type taskCancellationHandler func(context.Context, *sql.Tx, string, int64) error

// TaskCancellationService owns manual task cancellation and the business
// metadata that must become terminal in the same transaction.
type TaskCancellationService struct {
	db          *sql.DB
	coordinator *taskcore.Coordinator
	handlers    map[string]taskCancellationHandler
	now         func() time.Time
}

func NewTaskCancellationService(
	db *sql.DB,
	coordinator *taskcore.Coordinator,
	research *repository.ResearchRepo,
	improvements *repository.FirePlanImprovementRepo,
	frontiers *repository.FireFrontierRepo,
) *TaskCancellationService {
	noop := func(context.Context, *sql.Tx, string, int64) error { return nil }
	handlers := map[string]taskCancellationHandler{
		repository.WorkerTaskTypeSimulation:         noop,
		repository.WorkerTaskTypeStress:             noop,
		repository.WorkerTaskTypeSensitivity:        noop,
		repository.WorkerTaskTypeAutoUpdateScan:     noop,
		repository.WorkerTaskTypeAssetDirectorySync: noop,
		repository.WorkerTaskTypeAssetHistorySync:   noop,
		repository.WorkerTaskTypeFXRateSync:         noop,
		repository.WorkerTaskTypeInvestmentPath:     noop,
		repository.WorkerTaskTypeFirePlanImprovement: func(
			ctx context.Context, tx *sql.Tx, taskID string, at int64,
		) error {
			return improvements.MarkCanceledByTaskTx(ctx, tx, taskID, at)
		},
		repository.WorkerTaskTypeFireFrontier: func(
			ctx context.Context, tx *sql.Tx, taskID string, at int64,
		) error {
			return frontiers.MarkCanceledAndPruneByTaskTx(ctx, tx, taskID, at, 20)
		},
		repository.WorkerTaskTypeResearchBacktest: func(
			ctx context.Context, tx *sql.Tx, taskID string, at int64,
		) error {
			return research.MarkRunCanceledByTaskTx(ctx, tx, taskID, at)
		},
		repository.WorkerTaskTypeResearchOptimization: func(
			ctx context.Context, tx *sql.Tx, taskID string, at int64,
		) error {
			return research.MarkOptimizationCanceledByTaskTx(ctx, tx, taskID, at)
		},
	}
	for _, definition := range coordinator.Registry().Definitions() {
		if handlers[definition.Type] == nil {
			panic(fmt.Sprintf("task cancellation handler is not registered: %s/%s",
				definition.WorkerType, definition.Type))
		}
	}
	return &TaskCancellationService{
		db: db, coordinator: coordinator, handlers: handlers, now: time.Now,
	}
}

func (s *TaskCancellationService) Cancel(
	ctx context.Context, taskID string, source TaskCancellationSource,
) (repository.WorkerTask, error) {
	code, message := repository.WorkerTaskErrorCanceled, "task canceled by user"
	if source == TaskCancellationAdmin {
		code, message = repository.WorkerTaskErrorCanceledByAdmin, "task canceled by administrator"
	}
	now := s.now().UnixMilli()
	var canceled repository.WorkerTask
	err := fdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		canceled, err = s.coordinator.CancelImmediateTx(ctx, tx, taskID, code, message, now)
		if err != nil {
			return fmt.Errorf("cancel worker task: %w", err)
		}
		handler := s.handlers[canceled.Type]
		if handler == nil {
			return fmt.Errorf("%w: %s", errTaskCancellationHandlerMissing, canceled.Type)
		}
		return handler(ctx, tx, canceled.ID, now)
	})
	if err != nil {
		return repository.WorkerTask{}, fmt.Errorf("cancel task transaction: %w", err)
	}
	_ = s.coordinator.PublishCurrent(context.WithoutCancel(ctx), taskID)
	return canceled, nil
}
