package repository

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/testutil"
)

func seedOptimizationJob(t *testing.T, retryCount int, cancel bool, heartbeat int64) (*JobRepo, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO research_collections
		(id,name,created_at,updated_at) VALUES ('rc_reconcile','reconcile',0,0)`); err != nil {
		t.Fatal(err)
	}
	jobID, runID := "job_reconcile", "ror_reconcile"
	if _, err := db.ExecContext(ctx, `INSERT INTO jobs
		(id,type,status,input_hash,payload_json,progress_current,progress_total,phase,
		 cancel_requested,retry_count,heartbeat_at,created_at,started_at)
		VALUES (?,?,?,?,?,42,100,'evaluating',?,?,?,0,0)`,
		jobID, JobTypeResearchOptimization, JobStatusRunning, "hash", "{}",
		boolToInt(cancel), retryCount, heartbeat); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO research_optimization_runs
		(id,collection_id,job_id,status,input_hash,source_hash,engine_version,
		 base_currency,rebalance_policy,window_start,window_end,config_json,
		 input_snapshot_json,candidate_count,evaluated_count,result_json,
		 error_code,error_message,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,100,42,'{"partial":true}','','',0)`,
		runID, "rc_reconcile", jobID, ResearchRunStatusRunning, "hash", "source", "v1",
		"CNY", "monthly", "2020-01-01", "2024-01-01", "{}", "{}"); err != nil {
		t.Fatal(err)
	}
	return NewJobRepo(db), jobID, runID
}

func seedBacktestJob(t *testing.T, retryCount int, heartbeat int64) (*JobRepo, string, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO research_collections
		(id,name,created_at,updated_at) VALUES ('rc_backtest_reconcile','reconcile',0,0)`); err != nil {
		t.Fatal(err)
	}
	jobID, runID := "job_backtest_reconcile", "rbr_reconcile"
	if _, err := db.ExecContext(ctx, `INSERT INTO jobs
		(id,type,status,input_hash,payload_json,progress_current,progress_total,phase,
		 cancel_requested,retry_count,heartbeat_at,created_at,started_at)
		VALUES (?,?,?,?,?,42,100,'backtesting',0,?,?,0,0)`,
		jobID, JobTypeResearchBacktest, JobStatusRunning, "hash", "{}",
		retryCount, heartbeat); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO research_backtest_runs
		(id,collection_id,job_id,input_hash,input_snapshot_json,source_hash,
		 engine_version,base_currency,rebalance_policy,window_start,window_end,
		 status,summary_json,data_quality_json,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,0)`, runID, "rc_backtest_reconcile", jobID,
		"hash", "{}", "source", "v1", "CNY", "monthly", "2020-01-01", "2024-01-01",
		ResearchRunStatusRunning, `{"partial":true}`, `{"partial":true}`); err != nil {
		t.Fatal(err)
	}
	return NewJobRepo(db), jobID, runID
}

func TestConvergeInterruptedRequeuesOptimizationAtomically(t *testing.T) {
	now := time.Now().UnixMilli()
	repo, jobID, runID := seedOptimizationJob(t, 0, false, now)
	ctx := context.Background()
	candidates, err := repo.ListRunningForReconcile(ctx, nil)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("startup candidates=%d err=%v", len(candidates), err)
	}
	result, changed, err := repo.ConvergeInterrupted(ctx, jobID, nil, 1, now)
	if err != nil || !changed || result.Action != InterruptedActionRequeued {
		t.Fatalf("result=%+v changed=%v err=%v", result, changed, err)
	}
	job, err := repo.GetByID(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusQueued || job.RetryCount != 1 || job.ProgressCurrent != 0 || job.Phase != "retrying" {
		t.Fatalf("unexpected requeued job: %+v", job)
	}
	var status, resultJSON, code string
	var evaluated int
	var completed any
	if err := repo.db.QueryRowContext(ctx, `SELECT status,evaluated_count,result_json,error_code,completed_at
		FROM research_optimization_runs WHERE id=?`, runID).
		Scan(&status, &evaluated, &resultJSON, &code, &completed); err != nil {
		t.Fatal(err)
	}
	if status != ResearchRunStatusQueued || evaluated != 0 || resultJSON != "{}" || code != "" || completed != nil {
		t.Fatalf("run status=%s evaluated=%d result=%s code=%s completed=%v",
			status, evaluated, resultJSON, code, completed)
	}
}

func TestConvergeInterruptedFailsAfterRetryExhausted(t *testing.T) {
	now := time.Now().UnixMilli()
	repo, jobID, runID := seedOptimizationJob(t, 1, false, now)
	result, changed, err := repo.ConvergeInterrupted(context.Background(), jobID, nil, 1, now)
	if err != nil || !changed || result.Action != InterruptedActionFailed {
		t.Fatalf("result=%+v changed=%v err=%v", result, changed, err)
	}
	job, _ := repo.GetByID(context.Background(), jobID)
	if job.Status != JobStatusFailed || job.ErrorCode != JobErrorWorkerInterrupted || job.FinishedAt == nil {
		t.Fatalf("unexpected failed job: %+v", job)
	}
	var status, code string
	var completed int64
	if err := repo.db.QueryRow(`SELECT status,error_code,completed_at
		FROM research_optimization_runs WHERE id=?`, runID).Scan(&status, &code, &completed); err != nil {
		t.Fatal(err)
	}
	if status != ResearchRunStatusFailed || code != JobErrorWorkerInterrupted || completed != now {
		t.Fatalf("run status=%s code=%s completed=%d", status, code, completed)
	}
}

func TestConvergeInterruptedRequeuesBacktestAtomically(t *testing.T) {
	now := time.Now().UnixMilli()
	repo, jobID, runID := seedBacktestJob(t, 0, now)
	result, changed, err := repo.ConvergeInterrupted(context.Background(), jobID, nil, 1, now)
	if err != nil || !changed || result.Action != InterruptedActionRequeued {
		t.Fatalf("result=%+v changed=%v err=%v", result, changed, err)
	}
	var status, summary, quality string
	var completed any
	if err := repo.db.QueryRow(`SELECT status,summary_json,data_quality_json,completed_at
		FROM research_backtest_runs WHERE id=?`, runID).Scan(&status, &summary, &quality, &completed); err != nil {
		t.Fatal(err)
	}
	if status != ResearchRunStatusQueued || summary != "{}" || quality != "{}" || completed != nil {
		t.Fatalf("run status=%s summary=%s quality=%s completed=%v", status, summary, quality, completed)
	}
}

func TestFinishFailureConvergesBacktestAtomically(t *testing.T) {
	now := time.Now().UnixMilli()
	repo, jobID, runID := seedBacktestJob(t, 0, now)
	changed, err := repo.Finish(context.Background(), jobID, JobStatusFailed, "compute_failed", "boom")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	job, err := repo.GetByID(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusFailed || job.ErrorCode != "compute_failed" {
		t.Fatalf("job=%+v", job)
	}
	var status string
	var completed int64
	if err := repo.db.QueryRow(`SELECT status,completed_at FROM research_backtest_runs WHERE id=?`, runID).
		Scan(&status, &completed); err != nil {
		t.Fatal(err)
	}
	if status != ResearchRunStatusFailed || completed == 0 {
		t.Fatalf("run status=%s completed=%d", status, completed)
	}
}

func TestPeriodicConvergenceRechecksFreshHeartbeat(t *testing.T) {
	now := time.Now().UnixMilli()
	repo, jobID, _ := seedOptimizationJob(t, 0, false, now)
	staleBefore := now - int64(time.Minute/time.Millisecond)
	candidates, err := repo.ListRunningForReconcile(context.Background(), &staleBefore)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("fresh candidates=%d err=%v", len(candidates), err)
	}
	_, changed, err := repo.ConvergeInterrupted(context.Background(), jobID, &staleBefore, 1, now)
	if err != nil || changed {
		t.Fatalf("fresh job changed=%v err=%v", changed, err)
	}
}

func TestInterruptedCancelTakesPriorityOverRetry(t *testing.T) {
	now := time.Now().UnixMilli()
	repo, jobID, runID := seedOptimizationJob(t, 0, true, now-120_000)
	result, changed, err := repo.ConvergeInterrupted(context.Background(), jobID, nil, 1, now)
	if err != nil || !changed || result.Action != InterruptedActionCanceled {
		t.Fatalf("result=%+v changed=%v err=%v", result, changed, err)
	}
	job, _ := repo.GetByID(context.Background(), jobID)
	if job.Status != JobStatusCanceled {
		t.Fatalf("job status=%s", job.Status)
	}
	var status string
	if err := repo.db.QueryRow(`SELECT status FROM research_optimization_runs WHERE id=?`, runID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != ResearchRunStatusCanceled {
		t.Fatalf("run status=%s", status)
	}
}

func TestInterruptedDependentFailureRollsBackJob(t *testing.T) {
	now := time.Now().UnixMilli()
	repo, jobID, _ := seedOptimizationJob(t, 0, false, now)
	if _, err := repo.db.Exec(`CREATE TRIGGER reject_reconcile_run
		BEFORE UPDATE ON research_optimization_runs
		WHEN NEW.status='queued'
		BEGIN SELECT RAISE(ABORT, 'reject dependent update'); END`); err != nil {
		t.Fatal(err)
	}
	_, changed, err := repo.ConvergeInterrupted(context.Background(), jobID, nil, 1, now)
	if err == nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	job, getErr := repo.GetByID(context.Background(), jobID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if job.Status != JobStatusRunning || job.RetryCount != 0 || job.ProgressCurrent != 42 {
		t.Fatalf("job was partially committed: %+v", job)
	}
}
