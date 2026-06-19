package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fireman/fireman/internal/repository"
)

// supersededByNewerAnalysisCode marks an attached-analysis job canceled because
// the user re-ran the same analysis type against the same Monte Carlo run.
const supersededByNewerAnalysisCode = "superseded_by_newer_analysis"

// supersedePriorAnalysis cancels any in-flight (queued/running) analysis job of
// analysisType bound to runID, then removes its analysis_results rows. This keeps
// "only the newest stress/sensitivity per run" while ensuring the old job does
// not later fail (its analysis record is gone) or keep wasting a worker. It must
// run inside tx so cancellation and deletion commit atomically with the new job.
func supersedePriorAnalysis(
	ctx context.Context,
	tx *sql.Tx,
	jobs *repository.JobRepo,
	analysis *repository.AnalysisRepo,
	runID, analysisType string,
) error {
	priorJobIDs, err := analysis.JobIDsBySimulationRunAndType(ctx, tx, runID, analysisType)
	if err != nil {
		return wrapRepo("list prior analysis jobs", err)
	}
	for _, jobID := range priorJobIDs {
		// Queued jobs are canceled immediately. If the worker already claimed the
		// job (now running) the conditional update affects 0 rows; we then fall
		// back to a cancel request that the runner's cancelCheck honors mid-run.
		err := jobs.CancelQueuedWithError(ctx, tx, jobID, supersededByNewerAnalysisCode,
			"superseded by a newer analysis run")
		if errors.Is(err, repository.ErrJobNotFound) {
			if err := jobs.RequestCancelTx(ctx, tx, jobID); err != nil {
				return wrapRepo("request cancel prior analysis job", err)
			}
			continue
		}
		if err != nil {
			return wrapRepo("cancel prior analysis job", err)
		}
	}
	if err := analysis.DeleteBySimulationRunAndType(ctx, tx, runID, analysisType); err != nil {
		return wrapRepo("delete prior analysis", err)
	}
	return nil
}
