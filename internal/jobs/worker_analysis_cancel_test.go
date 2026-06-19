package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

// barrierAnalysisRunner reproduces the supersede race window: its last
// cancelCheck passes (no cancel yet), then it blocks just before returning a
// successful result. The test injects a supersede cancel while it is blocked, so
// the cancel lands after the final cancelCheck but before the worker writes the
// terminal state.
type barrierAnalysisRunner struct {
	reached chan struct{}
	release chan struct{}
}

func (r *barrierAnalysisRunner) run(cancelCheck func() bool) error {
	if cancelCheck != nil && cancelCheck() {
		return context.Canceled
	}
	close(r.reached)
	<-r.release
	return nil
}

func (r *barrierAnalysisRunner) RunStress(
	_ context.Context, _ string, cancelCheck func() bool, _ func(done, total int, phase string),
) error {
	return r.run(cancelCheck)
}

func (r *barrierAnalysisRunner) RunSensitivity(
	_ context.Context, _ string, cancelCheck func() bool, _ func(done, total int, phase string),
) error {
	return r.run(cancelCheck)
}

// TestAnalysisSupersededRunningJobConvergesToCanceled guards td/054 finding #1:
// when a running stress/sensitivity job is superseded after its final cancelCheck
// but before the terminal write, the worker must converge it to canceled with the
// supersede error code, never to succeeded.
func TestAnalysisSupersededRunningJobConvergesToCanceled(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()

	// The fake runner bypasses the analysis record, so only the job row is needed
	// to drive executeAnalysisJob's terminal convergence.
	jobID := "job_stress_race"
	if err := repo.Create(ctx, nil, repository.Job{
		ID: jobID, Type: repository.JobTypeStress, Status: repository.JobStatusQueued,
		InputHash: "ih", ProgressTotal: 8,
	}); err != nil {
		t.Fatal(err)
	}

	runner := &barrierAnalysisRunner{reached: make(chan struct{}), release: make(chan struct{})}
	w := NewWorker(db, repo, repository.NewSimulationRepo(db), nil, runner, nil, NewEventHub(), nil, nil)
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()
	go w.Start(workerCtx, 1)

	select {
	case <-runner.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach the analysis barrier")
	}

	// Supersede committing in the race window: flag the running job for cancel
	// with the supersede code, then drop its analysis record (as the service does).
	updated, err := repo.RequestCancelRunningWithErrorTx(
		ctx, nil, jobID, repository.JobErrSupersededByNewerAnalysis, "superseded by a newer analysis run")
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("expected the running job to be flagged for cancellation")
	}

	close(runner.release)

	deadline := time.Now().Add(5 * time.Second)
	for {
		job, err := repo.GetByID(ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == repository.JobStatusRunning || job.Status == repository.JobStatusQueued {
			if time.Now().After(deadline) {
				t.Fatalf("job did not reach a terminal state, status=%s", job.Status)
			}
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if job.Status != repository.JobStatusCanceled {
			t.Fatalf("superseded running job must end canceled, got %s", job.Status)
		}
		if job.ErrorCode != repository.JobErrSupersededByNewerAnalysis {
			t.Fatalf("canceled job error_code=%q want %q", job.ErrorCode, repository.JobErrSupersededByNewerAnalysis)
		}
		break
	}
}

// TestAnalysisRunningJobSucceedsWithoutSupersede ensures the cancel-aware
// convergence still completes a normal run as succeeded when no cancel is pending.
func TestAnalysisRunningJobSucceedsWithoutSupersede(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewJobRepo(db)
	ctx := context.Background()

	jobID := "job_stress_ok"
	if err := repo.Create(ctx, nil, repository.Job{
		ID: jobID, Type: repository.JobTypeStress, Status: repository.JobStatusQueued,
		InputHash: "ih2", ProgressTotal: 8,
	}); err != nil {
		t.Fatal(err)
	}

	runner := &barrierAnalysisRunner{reached: make(chan struct{}), release: make(chan struct{})}
	w := NewWorker(db, repo, repository.NewSimulationRepo(db), nil, runner, nil, NewEventHub(), nil, nil)
	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()
	go w.Start(workerCtx, 1)

	select {
	case <-runner.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach the analysis barrier")
	}
	close(runner.release)

	deadline := time.Now().Add(5 * time.Second)
	for {
		job, err := repo.GetByID(ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == repository.JobStatusRunning || job.Status == repository.JobStatusQueued {
			if time.Now().After(deadline) {
				t.Fatalf("job did not reach a terminal state, status=%s", job.Status)
			}
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if job.Status != repository.JobStatusSucceeded {
			t.Fatalf("uncanceled run must succeed, got %s", job.Status)
		}
		break
	}
}
