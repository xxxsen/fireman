package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

type blockingRunner struct {
	block time.Duration
}

func (b blockingRunner) RunSimulation(ctx context.Context, _, _ string, _ *simulation.InputSnapshot,
	_ func() bool, _ func(done, total int, phase string),
) error {
	select {
	case <-ctx.Done():
		return context.Canceled
	case <-time.After(b.block):
		return nil
	}
}

func TestWorkerHeartbeatDuringLongTask(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_hb','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, nil, repository.Job{
		ID: "job_hb", PlanID: "plan_hb", Type: repository.JobTypeSimulation,
		Status: repository.JobStatusQueued, InputHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO simulation_runs (id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count, summary_json, created_at)
		VALUES ('run_hb','job_hb','plan_hb','h','{}','m','v1',100,1,120,0,0,'{}',0)`)
	if err != nil {
		t.Fatal(err)
	}

	w := NewWorker(db, repo, repository.NewSimulationRepo(db), blockingRunner{block: 25 * time.Second}, nil, nil,
		NewEventHub(), nil, nil)
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(runCtx, 1)

	var firstHB int64
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByID(ctx, "job_hb")
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == repository.JobStatusRunning {
			if job.HeartbeatAt != nil {
				firstHB = *job.HeartbeatAt
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if firstHB == 0 {
		t.Fatal("job was not claimed")
	}

	deadline = time.Now().Add(22 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByID(ctx, "job_hb")
		if err != nil {
			t.Fatal(err)
		}
		if job.HeartbeatAt != nil && *job.HeartbeatAt > firstHB+1000 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("heartbeat_at was not updated during long task")
}

func TestStaleReconcileSkipsActiveHeartbeat(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_hb2','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO jobs (id, plan_id, type, status, input_hash, progress_current, progress_total,
			phase, cancel_requested, retry_count, heartbeat_at, created_at, started_at)
		VALUES ('job_live','plan_hb2','simulation','running','h',0,100,'sim',0,0,?, ?, ?)`,
		now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	n, err := repo.RequeueStaleRunning(ctx, time.Now().Add(-10*time.Minute).UnixMilli(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 requeued with fresh heartbeat, got %d", n)
	}
}
