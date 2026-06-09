package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

// CreateSensitivityTestRequest starts a sensitivity analysis job.
type CreateSensitivityTestRequest struct {
	PlanID         string  `json:"-"`
	IdempotencyKey string  `json:"-"`
	Runs           *int    `json:"runs,omitempty"`
	Seed           *string `json:"seed,omitempty"`
}

// CreateSensitivityTestResponse returns the enqueued job.
type CreateSensitivityTestResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// SensitivityTestView is the API view of a sensitivity test job.
type SensitivityTestView struct {
	JobID             string          `json:"job_id"`
	PlanID            string          `json:"plan_id"`
	Status            string          `json:"status"`
	InputHash         string          `json:"input_hash"`
	CurrentConfigHash string          `json:"current_config_hash"`
	ResultStale       bool            `json:"result_stale"`
	Result            json.RawMessage `json:"result_json,omitempty"`
	CreatedAt         int64           `json:"created_at"`
}

// SensitivityService orchestrates sensitivity test jobs.
type SensitivityService struct {
	sql      *sql.DB
	plans    *repository.PlanRepo
	jobs     *repository.JobRepo
	analysis *repository.AnalysisRepo
	sims     *SimulationService
	hash     *ConfigHashService
}

func NewSensitivityService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	jobs *repository.JobRepo,
	analysis *repository.AnalysisRepo,
	sims *SimulationService,
	hash *ConfigHashService,
) *SensitivityService {
	return &SensitivityService{
		sql: sqlDB, plans: plans, jobs: jobs, analysis: analysis, sims: sims, hash: hash,
	}
}

func (s *SensitivityService) Create(ctx context.Context, req CreateSensitivityTestRequest) (CreateSensitivityTestResponse, error) {
	snap, inputHash, err := s.sims.BuildInputSnapshot(ctx, req.PlanID, req.Runs, req.Seed)
	if err != nil {
		return CreateSensitivityTestResponse{}, err
	}

	if req.IdempotencyKey != "" {
		existing, storedHash, err := s.jobs.FindIdempotency(ctx, req.PlanID, repository.JobTypeSensitivity, req.IdempotencyKey)
		if err == nil {
			if storedHash != inputHash {
				return CreateSensitivityTestResponse{}, newErr("idempotency_conflict", "idempotency key reused with different input", nil)
			}
			return CreateSensitivityTestResponse{JobID: existing.ID, Status: existing.Status}, nil
		}
		if !errors.Is(err, repository.ErrJobNotFound) {
			return CreateSensitivityTestResponse{}, err
		}
	}

	jobID := "job_" + uuid.New().String()
	pending, err := marshalPendingSnapshot(snap)
	if err != nil {
		return CreateSensitivityTestResponse{}, err
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.jobs.Create(ctx, tx, repository.Job{
			ID: jobID, PlanID: req.PlanID, Type: repository.JobTypeSensitivity,
			Status: repository.JobStatusQueued, InputHash: inputHash,
			ProgressTotal: 50,
		}); err != nil {
			return err
		}
		if err := s.analysis.CreatePending(ctx, tx, repository.AnalysisResult{
			JobID: jobID, PlanID: req.PlanID, Type: repository.AnalysisTypeSensitivity,
			InputHash: inputHash, ResultJSON: pending,
		}); err != nil {
			return err
		}
		if req.IdempotencyKey != "" {
			return s.jobs.SaveIdempotency(ctx, tx, req.PlanID, repository.JobTypeSensitivity, req.IdempotencyKey, jobID, inputHash)
		}
		return nil
	})
	if err != nil {
		return CreateSensitivityTestResponse{}, err
	}
	return CreateSensitivityTestResponse{JobID: jobID, Status: repository.JobStatusQueued}, nil
}

func (s *SensitivityService) ListByPlan(ctx context.Context, planID string) ([]SensitivityTestView, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, err
	}
	recs, err := s.analysis.ListByPlan(ctx, planID, repository.AnalysisTypeSensitivity, 20)
	if err != nil {
		return nil, err
	}
	currentHash, _ := s.hash.Compute(ctx, planID)
	out := make([]SensitivityTestView, len(recs))
	for i, rec := range recs {
		out[i] = s.toView(ctx, rec, currentHash)
	}
	return out, nil
}

func (s *SensitivityService) GetByJobID(ctx context.Context, jobID string) (SensitivityTestView, error) {
	rec, err := s.analysis.GetByJobID(ctx, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrAnalysisNotFound) {
			return SensitivityTestView{}, newErr("sensitivity_test_not_found", "sensitivity test not found", nil)
		}
		return SensitivityTestView{}, err
	}
	if rec.Type != repository.AnalysisTypeSensitivity {
		return SensitivityTestView{}, newErr("sensitivity_test_not_found", "sensitivity test not found", nil)
	}
	currentHash, _ := s.hash.Compute(ctx, rec.PlanID)
	return s.toView(ctx, rec, currentHash), nil
}

func (s *SensitivityService) toView(ctx context.Context, rec repository.AnalysisResult, currentHash string) SensitivityTestView {
	job, _ := s.jobs.GetByID(ctx, rec.JobID)
	stale := currentHash != "" && rec.InputHash != currentHash
	view := SensitivityTestView{
		JobID: rec.JobID, PlanID: rec.PlanID, InputHash: rec.InputHash,
		CurrentConfigHash: currentHash, ResultStale: stale, CreatedAt: rec.CreatedAt,
		Status: job.Status,
	}
	if !isPendingResult(rec.ResultJSON) {
		view.Result = json.RawMessage(rec.ResultJSON)
	}
	return view
}
