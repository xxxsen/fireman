package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func TestWorkerStartupImmediatelyRequeuesFreshOrphan(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO jobs
		(id,type,status,input_hash,payload_json,progress_current,progress_total,phase,
		 cancel_requested,retry_count,heartbeat_at,created_at,started_at)
		VALUES ('fresh_orphan','simulation','running','h','{}',25,100,'simulating',0,0,?,0,0)`, now); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db, repo, repository.NewSimulationRepo(db), nil, nil, nil, NewEventHub(), nil, func() bool { return true })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Start(ctx, 1); close(done) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByID(context.Background(), "fresh_orphan")
		if err == nil && job.Status == repository.JobStatusQueued {
			if job.RetryCount != 1 || job.ProgressCurrent != 0 {
				t.Fatalf("unexpected requeue: %+v", job)
			}
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("fresh orphan was not requeued during startup")
}

func TestWorkerPeriodicReconcilesLaterStaleJob(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	startupDone := make(chan struct{})
	var signaled bool
	w := NewWorker(db, repo, repository.NewSimulationRepo(db), nil, nil, nil, NewEventHub(), nil, func() bool {
		if !signaled {
			signaled = true
			close(startupDone)
		}
		return true
	})
	w.interval = 5 * time.Millisecond
	w.reconcileInterval = 20 * time.Millisecond
	w.staleThreshold = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Start(ctx, 1); close(done) }()
	select {
	case <-startupDone:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("worker did not finish startup")
	}

	if _, err := db.Exec(`INSERT INTO jobs
		(id,type,status,input_hash,payload_json,progress_current,progress_total,phase,
		 cancel_requested,retry_count,heartbeat_at,created_at,started_at)
		VALUES ('periodic_orphan','simulation','running','h','{}',25,100,'simulating',0,0,?,0,0)`,
		time.Now().Add(-time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByID(context.Background(), "periodic_orphan")
		if err == nil && job.Status == repository.JobStatusQueued {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("periodic reconciler did not requeue stale job")
}

func TestWorkerStartupFailsRetryExhaustedOrphan(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	if _, err := db.Exec(`INSERT INTO jobs
		(id,type,status,input_hash,payload_json,progress_current,progress_total,phase,
		 cancel_requested,retry_count,heartbeat_at,created_at,started_at)
		VALUES ('exhausted_orphan','simulation','running','h','{}',25,100,'simulating',0,1,?,0,0)`,
		time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(db, repo, repository.NewSimulationRepo(db), nil, nil, nil, NewEventHub(), nil, func() bool { return true })
	w.reconcileOrphanedJobs(context.Background(), true)
	job, err := repo.GetByID(context.Background(), "exhausted_orphan")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != repository.JobStatusFailed || job.ErrorCode != repository.JobErrorWorkerInterrupted {
		t.Fatalf("unexpected exhausted job: %+v", job)
	}
}
