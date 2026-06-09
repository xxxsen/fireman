package jobs

import (
	"context"
	"encoding/json"

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

func (r *AnalysisRunner) RunStress(ctx context.Context, jobID string, cancelCheck func() bool, progress func(done, total int, phase string)) error {
	rec, err := r.analysis.GetByJobID(ctx, jobID)
	if err != nil {
		return err
	}
	var pending pendingAnalysisPayload
	if err := json.Unmarshal([]byte(rec.ResultJSON), &pending); err != nil {
		return err
	}
	report := stress.Run(&pending.InputSnapshot, stress.RunOptions{
		Runs:     pending.InputSnapshot.Parameters.SimulationRuns,
		Progress: progress, CancelCheck: cancelCheck,
	})
	if cancelCheck != nil && cancelCheck() {
		return context.Canceled
	}
	b, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return r.analysis.Complete(ctx, jobID, string(b))
}

func (r *AnalysisRunner) RunSensitivity(ctx context.Context, jobID string, cancelCheck func() bool, progress func(done, total int, phase string)) error {
	rec, err := r.analysis.GetByJobID(ctx, jobID)
	if err != nil {
		return err
	}
	var pending pendingAnalysisPayload
	if err := json.Unmarshal([]byte(rec.ResultJSON), &pending); err != nil {
		return err
	}
	report, err := sensitivity.Run(&pending.InputSnapshot, sensitivity.RunOptions{
		Runs:     pending.InputSnapshot.Parameters.SimulationRuns,
		Progress: progress, CancelCheck: cancelCheck,
	})
	if err != nil {
		return err
	}
	if cancelCheck != nil && cancelCheck() {
		return context.Canceled
	}
	b, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return r.analysis.Complete(ctx, jobID, string(b))
}
