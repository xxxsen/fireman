package worker

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/frontier"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	taskcore "github.com/fireman/fireman/internal/task"
	"github.com/fireman/fireman/internal/testutil"
)

func TestFrontierDeterministicFailureIsTerminalWithNoPartialResult(t *testing.T) {
	db := testutil.OpenTestDB(t)
	coordinator := taskcore.NewCoordinator(db, repository.NewWorkerTaskRepo(db),
		taskcore.DefaultRegistry(), taskcore.NewEventHub())
	createFrontierProcessorFixture(t, db, coordinator, "task_frontier_invalid", "ffr_invalid")
	processors := NewProcessorSet(db, coordinator, nil, nil)
	supervisor := NewSupervisor(coordinator, processors,
		slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	supervisor.workerID = "go_worker:frontier-invalid"
	if !supervisor.tryExecute(context.Background(), "task_frontier_invalid") {
		t.Fatal("frontier task was not executed")
	}
	assertFailedFrontierMetadata(t, db, coordinator, "task_frontier_invalid", 1,
		"frontier_result_inconsistent")
}

func TestFrontierTransientFailureRetriesOnceThenClosesRun(t *testing.T) {
	db := testutil.OpenTestDB(t)
	coordinator := taskcore.NewCoordinator(db, repository.NewWorkerTaskRepo(db),
		taskcore.DefaultRegistry(), taskcore.NewEventHub())
	createFrontierProcessorFixture(t, db, coordinator, "task_frontier_retry", "ffr_retry")
	processors := &ProcessorSet{
		db: db, frontiers: repository.NewFireFrontierRepo(db),
		processors: map[string]func(context.Context, repository.WorkerTask, Attempt) error{
			repository.WorkerTaskTypeFireFrontier: func(context.Context, repository.WorkerTask, Attempt) error {
				return errors.New("database is locked")
			},
		},
	}
	supervisor := NewSupervisor(coordinator, processors,
		slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	supervisor.workerID = "go_worker:frontier-retry"
	if !supervisor.tryExecute(context.Background(), "task_frontier_retry") {
		t.Fatal("first frontier attempt was not executed")
	}
	afterFirst, err := coordinator.Get(context.Background(), "task_frontier_retry")
	if err != nil || afterFirst.Status != repository.WorkerTaskStatusPending || afterFirst.AttemptCount != 1 {
		t.Fatalf("task after first retryable failure=%#v err=%v", afterFirst, err)
	}
	var completed sql.NullInt64
	if err := db.QueryRow(`SELECT completed_at FROM fire_frontier_runs WHERE task_id=?`, afterFirst.ID).
		Scan(&completed); err != nil || completed.Valid {
		t.Fatalf("retryable failure closed run early: completed=%v err=%v", completed, err)
	}
	if _, err := db.Exec(`UPDATE worker_tasks SET available_at=0 WHERE id=?`, afterFirst.ID); err != nil {
		t.Fatal(err)
	}
	if !supervisor.tryExecute(context.Background(), "task_frontier_retry") {
		t.Fatal("second frontier attempt was not executed")
	}
	assertFailedFrontierMetadata(t, db, coordinator, "task_frontier_retry", 2, taskcore.ErrRetryExhausted)
}

func createFrontierProcessorFixture(t *testing.T, db *sql.DB, coordinator *taskcore.Coordinator,
	taskID, runID string,
) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO plans
		(id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, "plan_"+runID, "frontier", "CNY", "2026-07-13", "active", 1, 1, 1); err != nil {
		t.Fatal(err)
	}
	err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := coordinator.CreateTx(ctx, tx, &repository.WorkerTask{
			ID: taskID, WorkerType: repository.WorkerTypeGo, Type: repository.WorkerTaskTypeFireFrontier,
			Status: repository.WorkerTaskStatusPending, ScopeType: "plan", ScopeID: "plan_" + runID,
			DedupeKey: taskID, PayloadJSON: `{"run_id":"` + runID + `"}`,
		}); err != nil {
			return err
		}
		return repository.NewFireFrontierRepo(db).CreateTx(ctx, tx, &repository.FireFrontierRun{
			ID: runID, TaskID: taskID, PlanID: "plan_" + runID,
			SourceSimulationRunID: "sim_source", InputHash: "sha256:input",
			AlgorithmVersion:    frontier.AlgorithmVersion,
			FrontierType:        frontier.TypeRetirementAgeMaxSpending,
			SourceEngineVersion: simulation.EngineVersion, SourceConfigHash: "sha256:config",
			SourceMarketHash: "sha256:market", EvaluationRuns: 1000,
			ConfigJSON: `{}`, InputSnapshotJSON: `{}`,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertFailedFrontierMetadata(t *testing.T, db *sql.DB, coordinator *taskcore.Coordinator,
	taskID string, attempts int, code string,
) {
	t.Helper()
	task, err := coordinator.Get(context.Background(), taskID)
	if err != nil || task.Status != repository.WorkerTaskStatusFailed ||
		task.AttemptCount != attempts || task.ErrorCode != code {
		t.Fatalf("failed frontier task=%#v err=%v", task, err)
	}
	var completed sql.NullInt64
	var result string
	if err := db.QueryRow(`SELECT completed_at,result_json FROM fire_frontier_runs WHERE task_id=?`, taskID).
		Scan(&completed, &result); err != nil {
		t.Fatal(err)
	}
	if !completed.Valid || result != `{}` {
		t.Fatalf("failed frontier metadata completed=%v result=%s", completed, result)
	}
}
