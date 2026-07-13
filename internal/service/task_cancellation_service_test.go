package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
	"github.com/fireman/fireman/internal/testutil"
)

func TestTaskCancellationClosesBusinessRunMetadata(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	tasks := repository.NewWorkerTaskRepo(db)
	coordinator := taskcore.NewCoordinator(db, tasks, taskcore.DefaultRegistry(), taskcore.NewEventHub())
	research := repository.NewResearchRepo(db)
	improvements := repository.NewFirePlanImprovementRepo(db)
	frontiers := repository.NewFireFrontierRepo(db)
	cancellation := NewTaskCancellationService(db, coordinator, research, improvements, frontiers)
	cancellation.now = func() time.Time { return time.UnixMilli(123456) }

	if _, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,valuation_date,created_at,updated_at)
		VALUES ('plan_cancel','plan','2026-07-13',1,1);
		INSERT INTO research_collections (id,name,created_at,updated_at)
		VALUES ('collection_cancel','collection',1,1)`); err != nil {
		t.Fatal(err)
	}
	type fixture struct {
		taskID   string
		taskType string
		insert   func(*sql.Tx) error
		table    string
	}
	fixtures := []fixture{
		{
			taskID: "task_cancel_frontier", taskType: repository.WorkerTaskTypeFireFrontier,
			table: "fire_frontier_runs",
			insert: func(tx *sql.Tx) error {
				return frontiers.CreateTx(ctx, tx, &repository.FireFrontierRun{
					ID: "frontier_cancel", TaskID: "task_cancel_frontier", PlanID: "plan_cancel",
					SourceSimulationRunID: "simulation", InputHash: "input", AlgorithmVersion: "v1",
					FrontierType: "required_current_assets", SourceEngineVersion: "v1",
					SourceConfigHash: "config", SourceMarketHash: "market", EvaluationRuns: 1000,
					ConfigJSON: `{}`, InputSnapshotJSON: `{}`, CreatedAt: 1,
				})
			},
		},
		{
			taskID: "task_cancel_improvement", taskType: repository.WorkerTaskTypeFirePlanImprovement,
			table: "fire_plan_improvement_runs",
			insert: func(tx *sql.Tx) error {
				return improvements.CreateTx(ctx, tx, &repository.FirePlanImprovementRun{
					ID: "improvement_cancel", TaskID: "task_cancel_improvement", PlanID: "plan_cancel",
					SourceSimulationRunID: "simulation", InputHash: "input", AlgorithmVersion: "v1",
					SourceEngineVersion: "v1", SourceConfigHash: "config", SourceMarketHash: "market",
					ConfigJSON: `{}`, InputSnapshotJSON: `{}`, CreatedAt: 1,
				})
			},
		},
		{
			taskID: "task_cancel_backtest", taskType: repository.WorkerTaskTypeResearchBacktest,
			table: "research_backtest_runs",
			insert: func(tx *sql.Tx) error {
				return research.CreateRunTx(ctx, tx, repository.ResearchBacktestRun{
					ID: "backtest_cancel", CollectionID: "collection_cancel", TaskID: "task_cancel_backtest",
					InputHash: "input", InputSnapshotJSON: `{}`, SourceHash: "source", EngineVersion: "v1",
					BaseCurrency: "CNY", RebalancePolicy: "monthly", WindowStart: "2020-01-01",
					WindowEnd: "2024-01-01", SummaryJSON: `{}`, DataQualityJSON: `{}`, CreatedAt: 1,
				})
			},
		},
		{
			taskID: "task_cancel_optimization", taskType: repository.WorkerTaskTypeResearchOptimization,
			table: "research_optimization_runs",
			insert: func(tx *sql.Tx) error {
				return research.CreateOptimizationRunTx(ctx, tx, repository.ResearchOptimizationRun{
					ID: "optimization_cancel", CollectionID: "collection_cancel", TaskID: "task_cancel_optimization",
					InputHash: "input", SourceHash: "source", EngineVersion: "v1", BaseCurrency: "CNY",
					RebalancePolicy: "monthly", WindowStart: "2020-01-01", WindowEnd: "2024-01-01",
					ConfigJSON: `{}`, InputSnapshotJSON: `{}`, ResultJSON: `{}`, CreatedAt: 1,
				})
			},
		},
	}

	for _, item := range fixtures {
		t.Run(item.taskType, func(t *testing.T) {
			err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
				if err := coordinator.CreateTx(ctx, tx, &repository.WorkerTask{
					ID: item.taskID, WorkerType: repository.WorkerTypeGo, Type: item.taskType,
					Status: repository.WorkerTaskStatusPending, ScopeType: "test", ScopeID: "cancel",
					DedupeKey: item.taskID, PayloadJSON: `{}`,
				}); err != nil {
					return err
				}
				return item.insert(tx)
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cancellation.Cancel(ctx, item.taskID, TaskCancellationUser); err != nil {
				t.Fatal(err)
			}
			var completedAt sql.NullInt64
			if err := db.QueryRowContext(ctx, "SELECT completed_at FROM "+item.table+" WHERE task_id=?", item.taskID).
				Scan(&completedAt); err != nil {
				t.Fatal(err)
			}
			if !completedAt.Valid || completedAt.Int64 != 123456 {
				t.Fatalf("completed_at=%v want 123456", completedAt)
			}
		})
	}
}
