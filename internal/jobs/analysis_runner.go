package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/sensitivity"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/stress"
)

// AnalysisRunner executes stress and sensitivity jobs.
type AnalysisRunner struct {
	analysis *repository.AnalysisRepo
}

func NewAnalysisRunner(analysis *repository.AnalysisRepo) *AnalysisRunner {
	return &AnalysisRunner{analysis: analysis}
}

type pendingAnalysisPayload struct {
	Pending       bool                     `json:"pending"`
	InputSnapshot simulation.InputSnapshot `json:"input_snapshot"`
}

func (r *AnalysisRunner) RunStress(
	ctx context.Context,
	jobID string,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
) error {
	return r.runAnalysis(ctx, jobID, cancelCheck, progress, func(snap *simulation.InputSnapshot) (any, error) {
		return stress.Run(snap, stress.RunOptions{
			Runs: snap.Parameters.SimulationRuns, Progress: progress, CancelCheck: cancelCheck,
		}), nil
	})
}

func (r *AnalysisRunner) RunSensitivity(
	ctx context.Context,
	jobID string,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
) error {
	return r.runAnalysis(ctx, jobID, cancelCheck, progress, func(snap *simulation.InputSnapshot) (any, error) {
		return sensitivity.Run(snap, sensitivity.RunOptions{
			Runs: snap.Parameters.SimulationRuns, Progress: progress, CancelCheck: cancelCheck,
		})
	})
}

func (r *AnalysisRunner) runAnalysis(
	ctx context.Context,
	jobID string,
	cancelCheck func() bool,
	_ func(done, total int, phase string),
	run func(*simulation.InputSnapshot) (any, error),
) error {
	rec, err := r.analysis.GetByJobID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("load analysis job: %w", err)
	}
	var pending pendingAnalysisPayload
	if err := json.Unmarshal([]byte(rec.ResultJSON), &pending); err != nil {
		return fmt.Errorf("decode pending analysis payload: %w", err)
	}
	report, err := run(&pending.InputSnapshot)
	if err != nil {
		return fmt.Errorf("run analysis job: %w", err)
	}
	if cancelCheck != nil && cancelCheck() {
		return context.Canceled
	}
	b, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal analysis report: %w", err)
	}
	if err := r.analysis.Complete(ctx, jobID, string(b)); err != nil {
		return fmt.Errorf("complete analysis job: %w", err)
	}
	return nil
}
