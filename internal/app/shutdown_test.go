package app

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

type shutdownBlockingRunner struct {
	block time.Duration
}

func (b shutdownBlockingRunner) RunSimulation(ctx context.Context, _, _ string, _ *simulation.InputSnapshot,
	_ func() bool, _ func(done, total int, phase string),
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(b.block):
		return nil
	}
}

func TestShutdownWaitsForWorkerBeforeDBClose(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_shutdown','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, nil, repository.Job{
		ID: "job_shutdown", PlanID: "plan_shutdown", Type: repository.JobTypeSimulation,
		Status: repository.JobStatusQueued, InputHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO simulation_runs (id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count, summary_json, created_at)
		VALUES ('run_shutdown','job_shutdown','plan_shutdown','h','{}','m','v1',100,1,120,0,0,'{}',0)`)
	if err != nil {
		t.Fatal(err)
	}

	w := jobs.NewWorker(db, repo, repository.NewSimulationRepo(db), shutdownBlockingRunner{block: 35 * time.Second}, nil,
		jobs.NewEventHub(), nil, nil)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		w.Start(workerCtx, 1)
		close(workerDone)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByID(ctx, "job_shutdown")
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == repository.JobStatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	events := []string{"http_shutdown"}
	workerCancel()
	select {
	case <-workerDone:
		events = append(events, "worker_done")
	case <-time.After(40 * time.Second):
		t.Fatal("worker did not stop before db close")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	events = append(events, "db_close")

	want := []string{"http_shutdown", "worker_done", "db_close"}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("event[%d]=%s want %s (all=%v)", i, events[i], want[i], events)
		}
	}
}
