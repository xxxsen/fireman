package service

import (
	"context"
	"errors"

	"github.com/fireman/fireman/internal/repository"
)

func findExistingIdempotentJob(
	ctx context.Context,
	jobs *repository.JobRepo,
	planID, jobType, key, inputHash, findErrMsg string,
) (repository.Job, bool, error) {
	if key == "" {
		return repository.Job{}, false, nil
	}
	existing, storedHash, err := jobs.FindIdempotency(ctx, planID, jobType, key)
	if err == nil {
		if storedHash != inputHash {
			return repository.Job{}, false, newErr(
				"idempotency_conflict",
				"idempotency key reused with different input",
				nil,
			)
		}
		return existing, true, nil
	}
	if !errors.Is(err, repository.ErrJobNotFound) {
		return repository.Job{}, false, wrapRepo(findErrMsg, err)
	}
	return repository.Job{}, false, nil
}
