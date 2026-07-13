package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
)

// createOrReuseActiveTaskTx is the single active-task admission gate used by
// business producers. The partial unique index remains the final arbiter when
// concurrent transactions race after the initial lookup.
func createOrReuseActiveTaskTx(
	ctx context.Context,
	tx *sql.Tx,
	tasks *repository.WorkerTaskRepo,
	coordinator *taskcore.Coordinator,
	task repository.WorkerTask,
	createBusiness func() error,
) (repository.WorkerTask, bool, error) {
	existing, err := tasks.FindActiveByDedupeTx(
		ctx, tx, task.WorkerType, task.Type, task.DedupeKey,
	)
	if err == nil {
		return resolveExistingActiveTask(existing, task.InputHash)
	}
	if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return repository.WorkerTask{}, false, fmt.Errorf("find active task: %w", err)
	}

	if err := coordinator.CreateTx(ctx, tx, &task); err != nil {
		if !repository.IsWorkerTaskUniqueConstraint(err) {
			return repository.WorkerTask{}, false, fmt.Errorf("create worker task: %w", err)
		}
		duplicate, findErr := tasks.FindActiveByDedupeTx(
			ctx, tx, task.WorkerType, task.Type, task.DedupeKey,
		)
		if findErr != nil {
			return repository.WorkerTask{}, false, fmt.Errorf("find raced active task: %w", findErr)
		}
		return resolveExistingActiveTask(duplicate, task.InputHash)
	}
	if createBusiness != nil {
		if err := createBusiness(); err != nil {
			return repository.WorkerTask{}, false, err
		}
	}
	return task, false, nil
}

func resolveExistingActiveTask(
	existing repository.WorkerTask, inputHash string,
) (repository.WorkerTask, bool, error) {
	if existing.InputHash == inputHash {
		return existing, true, nil
	}
	return repository.WorkerTask{}, false, taskAlreadyActiveError(existing)
}

func taskAlreadyActiveError(task repository.WorkerTask) error {
	details := map[string]any{
		"task_id": task.ID, "task_type": task.Type,
		"scope_type": task.ScopeType, "scope_id": task.ScopeID,
	}
	if resourceID := taskResourceID(task); resourceID != "" {
		details["resource_id"] = resourceID
	}
	return newErr("task_already_active", "已有同类任务正在执行", details)
}

func taskResourceID(task repository.WorkerTask) string {
	var payload map[string]any
	if json.Unmarshal([]byte(task.PayloadJSON), &payload) == nil {
		for _, key := range []string{"run_id", "optimization_run_id", "simulation_run_id"} {
			if value, ok := payload[key].(string); ok && value != "" {
				return value
			}
		}
	}
	return task.ID
}
