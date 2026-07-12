package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
)

// These helpers drive the same claim and atomic completion path as the Go
// supervisor while keeping research engine tests synchronous.
func (s *ResearchService) ExecuteBacktestJob(
	ctx context.Context, taskID string, cancelCheck func() bool,
	progress func(int, int, string),
) error {
	const workerID = "go_worker:research-test"
	const token = "research-test-token-0001"
	if _, err := s.coordinator.Claim(ctx, taskID, taskcore.ClaimRequest{
		WorkerType: repository.WorkerTypeGo, WorkerID: workerID, ClaimToken: token,
	}); err != nil {
		return err
	}
	run, err := s.research.GetRunByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	err = s.ExecuteBacktestTaskOwned(ctx, taskID, cancelCheck, progress, func(tx *sql.Tx) error {
		return s.coordinator.CompleteOwnedTx(ctx, tx, taskID, workerID,
			repository.HashClaimToken(token), "research_backtest_run:"+run.ID,
			map[string]any{"run_id": run.ID}, time.Now().UnixMilli())
	})
	if err != nil {
		outcome := "failed"
		if errors.Is(err, context.Canceled) {
			outcome = "canceled"
		}
		_, _ = s.coordinator.Report(context.WithoutCancel(ctx), taskID, taskcore.ResultRequest{
			WorkerType: repository.WorkerTypeGo, WorkerID: workerID, ClaimToken: token,
			Outcome: outcome, ErrorCode: "research_test_failed", ErrorMessage: err.Error(),
		})
	}
	return err
}

func (s *ResearchService) ExecuteOptimizationJob(
	ctx context.Context, taskID string, cancelCheck func() bool,
	progress func(int, int, string),
) error {
	const workerID = "go_worker:optimization-test"
	const token = "optimization-test-token-01" //nolint:gosec // Test lease token, not a credential.
	if _, err := s.coordinator.Claim(ctx, taskID, taskcore.ClaimRequest{
		WorkerType: repository.WorkerTypeGo, WorkerID: workerID, ClaimToken: token,
	}); err != nil {
		return err
	}
	run, err := s.research.GetOptimizationRunByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	err = s.ExecuteOptimizationTaskOwned(ctx, taskID, cancelCheck, progress, func(tx *sql.Tx) error {
		return s.coordinator.CompleteOwnedTx(ctx, tx, taskID, workerID,
			repository.HashClaimToken(token), "research_optimization_run:"+run.ID,
			map[string]any{"run_id": run.ID}, time.Now().UnixMilli())
	})
	if err != nil {
		outcome := "failed"
		if errors.Is(err, context.Canceled) {
			outcome = "canceled"
		}
		_, _ = s.coordinator.Report(context.WithoutCancel(ctx), taskID, taskcore.ResultRequest{
			WorkerType: repository.WorkerTypeGo, WorkerID: workerID, ClaimToken: token,
			Outcome: outcome, ErrorCode: "optimization_test_failed", ErrorMessage: err.Error(),
		})
	}
	return err
}
