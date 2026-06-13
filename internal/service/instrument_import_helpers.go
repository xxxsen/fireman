package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/fireman/fireman/internal/repository"
)

func (s *InstrumentService) checkExistingInstrumentImport(
	ctx context.Context,
	market, instrumentType, code, adjust, inputHash string,
) error {
	existing, err := s.instRepo.FindByKey(ctx, market, instrumentType, code, adjust)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return nil
		}
		return wrapRepo("find instrument by key", err)
	}
	if existing.Status == "active" || existing.Status == "fetch_failed" {
		return newErr("instrument_already_exists", "instrument already imported",
			map[string]any{"instrument_id": existing.ID})
	}
	if existing.Status != "pending_fetch" {
		return nil
	}
	job, err := s.jobs.FindInProgressByInputHash(ctx, repository.JobTypeInstrumentFetch, inputHash)
	if err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			return nil
		}
		return wrapRepo("find in-progress fetch job", err)
	}
	return newErr(
		"instrument_fetch_in_progress",
		"instrument fetch already in progress",
		map[string]any{
			"instrument_id": existing.ID, "job_id": job.ID,
		},
	)
}

func (s *InstrumentService) checkInProgressFetchJob(ctx context.Context, inputHash string) error {
	job, err := s.jobs.FindInProgressByInputHash(ctx, repository.JobTypeInstrumentFetch, inputHash)
	if err != nil {
		if errors.Is(err, repository.ErrJobNotFound) {
			return nil
		}
		return wrapRepo("find in-progress fetch job", err)
	}
	var payload InstrumentFetchPayload
	_ = json.Unmarshal([]byte(job.PayloadJSON), &payload)
	details := map[string]any{"job_id": job.ID}
	if payload.InstrumentID != "" {
		details["instrument_id"] = payload.InstrumentID
	}
	return newErr(
		"instrument_fetch_in_progress",
		"instrument fetch already in progress",
		details,
	)
}

func applyScenarioCopyDefaults(req *ScenarioCreateRequest, src repository.AllocationScenario) {
	req.Weights = src.Weights
	req.RegionTargets = src.RegionTargets
	if req.Name == "" {
		req.Name = src.Name + " (副本)"
	}
	if req.Description == "" {
		req.Description = src.Description
	}
}
