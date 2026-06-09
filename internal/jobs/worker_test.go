package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func TestJobClaimSingleWorker(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_x','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.Create(ctx, nil, repository.Job{
		ID: "job_a", PlanID: "plan_x", Type: repository.JobTypeSimulation,
		Status: repository.JobStatusQueued, InputHash: "hash",
	}); err != nil {
		t.Fatal(err)
	}

	j1, err := repo.ClaimNextQueued(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if j1.Status != repository.JobStatusRunning {
		t.Fatalf("expected running, got %s", j1.Status)
	}
	_, err = repo.ClaimNextQueued(ctx)
	if err == nil {
		t.Fatal("expected no queued job")
	}
}

func TestStaleJobRequeue(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_x','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-20 * time.Minute).UnixMilli()
	_, err = db.ExecContext(ctx, `
		INSERT INTO jobs (id, plan_id, type, status, input_hash, progress_current, progress_total,
			phase, cancel_requested, retry_count, heartbeat_at, created_at, started_at)
		VALUES ('job_stale','plan_x','simulation','running','h',0,100,'sim',0,0,?, ?, ?)`,
		stale, stale, stale)
	if err != nil {
		t.Fatal(err)
	}
	n, err := repo.RequeueStaleRunning(ctx, time.Now().Add(-10*time.Minute).UnixMilli(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 requeued, got %d", n)
	}
	job, err := repo.GetByID(ctx, "job_stale")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != repository.JobStatusQueued || job.RetryCount != 1 {
		t.Fatalf("unexpected job after requeue: %+v", job)
	}
}

func TestCancelQueuedJobImmediately(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_x','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, nil, repository.Job{
		ID: "job_cancel", PlanID: "plan_x", Type: repository.JobTypeSimulation,
		Status: repository.JobStatusQueued, InputHash: "hash",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CancelQueued(ctx, "job_cancel"); err != nil {
		t.Fatal(err)
	}
	job, err := repo.GetByID(ctx, "job_cancel")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != repository.JobStatusCanceled {
		t.Fatalf("expected canceled, got %s", job.Status)
	}
	if job.FinishedAt == nil {
		t.Fatal("expected finished_at to be set")
	}
}
