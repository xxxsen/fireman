package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

// CreateStressTestRequest starts a stress analysis job.
type CreateStressTestRequest struct {
	PlanID         string `json:"-"`
	IdempotencyKey string `json:"-"`
	Runs           *int   `json:"runs,omitempty"`
	Seed           *int64 `json:"seed,omitempty"`
}

// CreateStressTestResponse returns the enqueued job.
type CreateStressTestResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

// StressTestView is the API view of a stress test job.
type StressTestView struct {
	JobID             string          `json:"job_id"`
	PlanID            string          `json:"plan_id"`
	Status            string          `json:"status"`
	InputHash         string          `json:"input_hash"`
	CurrentConfigHash string          `json:"current_config_hash"`
	ResultStale       bool            `json:"result_stale"`
	Result            json.RawMessage `json:"result_json,omitempty"`
	CreatedAt         int64           `json:"created_at"`
}

// StressService orchestrates stress test jobs.
type StressService struct {
	sql      *sql.DB
	plans    *repository.PlanRepo
	jobs     *repository.JobRepo
	analysis *repository.AnalysisRepo
	sims     *SimulationService
	hash     *ConfigHashService
}

func NewStressService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	jobs *repository.JobRepo,
	analysis *repository.AnalysisRepo,
	sims *SimulationService,
	hash *ConfigHashService,
) *StressService {
	return &StressService{
		sql: sqlDB, plans: plans, jobs: jobs, analysis: analysis, sims: sims, hash: hash,
	}
}

func (s *StressService) Create(ctx context.Context, req CreateStressTestRequest) (CreateStressTestResponse, error) {
	snap, inputHash, err := s.sims.BuildInputSnapshot(ctx, req.PlanID, req.Runs, req.Seed)
	if err != nil {
		return CreateStressTestResponse{}, err
	}

	if req.IdempotencyKey != "" {
		existing, storedHash, err := s.jobs.FindIdempotency(ctx, req.PlanID, repository.JobTypeStress, req.IdempotencyKey)
		if err == nil {
			if storedHash != inputHash {
				return CreateStressTestResponse{}, newErr("idempotency_conflict", "idempotency key reused with different input", nil)
			}
			return CreateStressTestResponse{JobID: existing.ID, Status: existing.Status}, nil
		}
		if !errors.Is(err, repository.ErrJobNotFound) {
			return CreateStressTestResponse{}, err
		}
	}

	jobID := "job_" + uuid.New().String()
	pending, err := marshalPendingSnapshot(snap)
	if err != nil {
		return CreateStressTestResponse{}, err
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.jobs.Create(ctx, tx, repository.Job{
			ID: jobID, PlanID: req.PlanID, Type: repository.JobTypeStress,
			Status: repository.JobStatusQueued, InputHash: inputHash,
			ProgressTotal: 8,
		}); err != nil {
			return err
		}
		if err := s.analysis.CreatePending(ctx, tx, repository.AnalysisResult{
			JobID: jobID, PlanID: req.PlanID, Type: repository.AnalysisTypeStress,
			InputHash: inputHash, ResultJSON: pending,
		}); err != nil {
			return err
		}
		if req.IdempotencyKey != "" {
			return s.jobs.SaveIdempotency(ctx, tx, req.PlanID, repository.JobTypeStress, req.IdempotencyKey, jobID, inputHash)
		}
		return nil
	})
	if err != nil {
		return CreateStressTestResponse{}, err
	}
	return CreateStressTestResponse{JobID: jobID, Status: repository.JobStatusQueued}, nil
}

func (s *StressService) ListByPlan(ctx context.Context, planID string) ([]StressTestView, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, err
	}
	recs, err := s.analysis.ListByPlan(ctx, planID, repository.AnalysisTypeStress, 20)
	if err != nil {
		return nil, err
	}
	currentHash, _ := s.hash.Compute(ctx, planID)
	out := make([]StressTestView, len(recs))
	for i, rec := range recs {
		out[i] = s.toView(ctx, rec, currentHash)
	}
	return out, nil
}

func (s *StressService) GetByJobID(ctx context.Context, jobID string) (StressTestView, error) {
	rec, err := s.analysis.GetByJobID(ctx, jobID)
	if err != nil {
		if errors.Is(err, repository.ErrAnalysisNotFound) {
			return StressTestView{}, newErr("stress_test_not_found", "stress test not found", nil)
		}
		return StressTestView{}, err
	}
	if rec.Type != repository.AnalysisTypeStress {
		return StressTestView{}, newErr("stress_test_not_found", "stress test not found", nil)
	}
	currentHash, _ := s.hash.Compute(ctx, rec.PlanID)
	return s.toView(ctx, rec, currentHash), nil
}

func (s *StressService) toView(ctx context.Context, rec repository.AnalysisResult, currentHash string) StressTestView {
	job, _ := s.jobs.GetByID(ctx, rec.JobID)
	stale := currentHash != "" && rec.InputHash != currentHash
	view := StressTestView{
		JobID: rec.JobID, PlanID: rec.PlanID, InputHash: rec.InputHash,
		CurrentConfigHash: currentHash, ResultStale: stale, CreatedAt: rec.CreatedAt,
		Status: job.Status,
	}
	if !isPendingResult(rec.ResultJSON) {
		view.Result = json.RawMessage(rec.ResultJSON)
	}
	return view
}

func marshalPendingSnapshot(snap *simulation.InputSnapshot) (string, error) {
	b, err := json.Marshal(map[string]any{
		"pending": true, "input_snapshot": snap,
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func isPendingResult(raw string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return false
	}
	p, _ := m["pending"].(bool)
	return p
}
