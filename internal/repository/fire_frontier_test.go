package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/testutil"
)

func TestFireFrontierAtomicCreationAndRetention(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := NewFireFrontierRepo(db)
	if _, err := db.ExecContext(ctx, `INSERT INTO plans
		(id,name,valuation_date,created_at,updated_at) VALUES ('plan_frontier','plan','2026-07-13',1,1)`); err != nil {
		t.Fatal(err)
	}

	// A business-row constraint failure rolls the task back with the outer
	// transaction, proving task/run creation cannot leave an orphan.
	err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := insertFrontierTask(ctx, tx, "task_bad", 1, WorkerTaskStatusPending); err != nil {
			return err
		}
		return repo.CreateTx(ctx, tx, &FireFrontierRun{
			ID: "run_bad", TaskID: "task_bad", PlanID: "plan_frontier", SourceSimulationRunID: "sim",
			InputHash: "hash", AlgorithmVersion: "v1", FrontierType: "not_valid",
			SourceEngineVersion: "3.5.0", SourceConfigHash: "config", SourceMarketHash: "market",
			EvaluationRuns: 1000, ConfigJSON: `{}`, InputSnapshotJSON: `{}`,
		})
	})
	if err == nil {
		t.Fatal("expected invalid frontier type to fail")
	}
	var badTasks int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM worker_tasks WHERE id='task_bad'`).Scan(&badTasks); err != nil {
		t.Fatal(err)
	}
	if badTasks != 0 {
		t.Fatal("failed atomic create left an orphan task")
	}

	for i := 0; i < 24; i++ {
		status := []string{WorkerTaskStatusComplete, WorkerTaskStatusFailed, WorkerTaskStatusCanceled}[i%3]
		if i == 23 {
			status = WorkerTaskStatusRunning
		}
		taskID, runID := fmt.Sprintf("task_%02d", i), fmt.Sprintf("run_%02d", i)
		err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
			if err := insertFrontierTask(ctx, tx, taskID, int64(i+2), status); err != nil {
				return err
			}
			completed := int64(i + 100)
			run := &FireFrontierRun{
				ID: runID, TaskID: taskID, PlanID: "plan_frontier", SourceSimulationRunID: "sim_deleted",
				InputHash: runID, AlgorithmVersion: "fire_frontier_v1", FrontierType: "required_current_assets",
				SourceEngineVersion: "3.5.0", SourceConfigHash: "config", SourceMarketHash: "market",
				EvaluationRuns: 1000, ConfigJSON: `{}`, InputSnapshotJSON: `{}`, CreatedAt: int64(i + 1),
			}
			if status != WorkerTaskStatusRunning {
				run.CompletedAt = &completed
			}
			return repo.CreateTx(ctx, tx, run)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// The oldest terminal run is applied and therefore permanently retained.
	if err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		return repo.CreateApplicationTx(ctx, tx, FireFrontierApplication{
			ID: "app_00", FrontierRunID: "run_00", PointID: "point", PlanID: "plan_frontier",
			BeforeConfigVersion: 1, AfterConfigVersion: 2, PreviewHash: "preview",
			BeforeJSON: `{}`, AfterJSON: `{}`, AppliedAt: 1,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		return repo.PruneTx(ctx, tx, "plan_frontier", 20)
	}); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fire_frontier_runs WHERE plan_id='plan_frontier'`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	// 20 terminal unapplied + one applied + one active.
	if total != 22 {
		t.Fatalf("retained runs=%d want 22", total)
	}
	for _, id := range []string{"run_00", "run_23"} {
		if _, err := repo.GetByID(ctx, id); err != nil {
			t.Fatalf("protected run %s was pruned: %v", id, err)
		}
	}
}

func TestFireFrontierCompletionRollbackAndReusableStates(t *testing.T) {
	for _, status := range []string{
		WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete,
		WorkerTaskStatusComplete, WorkerTaskStatusFailed, WorkerTaskStatusCanceled,
	} {
		t.Run(status, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			ctx := context.Background()
			repo := NewFireFrontierRepo(db)
			if _, err := db.ExecContext(ctx, `INSERT INTO plans
				(id,name,valuation_date,created_at,updated_at) VALUES ('plan_reuse','plan','2026-07-13',1,1)`); err != nil {
				t.Fatal(err)
			}
			err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
				if err := insertFrontierTask(ctx, tx, "task_reuse", 1, status); err != nil {
					return err
				}
				completed := int64(2)
				run := &FireFrontierRun{
					ID: "run_reuse", TaskID: "task_reuse", PlanID: "plan_reuse",
					SourceSimulationRunID: "sim", InputHash: "same-input", AlgorithmVersion: "v1",
					FrontierType: "required_current_assets", SourceEngineVersion: "3.5.0",
					SourceConfigHash: "config", SourceMarketHash: "market", EvaluationRuns: 1000,
					ConfigJSON: `{}`, InputSnapshotJSON: `{}`,
				}
				if IsTerminalWorkerTaskStatus(status) {
					run.CompletedAt = &completed
				}
				return repo.CreateTx(ctx, tx, run)
			})
			if err != nil {
				t.Fatal(err)
			}
			found, findErr := repo.FindReusable(ctx, "plan_reuse", "same-input")
			if status == WorkerTaskStatusFailed || status == WorkerTaskStatusCanceled {
				if !errors.Is(findErr, ErrFireFrontierNotFound) {
					t.Fatalf("terminal failure/cancel was reused: %#v err=%v", found, findErr)
				}
			} else if findErr != nil || found.ID != "run_reuse" {
				t.Fatalf("status %s not reusable: %#v err=%v", status, found, findErr)
			}

			if status == WorkerTaskStatusPending {
				sentinel := errors.New("injected task completion failure")
				err = fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
					if err := repo.CompleteTx(ctx, tx, "task_reuse", json.RawMessage(`{"points":[]}`), 99); err != nil {
						return err
					}
					return sentinel
				})
				if !errors.Is(err, sentinel) {
					t.Fatalf("completion injection error=%v", err)
				}
				run, getErr := repo.GetByID(ctx, "run_reuse")
				if getErr != nil || run.CompletedAt != nil || string(run.ResultJSON) != `{}` {
					t.Fatalf("rolled-back completion left partial result: %#v err=%v", run, getErr)
				}
			}
		})
	}
}

func insertFrontierTask(ctx context.Context, tx *sql.Tx, id string, version int64, status string) error {
	finished := any(nil)
	if status == WorkerTaskStatusComplete || status == WorkerTaskStatusFailed || status == WorkerTaskStatusCanceled {
		finished = version + 100
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO worker_tasks (
		id,version_no,worker_type,type,status,payload_json,available_at,created_at,finished_at,updated_at
	) VALUES (?,?,?,?,?,'{}',?,?,?,?)`, id, version, WorkerTypeGo, WorkerTaskTypeFireFrontier,
		status, version, version, finished, version)
	return err
}
