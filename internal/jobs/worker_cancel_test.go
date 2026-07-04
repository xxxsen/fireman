package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

type cancelProbeRunner struct {
	probe chan func() bool
}

func (r cancelProbeRunner) RunSimulation(
	ctx context.Context,
	_, _ string,
	_ *simulation.InputSnapshot,
	cancelCheck func() bool,
	_ func(done, total int, phase string),
) error {
	r.probe <- cancelCheck
	<-ctx.Done()
	if cancelCheck() {
		return context.Canceled
	}
	return ctx.Err()
}

func TestCancelRequestedDuringWorkerShutdown(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_cancel','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, nil, repository.Job{
		ID: "job_cancel_run", PlanID: "plan_cancel", Type: repository.JobTypeSimulation,
		Status: repository.JobStatusQueued, InputHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO simulation_runs (id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count, summary_json, created_at)
		VALUES ('run_cancel','job_cancel_run','plan_cancel','h','{}','m','v1',100,1,120,0,0,'{}',0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RequestCancel(ctx, "job_cancel_run"); err != nil {
		t.Fatal(err)
	}

	probe := make(chan func() bool, 1)
	w := NewWorker(db, repo, repository.NewSimulationRepo(db), cancelProbeRunner{probe: probe}, nil, NewEventHub(),
		nil, nil)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Start(workerCtx, 1)
		close(done)
	}()

	select {
	case <-probe:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not start")
	}

	workerCancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit after shutdown")
	}

	job, err := repo.GetByID(ctx, "job_cancel_run")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != repository.JobStatusCanceled {
		t.Fatalf("expected canceled, got status=%s (job must not be requeued)", job.Status)
	}
}
