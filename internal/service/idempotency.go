package service

import (
	"context"
	"errors"

	"github.com/fireman/fireman/internal/repository"
)

func findExistingIdempotentTask(
	ctx context.Context,
	tasks *repository.WorkerTaskRepo,
	scopeID, taskType, key, inputHash, findErrMsg string,
) (repository.WorkerTask, bool, error) {
	if key == "" {
		return repository.WorkerTask{}, false, nil
	}
	existing, storedHash, err := tasks.FindIdempotency(ctx, "plan", scopeID, taskType, key)
	if err == nil {
		if storedHash != inputHash {
			return repository.WorkerTask{}, false, newErr(
				"idempotency_conflict", "idempotency key reused with different input", nil,
			)
		}
		return existing, true, nil
	}
	if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return repository.WorkerTask{}, false, wrapRepo(findErrMsg, err)
	}
	return repository.WorkerTask{}, false, nil
}
