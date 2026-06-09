package service

import (
	"context"
	"errors"
	"time"

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
)

// JobService exposes job status and cancellation.
type JobService struct {
	jobs   *repository.JobRepo
	sims   *repository.SimulationRepo
	events *jobs.EventHub
}

func NewJobService(jobs *repository.JobRepo, sims *repository.SimulationRepo, events *jobs.EventHub) *JobService {
	return &JobService{jobs: jobs, sims: sims, events: events}
}

func (s *JobService) Get(ctx context.Context, jobID string) (repository.Job, error) {
	job, err := s.jobs.GetByID(ctx, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			return repository.Job{}, newErr("job_not_found", "job not found", nil)
		}
		return repository.Job{}, err
	}
	return job, nil
}

func (s *JobService) Cancel(ctx context.Context, jobID string) (repository.Job, error) {
	job, err := s.jobs.GetByID(ctx, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			return repository.Job{}, newErr("job_not_found", "job not found", nil)
		}
		return repository.Job{}, err
	}
	switch job.Status {
	case repository.JobStatusSucceeded, repository.JobStatusFailed, repository.JobStatusCanceled:
		return repository.Job{}, newErr("job_already_terminal", "job already finished", map[string]any{"status": job.Status})
	case repository.JobStatusQueued:
		if err := s.jobs.CancelQueued(ctx, jobID); err != nil {
			if errors.Is(err, repository.ErrJobNotFound) {
				return repository.Job{}, newErr("job_not_found", "job not found", nil)
			}
			return repository.Job{}, err
		}
		job.Status = repository.JobStatusCanceled
		job.CancelRequested = true
		now := time.Now().UnixMilli()
		job.FinishedAt = &now
		s.events.Publish(jobs.Event{
			JobID: jobID, Status: repository.JobStatusCanceled,
			ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
		})
		return job, nil
	}
	if err := s.jobs.RequestCancel(ctx, jobID); err != nil {
		return repository.Job{}, err
	}
	job.CancelRequested = true
	s.events.Publish(jobs.Event{
		JobID: jobID, Status: job.Status, Phase: job.Phase,
		ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
	})
	return job, nil
}

func (s *JobService) EventsHub() *jobs.EventHub {
	return s.events
}
