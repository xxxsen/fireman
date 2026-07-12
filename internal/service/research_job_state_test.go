package service

import (
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func TestApplyOptimizationJobStateConvergesActiveRun(t *testing.T) {
	heartbeat := int64(1234)
	finished := int64(5678)
	view := ResearchOptimizationView{Status: repository.ResearchRunStatusRunning}
	job := repository.Job{
		Status: repository.JobStatusFailed, RetryCount: 1, HeartbeatAt: &heartbeat,
		ErrorCode: repository.JobErrorWorkerInterrupted, ErrorMessage: "interrupted",
		FinishedAt: &finished,
	}

	applyOptimizationJobState(&view, job)

	if view.Status != repository.ResearchRunStatusFailed ||
		view.ErrorCode != repository.JobErrorWorkerInterrupted ||
		view.CompletedAt == nil || *view.CompletedAt != finished {
		t.Fatalf("view did not converge to terminal job: %+v", view)
	}
	if view.Job == nil || view.Job.RetryCount != 1 ||
		view.Job.HeartbeatAt == nil || *view.Job.HeartbeatAt != heartbeat {
		t.Fatalf("job diagnostics missing: %+v", view.Job)
	}
}

func TestApplyBacktestJobStateDoesNotDowngradeTerminalRun(t *testing.T) {
	view := ResearchRunView{Status: repository.ResearchRunStatusSucceeded}
	job := repository.Job{Status: repository.JobStatusQueued, RetryCount: 1}

	applyBacktestJobState(&view, job)

	if view.Status != repository.ResearchRunStatusSucceeded {
		t.Fatalf("terminal run was downgraded to %s", view.Status)
	}
	if view.Job == nil || view.Job.Status != repository.JobStatusQueued || view.Job.RetryCount != 1 {
		t.Fatalf("job diagnostics missing: %+v", view.Job)
	}
}
