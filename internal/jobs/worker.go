package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

const (
	staleHeartbeat      = 10 * time.Minute
	maxAutoRetry        = 1
	heartbeatEvery      = 10 * time.Second
	shutdownCleanupWait = 5 * time.Second
)

// Runner executes simulation jobs using the frozen input snapshot.
type Runner interface {
	RunSimulation(
		ctx context.Context,
		jobID, runID string,
		snapshot *simulation.InputSnapshot,
		cancelCheck func() bool,
		progress func(done, total int, phase string),
	) error
}

// AnalysisRunnerIface executes stress and sensitivity jobs.
type AnalysisRunnerIface interface {
	RunStress(
		ctx context.Context,
		jobID string,
		cancelCheck func() bool,
		progress func(done, total int, phase string),
	) error
	RunSensitivity(
		ctx context.Context,
		jobID string,
		cancelCheck func() bool,
		progress func(done, total int, phase string),
	) error
}

// Worker polls and executes queued jobs.
type Worker struct {
	db                *sql.DB
	jobs              *repository.JobRepo
	sims              *repository.SimulationRepo
	runner            Runner
	analysis          AnalysisRunnerIface
	instrumentFetch   *InstrumentFetchRunner
	events            *EventHub
	logger            *slog.Logger
	interval          time.Duration
	heartbeatInterval time.Duration
	maintenance       func() bool
}

func NewWorker(
	db *sql.DB,
	jobs *repository.JobRepo,
	sims *repository.SimulationRepo,
	runner Runner,
	analysis AnalysisRunnerIface,
	instrumentFetch *InstrumentFetchRunner,
	events *EventHub,
	logger *slog.Logger,
	maintenance func() bool,
) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		db: db, jobs: jobs, sims: sims, runner: runner, analysis: analysis,
		instrumentFetch: instrumentFetch, events: events, logger: logger, maintenance: maintenance,
		interval: 500 * time.Millisecond,
	}
}

func (w *Worker) Start(ctx context.Context, concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}
	w.reconcileStale(ctx)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			w.loop(ctx)
		})
	}
	wg.Wait()
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
			completed, shutdown := w.waitActiveJob(ctx, activeJob, jobDone, jobCancel, &hb)
			if shutdown {
				return
			}
			if completed {
				activeJob = ""
				jobDone = nil
				jobCancel = nil
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

func (w *Worker) waitActiveJob(
	ctx context.Context,
	activeJob string,
	jobDone chan struct{},
	jobCancel context.CancelFunc,
	hbOut **time.Ticker,
) (bool, bool) {
	if *hbOut == nil {
		every := w.heartbeatInterval
		if every <= 0 {
			every = heartbeatEvery
		}
		*hbOut = time.NewTicker(every)
	}
	select {
	case <-ctx.Done():
		if jobCancel != nil {
			jobCancel()
		}
		<-jobDone
		w.requeueInterrupted(ctx, activeJob)
		return false, true
	case <-jobDone:
		if *hbOut != nil {
			(*hbOut).Stop()
			*hbOut = nil
		}
		return true, false
	case <-(*hbOut).C:
		_ = w.jobs.Heartbeat(ctx, activeJob)
		return false, false
	}
}

func jobWriteCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), shutdownCleanupWait)
}

func (w *Worker) requeueInterrupted(ctx context.Context, jobID string) {
	if jobID == "" {
		return
	}
	cleanupCtx, cancel := jobWriteCtx(ctx)
	defer cancel()
	cancelRequested, err := w.jobs.IsCancelRequested(cleanupCtx, jobID)
	if err != nil {
		w.logger.Error("check cancel_requested before requeue failed", "job_id", jobID, "error", err)
		return
	}
	if cancelRequested {
		w.logger.Info("skip requeue for user-canceled job", "job_id", jobID)
		return
	}
	requeued, err := w.jobs.RequeueIfRunning(cleanupCtx, jobID)
	if err != nil {
		w.logger.Error("requeue interrupted job failed", "job_id", jobID, "error", err)
		return
	}
	if requeued {
		w.logger.Info("requeued interrupted job", "job_id", jobID)
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
	case repository.JobTypeInstrumentFetch:
		w.executeInstrumentFetch(ctx, job)
		return
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

	cancelCheck := w.cancelCheck(ctx, job.ID)
	progress := w.jobProgress(ctx, job.ID)

	progress(0, snap.Parameters.SimulationRuns, "simulating")
	if err := w.runner.RunSimulation(ctx, job.ID, run.ID, &snap, cancelCheck, progress); err != nil {
		if w.handleRunError(ctx, job.ID, run.ID, cancelCheck, err) {
			return
		}
		w.fail(ctx, job.ID, "simulation_failed", err.Error())
		return
	}
	w.finish(ctx, job.ID, repository.JobStatusSucceeded, "", run.ID)
}

type analysisRunFunc func(
	ctx context.Context,
	jobID string,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
) error

func (w *Worker) executeAnalysisJob(
	ctx context.Context,
	job repository.Job,
	runnerMissingMsg string,
	phase string,
	total int,
	failCode string,
	run analysisRunFunc,
) {
	if w.analysis == nil {
		w.fail(ctx, job.ID, "runner_missing", runnerMissingMsg)
		return
	}
	cancelCheck := w.cancelCheck(ctx, job.ID)
	progress := w.jobProgress(ctx, job.ID)
	progress(0, total, phase)
	err := run(ctx, job.ID, cancelCheck, progress)
	if err != nil {
		// Worker shutdown without a user/supersede cancel: leave the job running so
		// the reconciler can requeue it.
		if ctx.Err() != nil && !cancelCheck() {
			return
		}
		// A genuine computation failure (no cancel pending) fails the job.
		if !cancelCheck() && !errors.Is(err, context.Canceled) {
			w.fail(ctx, job.ID, failCode, err.Error())
			return
		}
		// Otherwise it was canceled mid-run; converge to canceled below.
	}
	w.finishAnalysis(ctx, job.ID, err == nil)
}

// finishAnalysis converges a stress/sensitivity job to its terminal state with
// cancel priority. A concurrent supersede may set cancel_requested after the
// runner's last cancelCheck has already passed, so a successful run must not
// blindly overwrite that with "succeeded": it only succeeds while no cancel is
// pending, otherwise it finishes as canceled, preserving the supersede error
// code. This conditional convergence is used only for analysis jobs.
func (w *Worker) finishAnalysis(ctx context.Context, jobID string, computeSucceeded bool) {
	writeCtx, cancel := jobWriteCtx(ctx)
	defer cancel()
	if computeSucceeded {
		updated, err := w.jobs.FinishRunningIfNotCanceled(writeCtx, jobID)
		if err != nil {
			w.logger.Error("finish analysis job failed", "job_id", jobID, "error", err)
			return
		}
		if updated {
			w.events.Publish(Event{JobID: jobID, Status: repository.JobStatusSucceeded})
			return
		}
	}
	canceled, err := w.jobs.FinishCanceledIfRequested(writeCtx, jobID)
	if err != nil {
		w.logger.Error("finish canceled analysis job failed", "job_id", jobID, "error", err)
		return
	}
	if !canceled {
		// Job is no longer running (already terminal or requeued); nothing to do.
		return
	}
	var code, msg string
	if job, getErr := w.jobs.GetByID(writeCtx, jobID); getErr == nil {
		code, msg = job.ErrorCode, job.ErrorMessage
	}
	w.events.Publish(Event{
		JobID: jobID, Status: repository.JobStatusCanceled, ErrorCode: code, ErrorMessage: msg,
	})
}

func (w *Worker) executeStress(ctx context.Context, job repository.Job) {
	w.executeAnalysisJob(
		ctx, job, "stress runner not configured", "stress", 8, "stress_failed", w.analysis.RunStress,
	)
}

func (w *Worker) executeSensitivity(ctx context.Context, job repository.Job) {
	w.executeAnalysisJob(
		ctx, job, "sensitivity runner not configured", "sensitivity", 50, "sensitivity_failed",
		w.analysis.RunSensitivity,
	)
}

func (w *Worker) executeInstrumentFetch(ctx context.Context, job repository.Job) {
	if w.instrumentFetch == nil {
		w.fail(ctx, job.ID, "runner_missing", "instrument fetch runner not configured")
		return
	}
	cancelCheck := w.cancelCheck(ctx, job.ID)
	progress := w.jobProgress(ctx, job.ID)
	if err := w.instrumentFetch.Run(ctx, job, cancelCheck, progress); err != nil {
		if w.handleInstrumentFetchError(ctx, job.ID, cancelCheck, err) {
			return
		}
		code, msg := "fetch_failed", err.Error()
		if c, ok := errorCode(err); ok {
			code = c
			msg = c
		}
		w.fail(ctx, job.ID, code, msg)
		return
	}
	w.finish(ctx, job.ID, repository.JobStatusSucceeded, "", "")
}

func (w *Worker) cancelCheck(ctx context.Context, jobID string) func() bool {
	return func() bool {
		checkCtx := ctx
		if ctx.Err() != nil {
			var cancel context.CancelFunc
			checkCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), shutdownCleanupWait)
			defer cancel()
		}
		ok, _ := w.jobs.IsCancelRequested(checkCtx, jobID)
		return ok
	}
}

func (w *Worker) jobProgress(ctx context.Context, jobID string) func(done, total int, phase string) {
	return func(done, total int, phase string) {
		_ = w.jobs.UpdateProgress(ctx, jobID, done, total, phase)
		w.events.Publish(Event{
			JobID: jobID, Status: repository.JobStatusRunning,
			Phase: phase, ProgressCurrent: done, ProgressTotal: total,
		})
	}
}

func (w *Worker) handleRunError(
	ctx context.Context,
	jobID, runID string,
	cancelCheck func() bool,
	_ error,
) bool {
	if ctx.Err() != nil && !cancelCheck() {
		return true
	}
	if cancelCheck() {
		w.finish(ctx, jobID, repository.JobStatusCanceled, "canceled by user", runID)
		return true
	}
	return false
}

func (w *Worker) handleInstrumentFetchError(
	ctx context.Context,
	jobID string,
	cancelCheck func() bool,
	err error,
) bool {
	if errors.Is(err, ErrFetchCanceled) {
		w.events.Publish(Event{
			JobID: jobID, Status: repository.JobStatusCanceled,
			ErrorCode: "fetch_canceled", ErrorMessage: "instrument fetch canceled by user",
		})
		return true
	}
	if ctx.Err() != nil && !cancelCheck() {
		return true
	}
	if cancelCheck() {
		w.finish(ctx, jobID, repository.JobStatusCanceled, "instrument fetch canceled by user", "")
		w.events.Publish(Event{
			JobID: jobID, Status: repository.JobStatusCanceled,
			ErrorCode: "fetch_canceled", ErrorMessage: "instrument fetch canceled by user",
		})
		return true
	}
	return false
}

func (w *Worker) fail(ctx context.Context, jobID, code, msg string) {
	writeCtx, cancel := jobWriteCtx(ctx)
	defer cancel()
	if err := w.jobs.Finish(writeCtx, jobID, repository.JobStatusFailed, code, msg); err != nil {
		w.logger.Error("finish failed job failed", "job_id", jobID, "error", err)
	}
	w.events.Publish(Event{JobID: jobID, Status: repository.JobStatusFailed, ErrorCode: code, ErrorMessage: msg})
}

func (w *Worker) finish(ctx context.Context, jobID, status, msg, runID string) {
	writeCtx, cancel := jobWriteCtx(ctx)
	defer cancel()
	if err := w.jobs.Finish(writeCtx, jobID, status, "", msg); err != nil {
		w.logger.Error("finish job failed", "job_id", jobID, "status", status, "error", err)
	}
	w.events.Publish(Event{JobID: jobID, Status: status, ErrorMessage: msg, RunID: runID})
}

// SimulationRunner persists Monte Carlo output.
type SimulationRunner struct {
	db   *sql.DB
	sims *repository.SimulationRepo
}

func NewSimulationRunner(db *sql.DB, sims *repository.SimulationRepo) *SimulationRunner {
	return &SimulationRunner{db: db, sims: sims}
}

func (r *SimulationRunner) RunSimulation(
	ctx context.Context,
	_, runID string,
	snap *simulation.InputSnapshot,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
) error {
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
		return fmt.Errorf("marshal simulation summary: %w", err)
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

	qRows := quantileRows(runID, result.QuantileSeries)
	realRows := quantileRows(runID, result.RealQuantileSeries)

	if err := fdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		if err := r.sims.Complete(ctx, tx, runID, result.SuccessCount, result.FailureCount, summaryJSON); err != nil {
			return fmt.Errorf("complete simulation run: %w", err)
		}
		if err := r.sims.ReplacePathIndex(ctx, tx, runID, rows); err != nil {
			return fmt.Errorf("replace path index: %w", err)
		}
		if err := r.sims.ReplaceQuantileSeries(ctx, tx, runID, qRows); err != nil {
			return fmt.Errorf("replace quantile series: %w", err)
		}
		if err := r.sims.ReplaceRealQuantileSeries(ctx, tx, runID, realRows); err != nil {
			return fmt.Errorf("replace real quantile series: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("persist simulation run: %w", err)
	}
	return nil
}

// quantileRows maps an engine quantile series to repository rows for a run.
func quantileRows(runID string, series []simulation.QuantilePoint) []repository.QuantileSeriesRow {
	out := make([]repository.QuantileSeriesRow, len(series))
	for i, q := range series {
		out[i] = repository.QuantileSeriesRow{
			RunID: runID, MonthOffset: q.MonthOffset,
			P00Minor: q.P00Minor, P05Minor: q.P05Minor, P25Minor: q.P25Minor,
			P50Minor: q.P50Minor, P75Minor: q.P75Minor, P95Minor: q.P95Minor,
		}
	}
	return out
}
