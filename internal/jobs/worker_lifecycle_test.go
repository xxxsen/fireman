package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func TestWorkerLoopExitsOnContextCancel(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_exit','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, nil, repository.Job{
		ID: "job_exit", PlanID: "plan_exit", Type: repository.JobTypeSimulation,
		Status: repository.JobStatusQueued, InputHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO simulation_runs (id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count, summary_json, created_at)
		VALUES ('run_exit','job_exit','plan_exit','h','{}','m','v1',100,1,120,0,0,'{}',0)`)
	if err != nil {
		t.Fatal(err)
	}

	w := NewWorker(db, repo, repository.NewSimulationRepo(db), blockingRunner{block: 30 * time.Second}, nil, NewEventHub(), nil, nil)
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		w.Start(runCtx, 1)
		close(done)
	}()

	deadline := time.Now().Add(3 * time.Second)
	claimed := false
	for time.Now().Before(deadline) {
		job, err := repo.GetByID(ctx, "job_exit")
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == repository.JobStatusRunning {
			claimed = true
			cancel()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !claimed {
		t.Fatal("job was not claimed before cancel")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker loop did not exit after context cancel")
	}
}

func TestWorkerHeartbeatStopsAfterJobCompletes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	sims := repository.NewSimulationRepo(db)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_done','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, nil, repository.Job{
		ID: "job_done", PlanID: "plan_done", Type: repository.JobTypeSimulation,
		Status: repository.JobStatusQueued, InputHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	snapJSON, err := json.Marshal(testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO simulation_runs (id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count, summary_json, created_at)
		VALUES ('run_done','job_done','plan_done','h',?,'m','v1',10,42,120,0,0,'{}',0)`, string(snapJSON))
	if err != nil {
		t.Fatal(err)
	}

	w := NewWorker(db, repo, sims, NewSimulationRunner(db, sims), nil, NewEventHub(), nil, nil)
	w.heartbeatInterval = 50 * time.Millisecond
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(runCtx, 1)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByID(ctx, "job_done")
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == repository.JobStatusSucceeded {
			var hbAfterDone int64
			if job.HeartbeatAt != nil {
				hbAfterDone = *job.HeartbeatAt
			}
			time.Sleep(250 * time.Millisecond)
			job, err = repo.GetByID(ctx, "job_done")
			if err != nil {
				t.Fatal(err)
			}
			if job.HeartbeatAt != nil && *job.HeartbeatAt > hbAfterDone {
				t.Fatalf("heartbeat updated after job finished: before=%d after=%d", hbAfterDone, *job.HeartbeatAt)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("job did not succeed in time")
}

func TestWorkerSingleHeartbeatTickerDuringLongTask(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES ('plan_hb3','test','CNY','2026-06-09','active',1,0,0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, nil, repository.Job{
		ID: "job_hb3", PlanID: "plan_hb3", Type: repository.JobTypeSimulation,
		Status: repository.JobStatusQueued, InputHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO simulation_runs (id, job_id, plan_id, input_hash, input_snapshot_json, market_snapshot_hash,
			engine_version, runs, seed, horizon_months, success_count, failure_count, summary_json, created_at)
		VALUES ('run_hb3','job_hb3','plan_hb3','h','{}','m','v1',100,1,120,0,0,'{}',0)`)
	if err != nil {
		t.Fatal(err)
	}

	w := NewWorker(db, repo, repository.NewSimulationRepo(db), blockingRunner{block: 2 * time.Second}, nil, NewEventHub(), nil, nil)
	w.heartbeatInterval = 50 * time.Millisecond
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Start(runCtx, 1)

	var firstHB int64
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByID(ctx, "job_hb3")
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == repository.JobStatusRunning && job.HeartbeatAt != nil {
			firstHB = *job.HeartbeatAt
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if firstHB == 0 {
		t.Fatal("job was not claimed with heartbeat")
	}

	updates := 0
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByID(ctx, "job_hb3")
		if err != nil {
			t.Fatal(err)
		}
		if job.HeartbeatAt != nil && *job.HeartbeatAt > firstHB+100 {
			updates++
			firstHB = *job.HeartbeatAt
		}
		time.Sleep(60 * time.Millisecond)
	}
	if updates < 1 {
		t.Fatal("expected multiple heartbeat updates during long task")
	}
}
