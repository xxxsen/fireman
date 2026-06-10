package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
)

// JobService exposes job status and cancellation.
type JobService struct {
	sql      *sql.DB
	jobs     *repository.JobRepo
	instRepo *repository.InstrumentRepo
	sims     *repository.SimulationRepo
	events   *jobs.EventHub
}

func NewJobService(
	sqlDB *sql.DB,
	jobs *repository.JobRepo,
	instRepo *repository.InstrumentRepo,
	sims *repository.SimulationRepo,
	events *jobs.EventHub,
) *JobService {
	return &JobService{sql: sqlDB, jobs: jobs, instRepo: instRepo, sims: sims, events: events}
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
		if job.Type == repository.JobTypeInstrumentFetch {
			if err := s.cancelQueuedInstrumentFetch(ctx, job); err != nil {
				return repository.Job{}, err
			}
		} else if err := s.jobs.CancelQueued(ctx, jobID); err != nil {
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

func (s *JobService) cancelQueuedInstrumentFetch(ctx context.Context, job repository.Job) error {
	var payload repository.InstrumentFetchPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return err
	}
	return fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.jobs.CancelQueuedWithError(ctx, tx, job.ID, "fetch_canceled", "instrument fetch canceled by user"); err != nil {
			return err
		}
		if payload.InstrumentID == "" {
			return nil
		}
		return s.instRepo.UpdateStatusTx(ctx, tx, payload.InstrumentID, "fetch_failed")
	})
}

func (s *JobService) EventsHub() *jobs.EventHub {
	return s.events
}
