package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

const (
	staleHeartbeat = 10 * time.Minute
	maxAutoRetry   = 1
	heartbeatEvery = 10 * time.Second
)

// Runner executes simulation jobs using the frozen input snapshot.
type Runner interface {
	RunSimulation(ctx context.Context, jobID, runID string, snapshot *simulation.InputSnapshot, cancelCheck func() bool, progress func(done, total int, phase string)) error
}

// AnalysisRunnerIface executes stress and sensitivity jobs.
type AnalysisRunnerIface interface {
	RunStress(ctx context.Context, jobID string, cancelCheck func() bool, progress func(done, total int, phase string)) error
	RunSensitivity(ctx context.Context, jobID string, cancelCheck func() bool, progress func(done, total int, phase string)) error
}

// Worker polls and executes queued jobs.
type Worker struct {
	db                *sql.DB
	jobs              *repository.JobRepo
	sims              *repository.SimulationRepo
	runner            Runner
	analysis          AnalysisRunnerIface
	events            *EventHub
	logger            *slog.Logger
	interval          time.Duration
	heartbeatInterval time.Duration
	maintenance       func() bool
}

func NewWorker(db *sql.DB, jobs *repository.JobRepo, sims *repository.SimulationRepo, runner Runner, analysis AnalysisRunnerIface, events *EventHub, logger *slog.Logger, maintenance func() bool) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		db: db, jobs: jobs, sims: sims, runner: runner, analysis: analysis,
		events: events, logger: logger, maintenance: maintenance,
		interval: 500 * time.Millisecond,
	}
}

func (w *Worker) Start(ctx context.Context, concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}
	w.reconcileStale(ctx)
	for i := 0; i < concurrency; i++ {
		go w.loop(ctx)
	}
}

func (w *Worker) loop(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	var activeJob string
	var jobDone chan struct{}
	var jobCancel context.CancelFunc
	var hb *time.Ticker
	defer func() {
		if hb != nil {
			hb.Stop()
		}
	}()

	for {
		if activeJob != "" && jobDone != nil {
			if hb == nil {
				every := w.heartbeatInterval
				if every <= 0 {
					every = heartbeatEvery
				}
				hb = time.NewTicker(every)
			}
			select {
			case <-ctx.Done():
				if jobCancel != nil {
					jobCancel()
				}
				<-jobDone
				return
			case <-jobDone:
				hb.Stop()
				hb = nil
				activeJob = ""
				jobDone = nil
				jobCancel = nil
			case <-hb.C:
				_ = w.jobs.Heartbeat(ctx, activeJob)
			}
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if activeJob != "" {
				continue
			}
			if w.maintenance != nil && w.maintenance() {
				continue
			}
			job, err := w.jobs.ClaimNextQueued(ctx)
			if err != nil {
				continue
			}
			activeJob = job.ID
			jobDone = make(chan struct{})
			jobCtx, cancel := context.WithCancel(ctx)
			jobCancel = cancel
			go func(j repository.Job) {
				w.execute(jobCtx, j)
				close(jobDone)
			}(job)
		}
	}
}

func (w *Worker) reconcileStale(ctx context.Context) {
	staleBefore := time.Now().Add(-staleHeartbeat).UnixMilli()
	n, err := w.jobs.RequeueStaleRunning(ctx, staleBefore, maxAutoRetry)
	if err != nil {
		w.logger.Error("reconcile stale jobs failed", "error", err)
		return
	}
	if n > 0 {
		w.logger.Info("requeued stale running jobs", "count", n)
	}
}

func (w *Worker) execute(ctx context.Context, job repository.Job) {
	switch job.Type {
	case repository.JobTypeStress:
		w.executeStress(ctx, job)
		return
	case repository.JobTypeSensitivity:
		w.executeSensitivity(ctx, job)
		return
	}
	run, err := w.sims.GetByJobID(ctx, job.ID)
	if err != nil {
		w.fail(ctx, job.ID, "snapshot_missing", err.Error())
		return
	}
	var snap simulation.InputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snap); err != nil {
		w.fail(ctx, job.ID, "snapshot_invalid", err.Error())
		return
	}

	cancelCheck := func() bool {
		ok, _ := w.jobs.IsCancelRequested(ctx, job.ID)
		return ok
	}
	progress := func(done, total int, phase string) {
		_ = w.jobs.UpdateProgress(ctx, job.ID, done, total, phase)
		w.events.Publish(Event{
			JobID: job.ID, Status: repository.JobStatusRunning,
			Phase: phase, ProgressCurrent: done, ProgressTotal: total,
		})
	}

	progress(0, snap.Parameters.SimulationRuns, "simulating")
	if err := w.runner.RunSimulation(ctx, job.ID, run.ID, &snap, cancelCheck, progress); err != nil {
		if cancelCheck() {
			w.finish(ctx, job.ID, repository.JobStatusCanceled, "", "canceled by user", run.ID)
			return
		}
		w.fail(ctx, job.ID, "simulation_failed", err.Error())
		return
	}
	w.finish(ctx, job.ID, repository.JobStatusSucceeded, "", "", run.ID)
}

func (w *Worker) executeStress(ctx context.Context, job repository.Job) {
	if w.analysis == nil {
		w.fail(ctx, job.ID, "runner_missing", "stress runner not configured")
		return
	}
	cancelCheck := func() bool {
		ok, _ := w.jobs.IsCancelRequested(ctx, job.ID)
		return ok
	}
	progress := func(done, total int, phase string) {
		_ = w.jobs.UpdateProgress(ctx, job.ID, done, total, phase)
		w.events.Publish(Event{
			JobID: job.ID, Status: repository.JobStatusRunning,
			Phase: phase, ProgressCurrent: done, ProgressTotal: total,
		})
	}
	progress(0, 8, "stress")
	if err := w.analysis.RunStress(ctx, job.ID, cancelCheck, progress); err != nil {
		if cancelCheck() {
			w.finish(ctx, job.ID, repository.JobStatusCanceled, "", "canceled by user", "")
			return
		}
		w.fail(ctx, job.ID, "stress_failed", err.Error())
		return
	}
	w.finish(ctx, job.ID, repository.JobStatusSucceeded, "", "", "")
}

func (w *Worker) executeSensitivity(ctx context.Context, job repository.Job) {
	if w.analysis == nil {
		w.fail(ctx, job.ID, "runner_missing", "sensitivity runner not configured")
		return
	}
	cancelCheck := func() bool {
		ok, _ := w.jobs.IsCancelRequested(ctx, job.ID)
		return ok
	}
	progress := func(done, total int, phase string) {
		_ = w.jobs.UpdateProgress(ctx, job.ID, done, total, phase)
		w.events.Publish(Event{
			JobID: job.ID, Status: repository.JobStatusRunning,
			Phase: phase, ProgressCurrent: done, ProgressTotal: total,
		})
	}
	progress(0, 50, "sensitivity")
	if err := w.analysis.RunSensitivity(ctx, job.ID, cancelCheck, progress); err != nil {
		if cancelCheck() {
			w.finish(ctx, job.ID, repository.JobStatusCanceled, "", "canceled by user", "")
			return
		}
		w.fail(ctx, job.ID, "sensitivity_failed", err.Error())
		return
	}
	w.finish(ctx, job.ID, repository.JobStatusSucceeded, "", "", "")
}

func (w *Worker) fail(ctx context.Context, jobID, code, msg string) {
	_ = w.jobs.Finish(ctx, jobID, repository.JobStatusFailed, code, msg)
	w.events.Publish(Event{JobID: jobID, Status: repository.JobStatusFailed, ErrorCode: code, ErrorMessage: msg})
}

func (w *Worker) finish(ctx context.Context, jobID, status, code, msg, runID string) {
	_ = w.jobs.Finish(ctx, jobID, status, code, msg)
	w.events.Publish(Event{JobID: jobID, Status: status, ErrorCode: code, ErrorMessage: msg, RunID: runID})
}

// SimulationRunner persists Monte Carlo output.
type SimulationRunner struct {
	db   *sql.DB
	sims *repository.SimulationRepo
}

func NewSimulationRunner(db *sql.DB, sims *repository.SimulationRepo) *SimulationRunner {
	return &SimulationRunner{db: db, sims: sims}
}

func (r *SimulationRunner) RunSimulation(ctx context.Context, jobID, runID string, snap *simulation.InputSnapshot, cancelCheck func() bool, progress func(done, total int, phase string)) error {
	result := simulation.Run(snap, simulation.RunOptions{
		Runs:        snap.Parameters.SimulationRuns,
		Progress:    progress,
		CancelCheck: cancelCheck,
	})
	if cancelCheck != nil && cancelCheck() {
		return context.Canceled
	}

	summaryJSON, err := json.Marshal(result.Summary)
	if err != nil {
		return err
	}

	repPath := make(map[int]string)
	for label, pathNo := range result.Representative {
		repPath[pathNo] = label
	}

	rows := make([]repository.PathIndexRow, 0, len(result.Paths))
	for _, p := range result.Paths {
		rows = append(rows, repository.PathIndexRow{
			RunID: runID, PathNo: p.PathNo, PathSeed: p.PathSeed, Succeeded: p.Succeeded,
			FailureMonth: p.FailureMonth, TerminalWealthMinor: p.TerminalWealthMinor,
			MaxDrawdown: p.MaxDrawdown, RepresentativePercentile: repPath[p.PathNo],
		})
	}

	qRows := make([]repository.QuantileSeriesRow, len(result.QuantileSeries))
	for i, q := range result.QuantileSeries {
		qRows[i] = repository.QuantileSeriesRow{
			RunID: runID, MonthOffset: q.MonthOffset,
			P00Minor: q.P00Minor, P05Minor: q.P05Minor, P25Minor: q.P25Minor,
			P50Minor: q.P50Minor, P75Minor: q.P75Minor, P95Minor: q.P95Minor,
		}
	}

	return fdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		if err := r.sims.Complete(ctx, tx, runID, result.SuccessCount, result.FailureCount, summaryJSON); err != nil {
			return err
		}
		if err := r.sims.ReplacePathIndex(ctx, tx, runID, rows); err != nil {
			return err
		}
		return r.sims.ReplaceQuantileSeries(ctx, tx, runID, qRows)
	})
}
