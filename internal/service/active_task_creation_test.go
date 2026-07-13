package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
	"github.com/fireman/fireman/internal/testutil"
)

func TestStableActiveTaskAdmissionForEveryRegisteredType(t *testing.T) {
	tests := []struct {
		workerType string
		taskType   string
		dedupeKey  string
	}{
		{repository.WorkerTypeGo, repository.WorkerTaskTypeSimulation, "simulation|plan:plan_1"},
		{repository.WorkerTypeGo, repository.WorkerTaskTypeStress, "stress|simulation_run:sim_1"},
		{repository.WorkerTypeGo, repository.WorkerTaskTypeSensitivity, "sensitivity|simulation_run:sim_1"},
		{repository.WorkerTypeGo, repository.WorkerTaskTypeFirePlanImprovement, "fire_plan_improvement|plan:plan_1"},
		{repository.WorkerTypeGo, repository.WorkerTaskTypeResearchBacktest, "research_backtest|collection:rc_1"},
		{repository.WorkerTypeGo, repository.WorkerTaskTypeResearchOptimization, "research_optimization_backtest|collection:rc_1"},
		{repository.WorkerTypeGo, repository.WorkerTaskTypeAutoUpdateScan, "market_data_auto_update_scan|system"},
		{repository.WorkerTypeSidecar, repository.WorkerTaskTypeAssetDirectorySync, "asset_directory_sync|cn_exchange_stock"},
		{repository.WorkerTypeSidecar, repository.WorkerTaskTypeAssetHistorySync, "asset_history_sync|CN|cn_exchange_stock|sh|600000|qfq|close"},
		{repository.WorkerTypeSidecar, repository.WorkerTaskTypeFXRateSync, "fx_rate_sync|system"},
	}

	for _, tc := range tests {
		t.Run(tc.taskType, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			tasks := repository.NewWorkerTaskRepo(db)
			coordinator := taskcore.NewCoordinator(db, tasks, taskcore.DefaultRegistry(), taskcore.NewEventHub())
			ctx := context.Background()

			type result struct {
				task   repository.WorkerTask
				reused bool
				err    error
			}
			start := make(chan struct{})
			results := make(chan result, 2)
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Go(func() {
					<-start
					var bound repository.WorkerTask
					var reused bool
					err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
						var createErr error
						bound, reused, createErr = createOrReuseActiveTaskTx(
							ctx, tx, tasks, coordinator, repository.WorkerTask{
								ID: fmt.Sprintf("task_%d", i), WorkerType: tc.workerType,
								Type: tc.taskType, Status: repository.WorkerTaskStatusPending,
								ScopeType: "test", ScopeID: "scope_1", DedupeKey: tc.dedupeKey,
								InputHash: "same-input", PayloadJSON: `{}`,
							}, nil,
						)
						return createErr
					})
					results <- result{task: bound, reused: reused, err: err}
				})
			}
			close(start)
			wg.Wait()
			close(results)

			var got []result
			for item := range results {
				if item.err != nil {
					t.Fatalf("concurrent admission failed: %v", item.err)
				}
				got = append(got, item)
			}
			if len(got) != 2 || got[0].task.ID != got[1].task.ID {
				t.Fatalf("requests did not converge on one task: %+v", got)
			}
			if got[0].reused == got[1].reused {
				t.Fatalf("want one create and one reuse: %+v", got)
			}
			_, total, err := tasks.List(ctx, repository.WorkerTaskFilter{
				WorkerType: tc.workerType, Type: tc.taskType, Limit: 10,
			})
			if err != nil || total != 1 {
				t.Fatalf("active task count=%d err=%v, want 1", total, err)
			}

			conflictErr := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
				_, _, err := createOrReuseActiveTaskTx(
					ctx, tx, tasks, coordinator, repository.WorkerTask{
						ID: "task_conflict", WorkerType: tc.workerType, Type: tc.taskType,
						Status: repository.WorkerTaskStatusPending, ScopeType: "test", ScopeID: "scope_1",
						DedupeKey: tc.dedupeKey, InputHash: "different-input", PayloadJSON: `{}`,
					}, nil,
				)
				return err
			})
			var appErr *AppError
			if !errors.As(conflictErr, &appErr) || appErr.Code != "task_already_active" {
				t.Fatalf("different input error=%v, want task_already_active", conflictErr)
			}
			if appErr.Details["task_id"] != got[0].task.ID {
				t.Fatalf("conflict details=%+v", appErr.Details)
			}
		})
	}
}
