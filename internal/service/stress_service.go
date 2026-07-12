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
	taskcore "github.com/fireman/fireman/internal/task"
)

// CreateStressTestRequest starts a stress analysis job against a Monte Carlo run.
type CreateStressTestRequest struct {
	PlanID          string  `json:"-"`
	IdempotencyKey  string  `json:"-"`
	SimulationRunID string  `json:"simulation_run_id,omitempty"`
	Runs            *int    `json:"runs,omitempty"`
	Seed            *string `json:"seed,omitempty"`
}

// CreateStressTestResponse returns the enqueued job.
type CreateStressTestResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// StressTestView is the API view of a stress test job.
type StressTestView struct {
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

// StressService orchestrates stress test tasks.
type StressService struct {
	sql         *sql.DB
	plans       *repository.PlanRepo
	tasks       *repository.WorkerTaskRepo
	coordinator *taskcore.Coordinator
	analysis    *repository.AnalysisRepo
	sims        *SimulationService
	hash        *ConfigHashService
}

func NewStressService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	tasks *repository.WorkerTaskRepo,
	coordinator *taskcore.Coordinator,
	analysis *repository.AnalysisRepo,
	sims *SimulationService,
	hash *ConfigHashService,
) *StressService {
	return &StressService{
		sql: sqlDB, plans: plans, tasks: tasks, coordinator: coordinator,
		analysis: analysis, sims: sims, hash: hash,
	}
}

func (s *StressService) Create(ctx context.Context, req CreateStressTestRequest) (CreateStressTestResponse, error) {
	runCtx, err := s.sims.ResolveAnalysisRun(ctx, req.PlanID, req.SimulationRunID)
	if err != nil {
		return CreateStressTestResponse{}, err
	}
	inputHash := runCtx.InputHash

	if req.IdempotencyKey != "" {
		existing, found, err := findExistingIdempotentTask(
			ctx, s.tasks, "plan", req.PlanID, repository.WorkerTaskTypeStress,
			req.IdempotencyKey, inputHash,
			"find stress idempotency",
		)
		if err != nil {
			return CreateStressTestResponse{}, err
		}
		if found {
			return CreateStressTestResponse{TaskID: existing.ID, Status: existing.Status}, nil
		}
	}

	taskID := "task_" + uuid.New().String()
	pending, err := marshalPendingSnapshot(runCtx.Snapshot)
	if err != nil {
		return CreateStressTestResponse{}, err
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		// Each Monte Carlo run keeps only the latest stress result; cancel any
		// in-flight prior stress job before dropping its record.
		if err := supersedePriorAnalysis(
			ctx, tx, s.coordinator, s.analysis, runCtx.RunID, repository.AnalysisTypeStress,
		); err != nil {
			return err
		}
		payload, marshalErr := json.Marshal(map[string]string{
			"simulation_run_id": runCtx.RunID, "analysis_type": repository.AnalysisTypeStress,
		})
		if marshalErr != nil {
			return marshalErr
		}
		if err := s.coordinator.CreateTx(ctx, tx, &repository.WorkerTask{
			ID: taskID, WorkerType: repository.WorkerTypeGo, Type: repository.WorkerTaskTypeStress,
			Status: repository.WorkerTaskStatusPending, ScopeType: "plan", ScopeID: req.PlanID,
			DedupeKey: repository.WorkerTaskTypeStress + "|simulation_run:" + runCtx.RunID,
			InputHash: inputHash, PayloadJSON: string(payload), ProgressTotal: 8,
		}); err != nil {
			return wrapRepo("create stress task", err)
		}
		if err := s.analysis.CreatePending(ctx, tx, repository.AnalysisResult{
			TaskID: taskID, PlanID: req.PlanID, Type: repository.AnalysisTypeStress,
			InputHash: inputHash, SimulationRunID: runCtx.RunID, ResultJSON: pending,
		}); err != nil {
			return wrapRepo("create stress analysis pending", err)
		}
		if req.IdempotencyKey != "" {
			return s.tasks.SaveIdempotency(ctx, tx, "plan", req.PlanID,
				repository.WorkerTaskTypeStress, req.IdempotencyKey, taskID, inputHash)
		}
		return nil
	})
	if err != nil {
		return CreateStressTestResponse{}, wrapRepo("create stress tx", err)
	}
	return CreateStressTestResponse{TaskID: taskID, Status: repository.WorkerTaskStatusPending}, nil
}

func (s *StressService) ListByPlan(ctx context.Context, planID string) ([]StressTestView, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("get plan for stress list", err)
	}
	recs, err := s.analysis.ListByPlan(ctx, planID, repository.AnalysisTypeStress, 20)
	if err != nil {
		return nil, wrapRepo("list stress tests", err)
	}
	currentHash, _ := s.hash.Compute(ctx, planID)
	out := make([]StressTestView, len(recs))
	for i, rec := range recs {
		out[i] = s.toView(ctx, rec, currentHash)
	}
	return out, nil
}

// ListByRun returns the latest stress test attached to a Monte Carlo run.
func (s *StressService) ListByRun(ctx context.Context, planID, runID string) ([]StressTestView, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("get plan for stress list", err)
	}
	if err := s.sims.EnsureRunInPlan(ctx, planID, runID); err != nil {
		return nil, err
	}
	recs, err := s.analysis.ListBySimulationRun(ctx, runID, repository.AnalysisTypeStress, 1)
	if err != nil {
		return nil, wrapRepo("list stress tests by run", err)
	}
	currentHash, _ := s.hash.Compute(ctx, planID)
	out := make([]StressTestView, len(recs))
	for i, rec := range recs {
		out[i] = s.toView(ctx, rec, currentHash)
	}
	return out, nil
}

func (s *StressService) GetByTaskID(ctx context.Context, taskID string) (StressTestView, error) {
	rec, err := s.analysis.GetByTaskID(ctx, taskID)
	if err != nil {
		if errors.Is(err, repository.ErrAnalysisNotFound) {
			return StressTestView{}, newErr("stress_test_not_found", "stress test not found", nil)
		}
		return StressTestView{}, wrapRepo("get stress test", err)
	}
	if rec.Type != repository.AnalysisTypeStress {
		return StressTestView{}, newErr("stress_test_not_found", "stress test not found", nil)
	}
	currentHash, _ := s.hash.Compute(ctx, rec.PlanID)
	return s.toView(ctx, rec, currentHash), nil
}

func (s *StressService) toView(ctx context.Context, rec repository.AnalysisResult, currentHash string) StressTestView {
	task, _ := s.tasks.GetByID(ctx, rec.TaskID)
	stale := analysisResultStale(ctx, s.sims, rec.SimulationRunID, currentHash)
	view := StressTestView{
		TaskID: rec.TaskID, PlanID: rec.PlanID, SimulationRunID: rec.SimulationRunID, InputHash: rec.InputHash,
		CurrentConfigHash: currentHash, ResultStale: stale, CreatedAt: rec.CreatedAt,
		Status: task.Status,
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
