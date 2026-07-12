package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
)

// CreateSensitivityTestRequest starts a sensitivity analysis job against a run.
type CreateSensitivityTestRequest struct {
	PlanID          string  `json:"-"`
	IdempotencyKey  string  `json:"-"`
	SimulationRunID string  `json:"simulation_run_id,omitempty"`
	Runs            *int    `json:"runs,omitempty"`
	Seed            *string `json:"seed,omitempty"`
}

// CreateSensitivityTestResponse returns the enqueued job.
type CreateSensitivityTestResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// SensitivityTestView is the API view of a sensitivity test job.
type SensitivityTestView struct {
	TaskID            string          `json:"task_id"`
	PlanID            string          `json:"plan_id"`
	SimulationRunID   string          `json:"simulation_run_id"`
	Status            string          `json:"status"`
	InputHash         string          `json:"input_hash"`
	CurrentConfigHash string          `json:"current_config_hash"`
	ResultStale       bool            `json:"result_stale"`
	Result            json.RawMessage `json:"result_json,omitempty"`
	CreatedAt         int64           `json:"created_at"`
}

// SensitivityService orchestrates sensitivity test tasks.
type SensitivityService struct {
	sql         *sql.DB
	plans       *repository.PlanRepo
	tasks       *repository.WorkerTaskRepo
	coordinator *taskcore.Coordinator
	analysis    *repository.AnalysisRepo
	sims        *SimulationService
	hash        *ConfigHashService
}

func NewSensitivityService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	tasks *repository.WorkerTaskRepo,
	coordinator *taskcore.Coordinator,
	analysis *repository.AnalysisRepo,
	sims *SimulationService,
	hash *ConfigHashService,
) *SensitivityService {
	return &SensitivityService{
		sql: sqlDB, plans: plans, tasks: tasks, coordinator: coordinator,
		analysis: analysis, sims: sims, hash: hash,
	}
}

func (s *SensitivityService) Create(ctx context.Context,
	req CreateSensitivityTestRequest,
) (CreateSensitivityTestResponse, error) {
	runCtx, err := s.sims.ResolveAnalysisRun(ctx, req.PlanID, req.SimulationRunID)
	if err != nil {
		return CreateSensitivityTestResponse{}, err
	}
	inputHash := runCtx.InputHash

	if req.IdempotencyKey != "" {
		existing, found, err := findExistingIdempotentTask(
			ctx, s.tasks, "plan", req.PlanID, repository.WorkerTaskTypeSensitivity,
			req.IdempotencyKey, inputHash,
			"find sensitivity idempotency",
		)
		if err != nil {
			return CreateSensitivityTestResponse{}, err
		}
		if found {
			return CreateSensitivityTestResponse{TaskID: existing.ID, Status: existing.Status}, nil
		}
	}

	taskID := "task_" + uuid.New().String()
	pending, err := marshalPendingSnapshot(runCtx.Snapshot)
	if err != nil {
		return CreateSensitivityTestResponse{}, err
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		// Each Monte Carlo run keeps only the latest sensitivity result; cancel
		// any in-flight prior sensitivity job before dropping its record.
		if err := supersedePriorAnalysis(
			ctx, tx, s.coordinator, s.analysis, runCtx.RunID, repository.AnalysisTypeSensitivity,
		); err != nil {
			return err
		}
		payload, marshalErr := json.Marshal(map[string]string{
			"simulation_run_id": runCtx.RunID, "analysis_type": repository.AnalysisTypeSensitivity,
		})
		if marshalErr != nil {
			return marshalErr
		}
		if err := s.coordinator.CreateTx(ctx, tx, &repository.WorkerTask{
			ID: taskID, WorkerType: repository.WorkerTypeGo, Type: repository.WorkerTaskTypeSensitivity,
			Status: repository.WorkerTaskStatusPending, ScopeType: "plan", ScopeID: req.PlanID,
			DedupeKey: repository.WorkerTaskTypeSensitivity + "|simulation_run:" + runCtx.RunID,
			InputHash: inputHash, PayloadJSON: string(payload), ProgressTotal: 50,
		}); err != nil {
			return wrapRepo("create sensitivity task", err)
		}
		if err := s.analysis.CreatePending(ctx, tx, repository.AnalysisResult{
			TaskID: taskID, PlanID: req.PlanID, Type: repository.AnalysisTypeSensitivity,
			InputHash: inputHash, SimulationRunID: runCtx.RunID, ResultJSON: pending,
		}); err != nil {
			return wrapRepo("create sensitivity analysis pending", err)
		}
		if req.IdempotencyKey != "" {
			return s.tasks.SaveIdempotency(ctx, tx, "plan", req.PlanID,
				repository.WorkerTaskTypeSensitivity, req.IdempotencyKey, taskID,
				inputHash)
		}
		return nil
	})
	if err != nil {
		return CreateSensitivityTestResponse{}, wrapRepo("create sensitivity tx", err)
	}
	return CreateSensitivityTestResponse{TaskID: taskID, Status: repository.WorkerTaskStatusPending}, nil
}

func (s *SensitivityService) ListByPlan(ctx context.Context, planID string) ([]SensitivityTestView, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("get plan for sensitivity list", err)
	}
	recs, err := s.analysis.ListByPlan(ctx, planID, repository.AnalysisTypeSensitivity, 20)
	if err != nil {
		return nil, wrapRepo("list sensitivity tests", err)
	}
	currentHash, _ := s.hash.Compute(ctx, planID)
	out := make([]SensitivityTestView, len(recs))
	for i, rec := range recs {
		out[i] = s.toView(ctx, rec, currentHash)
	}
	return out, nil
}

// ListByRun returns the latest sensitivity test attached to a Monte Carlo run.
func (s *SensitivityService) ListByRun(ctx context.Context, planID, runID string) ([]SensitivityTestView, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("get plan for sensitivity list", err)
	}
	if err := s.sims.EnsureRunInPlan(ctx, planID, runID); err != nil {
		return nil, err
	}
	recs, err := s.analysis.ListBySimulationRun(ctx, runID, repository.AnalysisTypeSensitivity, 1)
	if err != nil {
		return nil, wrapRepo("list sensitivity tests by run", err)
	}
	currentHash, _ := s.hash.Compute(ctx, planID)
	out := make([]SensitivityTestView, len(recs))
	for i, rec := range recs {
		out[i] = s.toView(ctx, rec, currentHash)
	}
	return out, nil
}

func (s *SensitivityService) GetByTaskID(ctx context.Context, taskID string) (SensitivityTestView, error) {
	rec, err := s.analysis.GetByTaskID(ctx, taskID)
	if err != nil {
		if errors.Is(err, repository.ErrAnalysisNotFound) {
			return SensitivityTestView{}, newErr("sensitivity_test_not_found", "sensitivity test not found", nil)
		}
		return SensitivityTestView{}, wrapRepo("get sensitivity test", err)
	}
	if rec.Type != repository.AnalysisTypeSensitivity {
		return SensitivityTestView{}, newErr("sensitivity_test_not_found", "sensitivity test not found", nil)
	}
	currentHash, _ := s.hash.Compute(ctx, rec.PlanID)
	return s.toView(ctx, rec, currentHash), nil
}

func (s *SensitivityService) toView(ctx context.Context, rec repository.AnalysisResult,
	currentHash string,
) SensitivityTestView {
	task, _ := s.tasks.GetByID(ctx, rec.TaskID)
	stale := analysisResultStale(ctx, s.sims, rec.SimulationRunID, currentHash)
	view := SensitivityTestView{
		TaskID: rec.TaskID, PlanID: rec.PlanID, SimulationRunID: rec.SimulationRunID, InputHash: rec.InputHash,
		CurrentConfigHash: currentHash, ResultStale: stale, CreatedAt: rec.CreatedAt,
		Status: task.Status,
	}
	if !isPendingResult(rec.ResultJSON) {
		view.Result = json.RawMessage(rec.ResultJSON)
	}
	return view
}
