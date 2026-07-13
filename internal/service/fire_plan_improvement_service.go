package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/improvement"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	taskcore "github.com/fireman/fireman/internal/task"
	"github.com/google/uuid"
)

const (
	improvementRetentionLimit = 20
	improvementPreviewTTL     = 15 * time.Minute
)

var errCandidateSnapshotMismatch = errors.New("candidate snapshot mismatch")

type FirePlanImprovementService struct {
	db          *sql.DB
	plans       *repository.PlanRepo
	params      *repository.ParametersRepo
	sims        *repository.SimulationRepo
	tasks       *repository.WorkerTaskRepo
	coordinator *taskcore.Coordinator
	runs        *repository.FirePlanImprovementRepo
	hash        *ConfigHashService
	simulation  *SimulationService
	now         func() time.Time
}

func NewFirePlanImprovementService(
	db *sql.DB, plans *repository.PlanRepo, params *repository.ParametersRepo,
	sims *repository.SimulationRepo, tasks *repository.WorkerTaskRepo,
	coordinator *taskcore.Coordinator, runs *repository.FirePlanImprovementRepo,
	hash *ConfigHashService, simulationService *SimulationService,
) *FirePlanImprovementService {
	return &FirePlanImprovementService{
		db: db, plans: plans, params: params, sims: sims,
		tasks: tasks, coordinator: coordinator, runs: runs, hash: hash,
		simulation: simulationService, now: time.Now,
	}
}

type ImprovementSourceRun struct {
	ID                 string  `json:"id"`
	EngineVersion      string  `json:"engine_version"`
	Runs               int     `json:"runs"`
	SuccessProbability float64 `json:"success_probability"`
	SuccessWilsonLow   float64 `json:"success_wilson_low"`
	SuccessWilsonHigh  float64 `json:"success_wilson_high"`
	CreatedAt          int64   `json:"created_at"`
}

type ImprovementParameters struct {
	RetirementAge               int   `json:"retirement_age"`
	EndAge                      int   `json:"end_age"`
	AnnualSavingsMinor          int64 `json:"annual_savings_minor"`
	AnnualSpendingMinor         int64 `json:"annual_spending_minor"`
	AnnualRetirementIncomeMinor int64 `json:"annual_retirement_income_minor"`
}

type ImprovementReadiness struct {
	Ready             bool                  `json:"ready"`
	SourceRun         *ImprovementSourceRun `json:"source_run,omitempty"`
	CurrentParameters ImprovementParameters `json:"current_parameters"`
	BlockingReasons   []ImprovementIssue    `json:"blocking_reasons"`
	Warnings          []ImprovementIssue    `json:"warnings"`
}

type ImprovementIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CreateImprovementRequest struct {
	SimulationRunID          string                            `json:"simulation_run_id"`
	TargetSuccessProbability float64                           `json:"target_success_probability"`
	RetirementDelay          *improvement.RetirementDelayLever `json:"retirement_delay,omitempty"`
	SavingsIncrease          *improvement.MoneyIncreaseLever   `json:"savings_increase,omitempty"`
	SpendingReduction        *improvement.MoneyReductionLever  `json:"spending_reduction,omitempty"`
	RetirementIncomeIncrease *improvement.MoneyIncreaseLever   `json:"retirement_income_increase,omitempty"`
	IdempotencyKey           string                            `json:"-"`
}

func (r CreateImprovementRequest) config() improvement.Config {
	return improvement.Config{
		TargetSuccessProbability: r.TargetSuccessProbability,
		RetirementDelay:          r.RetirementDelay, SavingsIncrease: r.SavingsIncrease,
		SpendingReduction:        r.SpendingReduction,
		RetirementIncomeIncrease: r.RetirementIncomeIncrease,
	}
}

type CreateImprovementResponse struct {
	RunID  string `json:"run_id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Reused bool   `json:"reused"`
}

type ImprovementRunView struct {
	ID                    string                                     `json:"id"`
	TaskID                string                                     `json:"task_id"`
	PlanID                string                                     `json:"plan_id"`
	SourceSimulationRunID string                                     `json:"source_simulation_run_id"`
	InputHash             string                                     `json:"input_hash"`
	AlgorithmVersion      string                                     `json:"algorithm_version"`
	SourceEngineVersion   string                                     `json:"source_engine_version"`
	SourceConfigHash      string                                     `json:"source_config_hash"`
	SourceMarketHash      string                                     `json:"source_market_hash"`
	Config                improvement.Config                         `json:"config"`
	Result                *improvement.Result                        `json:"result,omitempty"`
	Status                string                                     `json:"status"`
	ProgressCurrent       int                                        `json:"progress_current"`
	ProgressTotal         int                                        `json:"progress_total"`
	Phase                 string                                     `json:"phase"`
	AttemptCount          int                                        `json:"attempt_count"`
	ErrorCode             string                                     `json:"error_code,omitempty"`
	ErrorMessage          string                                     `json:"error_message,omitempty"`
	CreatedAt             int64                                      `json:"created_at"`
	CompletedAt           *int64                                     `json:"completed_at,omitempty"`
	ResultStale           bool                                       `json:"result_stale"`
	Application           *repository.FirePlanImprovementApplication `json:"application,omitempty"`
}

type ImprovementRunSummary struct {
	ID                    string                `json:"id"`
	TaskID                string                `json:"task_id"`
	SourceSimulationRunID string                `json:"source_simulation_run_id"`
	TargetProbability     float64               `json:"target_probability"`
	Status                string                `json:"status"`
	ProgressCurrent       int                   `json:"progress_current"`
	ProgressTotal         int                   `json:"progress_total"`
	Phase                 string                `json:"phase"`
	ErrorCode             string                `json:"error_code,omitempty"`
	ErrorMessage          string                `json:"error_message,omitempty"`
	CreatedAt             int64                 `json:"created_at"`
	CompletedAt           *int64                `json:"completed_at,omitempty"`
	TargetReached         *bool                 `json:"target_reached,omitempty"`
	BestAttainable        *improvement.Proposal `json:"best_attainable,omitempty"`
	ResultStale           bool                  `json:"result_stale"`
}

func (s *FirePlanImprovementService) Readiness(
	ctx context.Context, planID, runID string,
) (ImprovementReadiness, error) {
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return ImprovementReadiness{}, newErr("plan_not_found", "plan not found", nil)
		}
		return ImprovementReadiness{}, wrapRepo("load improvement parameters", err)
	}
	readiness := ImprovementReadiness{CurrentParameters: improvementParameters(params)}
	run, snapshot, baseline, _, err := s.resolveSource(ctx, planID, runID)
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			readiness.BlockingReasons = []ImprovementIssue{{Code: appErr.Code, Message: appErr.Message}}
			return readiness, nil
		}
		return ImprovementReadiness{}, err
	}
	_ = snapshot
	readiness.Ready = true
	readiness.SourceRun = sourceRunView(run, baseline)
	return readiness, nil
}

//nolint:funlen,gocognit,gocyclo,nestif,wrapcheck // Producer validates, freezes and persists in one audit boundary.
func (s *FirePlanImprovementService) Create(
	ctx context.Context, planID string, req CreateImprovementRequest,
) (CreateImprovementResponse, error) {
	run, snapshot, baseline, configInput, err := s.resolveSource(ctx, planID, req.SimulationRunID)
	if err != nil {
		return CreateImprovementResponse{}, err
	}
	config := req.config()
	if err := config.Validate(snapshot.Parameters.EndAge, snapshot.Parameters.RetirementAge); err != nil {
		code := "improvement_config_invalid"
		if errors.Is(err, improvement.ErrNoEnabledLever) {
			code = "improvement_no_enabled_lever"
		}
		return CreateImprovementResponse{}, newErr(code, err.Error(), nil)
	}
	if baseline.SuccessWilsonLow >= config.TargetSuccessProbability {
		return CreateImprovementResponse{}, newErr("improvement_target_already_met",
			"current plan already meets the requested confidence target", map[string]any{
				"success_probability": baseline.SuccessProbability,
				"success_wilson_low":  baseline.SuccessWilsonLow,
				"success_wilson_high": baseline.SuccessWilsonHigh,
			})
	}
	configRaw, err := json.Marshal(config)
	if err != nil {
		return CreateImprovementResponse{}, err
	}
	configInputRaw, err := json.Marshal(configInput)
	if err != nil {
		return CreateImprovementResponse{}, err
	}
	paths, err := s.sims.ListPathIndex(ctx, run.ID)
	if err != nil {
		return CreateImprovementResponse{}, wrapRepo("load source path outcomes", err)
	}
	outcomes := make([]bool, len(paths))
	for i := range paths {
		outcomes[i] = paths[i].Succeeded
	}
	encoded, outcomeHash := improvement.EncodeOutcomeBits(outcomes)
	baseline.Outcomes = nil
	var summary simulation.Summary
	if err := json.Unmarshal(run.SummaryJSON, &summary); err != nil {
		return CreateImprovementResponse{}, newErr("improvement_result_inconsistent", "source summary is invalid", nil)
	}
	frozen := improvement.FrozenInput{
		SourceSnapshot: snapshot, SourceSummary: summary,
		Baseline: baseline, BaselineBits: encoded, BaselineHash: outcomeHash,
		Config: config, ConfigHashInputJSON: configInputRaw,
	}
	frozenRaw, err := json.Marshal(frozen)
	if err != nil {
		return CreateImprovementResponse{}, err
	}
	inputHash, err := improvementInputHash(configRaw, snapshot, configInputRaw, outcomeHash)
	if err != nil {
		return CreateImprovementResponse{}, err
	}
	if existing, err := s.runs.FindReusable(ctx, planID, inputHash); err == nil {
		return createImprovementResponse(existing, true), nil
	} else if !errors.Is(err, repository.ErrFirePlanImprovementNotFound) {
		return CreateImprovementResponse{}, wrapRepo("find reusable improvement", err)
	}
	if req.IdempotencyKey != "" {
		if task, found, err := findExistingIdempotentTask(ctx, s.tasks, planID,
			repository.WorkerTaskTypeFirePlanImprovement, req.IdempotencyKey, inputHash,
			"find improvement idempotency"); err != nil {
			return CreateImprovementResponse{}, err
		} else if found {
			existing, getErr := s.runs.GetByTaskID(ctx, task.ID)
			if getErr != nil {
				return CreateImprovementResponse{}, wrapRepo("load idempotent improvement", getErr)
			}
			return createImprovementResponse(existing, true), nil
		}
	}
	record := repository.FirePlanImprovementRun{
		ID: "fpir_" + uuid.NewString(), TaskID: "task_" + uuid.NewString(), PlanID: planID,
		SourceSimulationRunID: run.ID, InputHash: inputHash, AlgorithmVersion: improvement.AlgorithmVersion,
		SourceEngineVersion: snapshot.EngineVersion, SourceConfigHash: snapshot.ConfigHash,
		SourceMarketHash: snapshot.MarketSnapshotHash, ConfigJSON: string(configRaw),
		InputSnapshotJSON: string(frozenRaw),
	}
	var bound repository.WorkerTask
	var reused bool
	err = fdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		payload, marshalErr := json.Marshal(map[string]string{"run_id": record.ID})
		if marshalErr != nil {
			return marshalErr
		}
		task := repository.WorkerTask{
			ID: record.TaskID, WorkerType: repository.WorkerTypeGo,
			Type: repository.WorkerTaskTypeFirePlanImprovement, Status: repository.WorkerTaskStatusPending,
			ScopeType: "plan", ScopeID: planID,
			DedupeKey: repository.WorkerTaskTypeFirePlanImprovement + "|plan:" + planID,
			InputHash: inputHash, PayloadJSON: string(payload),
			ProgressTotal: improvement.SearchUpperBound(config),
		}
		var createErr error
		bound, reused, createErr = createOrReuseActiveTaskTx(
			ctx, tx, s.tasks, s.coordinator, task, func() error {
				if err := s.runs.CreateTx(ctx, tx, &record); err != nil {
					return err
				}
				return s.runs.PruneTx(ctx, tx, planID, improvementRetentionLimit)
			},
		)
		if createErr != nil {
			return createErr
		}
		if req.IdempotencyKey != "" {
			if err := s.tasks.SaveIdempotency(ctx, tx, "plan", planID,
				repository.WorkerTaskTypeFirePlanImprovement, req.IdempotencyKey, bound.ID, inputHash); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return CreateImprovementResponse{}, wrapRepo("create improvement transaction", err)
	}
	if reused {
		existing, getErr := s.runs.GetByTaskID(ctx, bound.ID)
		if getErr != nil {
			return CreateImprovementResponse{}, wrapRepo("load active improvement", getErr)
		}
		return createImprovementResponse(existing, true), nil
	}
	record.TaskStatus = repository.WorkerTaskStatusPending
	return createImprovementResponse(record, false), nil
}

func (s *FirePlanImprovementService) List(
	ctx context.Context, planID string, limit, offset int,
) ([]ImprovementRunSummary, int, error) {
	records, total, err := s.runs.ListByPlan(ctx, planID, limit, offset)
	if err != nil {
		return nil, 0, wrapRepo("list plan improvements", err)
	}
	out := make([]ImprovementRunSummary, len(records))
	for i := range records {
		view, err := s.toView(ctx, records[i], false)
		if err != nil {
			return nil, 0, err
		}
		summary := ImprovementRunSummary{
			ID: view.ID, TaskID: view.TaskID,
			SourceSimulationRunID: view.SourceSimulationRunID,
			TargetProbability:     view.Config.TargetSuccessProbability, Status: view.Status,
			ProgressCurrent: view.ProgressCurrent, ProgressTotal: view.ProgressTotal,
			Phase: view.Phase, ErrorCode: view.ErrorCode, ErrorMessage: view.ErrorMessage,
			CreatedAt: view.CreatedAt, CompletedAt: view.CompletedAt, ResultStale: view.ResultStale,
		}
		if view.Result != nil {
			reached := view.Result.TargetReached
			summary.TargetReached, summary.BestAttainable = &reached, view.Result.BestAttainable
		}
		out[i] = summary
	}
	return out, total, nil
}

func (s *FirePlanImprovementService) Get(ctx context.Context, runID string) (ImprovementRunView, error) {
	record, err := s.runs.GetByID(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrFirePlanImprovementNotFound) {
			return ImprovementRunView{}, newErr("improvement_run_not_found", "improvement run not found", nil)
		}
		return ImprovementRunView{}, wrapRepo("get improvement run", err)
	}
	return s.toView(ctx, record, true)
}

func (s *FirePlanImprovementService) toView(
	ctx context.Context, record repository.FirePlanImprovementRun, includeResult bool,
) (ImprovementRunView, error) {
	var config improvement.Config
	if err := json.Unmarshal([]byte(record.ConfigJSON), &config); err != nil {
		return ImprovementRunView{}, wrapRepo("decode improvement config", err)
	}
	view := ImprovementRunView{
		ID: record.ID, TaskID: record.TaskID, PlanID: record.PlanID,
		SourceSimulationRunID: record.SourceSimulationRunID, InputHash: record.InputHash,
		AlgorithmVersion: record.AlgorithmVersion, SourceEngineVersion: record.SourceEngineVersion,
		SourceConfigHash: record.SourceConfigHash, SourceMarketHash: record.SourceMarketHash,
		Config: config, Status: record.TaskStatus, ProgressCurrent: record.TaskProgressCurrent,
		ProgressTotal: record.TaskProgressTotal, Phase: record.TaskPhase,
		AttemptCount: record.TaskAttemptCount, ErrorCode: record.TaskErrorCode,
		ErrorMessage: record.TaskErrorMessage, CreatedAt: record.CreatedAt, CompletedAt: record.CompletedAt,
	}
	if includeResult && record.TaskStatus == repository.WorkerTaskStatusComplete {
		var result improvement.Result
		if err := json.Unmarshal(record.ResultJSON, &result); err != nil {
			return ImprovementRunView{}, wrapRepo("decode improvement result", err)
		}
		view.Result = &result
	}
	view.ResultStale = s.resultStale(ctx, record)
	if application, err := s.runs.GetApplication(ctx, record.ID); err == nil {
		view.Application = &application
	} else if !errors.Is(err, repository.ErrFirePlanImprovementNotFound) {
		return ImprovementRunView{}, wrapRepo("load improvement application", err)
	}
	return view, nil
}

type ImprovementParameterValues struct {
	RetirementAge               int   `json:"retirement_age"`
	AnnualSavingsMinor          int64 `json:"annual_savings_minor"`
	AnnualSpendingMinor         int64 `json:"annual_spending_minor"`
	AnnualRetirementIncomeMinor int64 `json:"annual_retirement_income_minor"`
}

type ImprovementPreview struct {
	RunID                     string                     `json:"run_id"`
	ProposalID                string                     `json:"proposal_id"`
	ExpectedPlanConfigVersion int                        `json:"expected_plan_config_version"`
	Before                    ImprovementParameterValues `json:"before"`
	After                     ImprovementParameterValues `json:"after"`
	Unchanged                 []string                   `json:"unchanged"`
	SourceRunID               string                     `json:"source_run_id"`
	AlgorithmVersion          string                     `json:"algorithm_version"`
	TargetProbability         float64                    `json:"target_probability"`
	SuccessProbability        float64                    `json:"success_probability"`
	SuccessWilsonLow          float64                    `json:"success_wilson_low"`
	SuccessWilsonHigh         float64                    `json:"success_wilson_high"`
	RetirementIncomeDelayed   bool                       `json:"retirement_income_delayed"`
	CurrentConfigHash         string                     `json:"current_config_hash"`
	CurrentMarketHash         string                     `json:"current_market_hash"`
	PreviewHash               string                     `json:"preview_hash"`
	PreviewExpiresAt          int64                      `json:"preview_expires_at"`
}

type PreviewImprovementRequest struct {
	ExpectedPlanConfigVersion int `json:"expected_plan_config_version"`
}

func (s *FirePlanImprovementService) Preview(
	ctx context.Context, runID, proposalID string, req PreviewImprovementRequest,
) (ImprovementPreview, error) {
	record, frozen, result, proposal, err := s.loadApplicable(ctx, runID, proposalID)
	if err != nil {
		return ImprovementPreview{}, err
	}
	plan, params, configHash, marketHash, err := s.currentState(ctx, record, frozen)
	if err != nil {
		return ImprovementPreview{}, err
	}
	if req.ExpectedPlanConfigVersion != plan.ConfigVersion || configHash != record.SourceConfigHash {
		return ImprovementPreview{}, newErr("improvement_preview_stale", "plan configuration has changed", nil)
	}
	if marketHash != record.SourceMarketHash {
		return ImprovementPreview{}, newErr("improvement_source_market_changed", "market inputs have changed", nil)
	}
	before := parameterValues(params)
	after := proposalValues(proposal)
	expiresAt := s.now().Add(improvementPreviewTTL).UnixMilli()
	hash := previewHash(record.ID, proposal.ID, plan.ConfigVersion, configHash, marketHash,
		before, after, expiresAt)
	return ImprovementPreview{
		RunID: record.ID, ProposalID: proposal.ID,
		ExpectedPlanConfigVersion: plan.ConfigVersion, Before: before, After: after,
		Unchanged:   []string{"持仓与权重", "收益与风险假设", "通胀与提款策略", "模拟 seed"},
		SourceRunID: record.SourceSimulationRunID, AlgorithmVersion: record.AlgorithmVersion,
		TargetProbability: result.TargetProbability, SuccessProbability: proposal.SuccessProbability,
		SuccessWilsonLow: proposal.SuccessWilsonLow, SuccessWilsonHigh: proposal.SuccessWilsonHigh,
		RetirementIncomeDelayed: proposal.DelayYears > 0 && params.AnnualRetirementIncomeMinor > 0,
		CurrentConfigHash:       configHash, CurrentMarketHash: marketHash,
		PreviewHash: hash, PreviewExpiresAt: expiresAt,
	}, nil
}

type ApplyImprovementRequest struct {
	ExpectedPlanConfigVersion int    `json:"expected_plan_config_version"`
	PreviewHash               string `json:"preview_hash"`
	PreviewExpiresAt          int64  `json:"preview_expires_at"`
}

type ApplyImprovementResponse struct {
	Application repository.FirePlanImprovementApplication `json:"application"`
	Plan        repository.Plan                           `json:"plan"`
	Parameters  PlanParametersAPI                         `json:"parameters"`
}

//nolint:funlen,gocognit,gocyclo,wrapcheck // Preview identity and transactional CAS checks stay in one boundary.
func (s *FirePlanImprovementService) Apply(
	ctx context.Context, runID, proposalID string, req ApplyImprovementRequest,
) (ApplyImprovementResponse, error) {
	record, frozen, _, proposal, err := s.loadApplicable(ctx, runID, proposalID)
	if err != nil {
		return ApplyImprovementResponse{}, err
	}
	plan, params, configHash, marketHash, err := s.currentState(ctx, record, frozen)
	if err != nil {
		return ApplyImprovementResponse{}, err
	}
	if req.ExpectedPlanConfigVersion != plan.ConfigVersion || configHash != record.SourceConfigHash ||
		s.now().UnixMilli() > req.PreviewExpiresAt {
		return ApplyImprovementResponse{}, newErr("improvement_preview_stale", "preview has expired or plan changed", nil)
	}
	if marketHash != record.SourceMarketHash {
		return ApplyImprovementResponse{}, newErr("improvement_source_market_changed", "market inputs have changed", nil)
	}
	before, after := parameterValues(params), proposalValues(proposal)
	wantPreviewHash := previewHash(record.ID, proposal.ID, plan.ConfigVersion, configHash,
		marketHash, before, after, req.PreviewExpiresAt)
	if req.PreviewHash == "" || req.PreviewHash != wantPreviewHash {
		return ApplyImprovementResponse{}, newErr("improvement_preview_stale", "preview identity is invalid", nil)
	}
	configInput, err := s.hash.SnapshotReadOnly(ctx, record.PlanID)
	if err != nil {
		return ApplyImprovementResponse{}, wrapRepo("build current config input", err)
	}
	candidateInput, err := improvement.ApplyConfigAdjustments(configInput, proposalAdjustments(proposal))
	if err != nil {
		return ApplyImprovementResponse{}, newErr("improvement_result_inconsistent", err.Error(), nil)
	}
	candidateHash, err := domain.ComputeConfigHash(candidateInput)
	if err != nil || candidateHash != proposal.CandidateConfigHash {
		return ApplyImprovementResponse{}, newErr("improvement_result_inconsistent",
			"proposal config hash cannot be reproduced", nil)
	}
	updatedParams := params
	updatedParams.RetirementAge = after.RetirementAge
	updatedParams.AnnualSavingsMinor = after.AnnualSavingsMinor
	updatedParams.AnnualSpendingMinor = after.AnnualSpendingMinor
	updatedParams.AnnualRetirementIncomeMinor = after.AnnualRetirementIncomeMinor
	if err := validateParameters(updatedParams); err != nil {
		return ApplyImprovementResponse{}, newErr("parameters_invalid", err.Error(), nil)
	}
	beforeRaw, _ := json.Marshal(before)
	afterRaw, _ := json.Marshal(after)
	application := repository.FirePlanImprovementApplication{
		ID:               "fpia_" + uuid.NewString(),
		ImprovementRunID: record.ID, ProposalID: proposal.ID, PlanID: record.PlanID,
		BeforeConfigVersion: plan.ConfigVersion, AfterConfigVersion: plan.ConfigVersion + 1,
		PreviewHash: req.PreviewHash, BeforeJSON: string(beforeRaw), AfterJSON: string(afterRaw),
		AppliedAt: s.now().UnixMilli(),
	}
	err = fdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		currentPlan, err := s.plans.GetByIDTx(ctx, tx, record.PlanID)
		if err != nil {
			return err
		}
		if currentPlan.ConfigVersion != plan.ConfigVersion {
			return repository.ErrVersionConflict
		}
		currentParams, err := s.params.GetTx(ctx, tx, record.PlanID)
		if err != nil {
			return err
		}
		if parameterValues(currentParams) != before {
			return repository.ErrVersionConflict
		}
		currentMarketHash, err := s.simulation.CurrentMarketSnapshotHashReadOnlyTx(
			ctx, tx, record.PlanID, frozen.SourceSnapshot,
		)
		if err != nil {
			return err
		}
		if currentMarketHash != record.SourceMarketHash {
			return newErr("improvement_source_market_changed", "market inputs changed while applying", nil)
		}
		if err := s.params.Upsert(ctx, tx, updatedParams); err != nil {
			return err
		}
		version, err := s.plans.BumpVersionTx(ctx, tx, record.PlanID, plan.ConfigVersion)
		if err != nil {
			return err
		}
		application.AfterConfigVersion = version
		return s.runs.CreateApplicationTx(ctx, tx, application)
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return ApplyImprovementResponse{}, newErr("improvement_preview_stale", "plan changed while applying", nil)
		}
		if isUniqueConstraintErr(err) {
			return ApplyImprovementResponse{}, newErr("improvement_proposal_already_applied",
				"proposal has already been applied", nil)
		}
		return ApplyImprovementResponse{}, wrapRepo("apply improvement transaction", err)
	}
	plan.ConfigVersion = application.AfterConfigVersion
	return ApplyImprovementResponse{
		Application: application, Plan: plan,
		Parameters: ParametersToAPI(updatedParams),
	}, nil
}

//nolint:funlen,gocognit,gocyclo,nestif // Source eligibility is intentionally audited as one ordered gate.
func (s *FirePlanImprovementService) resolveSource(
	ctx context.Context, planID, runID string,
) (repository.SimulationRun, simulation.InputSnapshot, simulation.OutcomeEvaluation, domain.ConfigHashInput, error) {
	var run repository.SimulationRun
	var err error
	if runID != "" {
		run, err = s.sims.GetByID(ctx, runID)
	} else {
		runs, listErr := s.sims.ListByPlan(ctx, planID, 20)
		if listErr != nil {
			err = listErr
		} else {
			for _, candidate := range runs {
				if candidate.TaskStatus == repository.WorkerTaskStatusComplete {
					run = candidate
					break
				}
			}
			if run.ID == "" {
				err = repository.ErrSimulationNotFound
			}
		}
	}
	if err != nil || run.PlanID != planID {
		return repository.SimulationRun{}, simulation.InputSnapshot{}, simulation.OutcomeEvaluation{},
			domain.ConfigHashInput{}, newErr("improvement_source_run_not_found", "source simulation not found", nil)
	}
	if run.TaskStatus != repository.WorkerTaskStatusComplete {
		return run, simulation.InputSnapshot{}, simulation.OutcomeEvaluation{}, domain.ConfigHashInput{},
			newErr("improvement_source_run_not_complete", "source simulation is not complete", nil)
	}
	var snapshot simulation.InputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snapshot); err != nil {
		return run, snapshot, simulation.OutcomeEvaluation{}, domain.ConfigHashInput{},
			newErr("improvement_result_inconsistent", "source snapshot is invalid", nil)
	}
	if snapshot.EngineVersion != simulation.EngineVersion || run.EngineVersion != simulation.EngineVersion {
		return run, snapshot, simulation.OutcomeEvaluation{}, domain.ConfigHashInput{},
			newErr("improvement_source_engine_legacy", "source simulation uses a legacy engine", nil)
	}
	configInput, err := s.hash.SnapshotReadOnly(ctx, planID)
	if err != nil {
		return run, snapshot, simulation.OutcomeEvaluation{}, domain.ConfigHashInput{}, err
	}
	currentConfigHash, err := domain.ComputeConfigHash(configInput)
	if err != nil || currentConfigHash != snapshot.ConfigHash {
		return run, snapshot, simulation.OutcomeEvaluation{}, configInput,
			newErr("improvement_source_run_stale", "source simulation no longer matches the plan", nil)
	}
	marketHash, err := s.simulation.CurrentMarketSnapshotHashReadOnly(ctx, planID, snapshot)
	if err != nil || marketHash != snapshot.MarketSnapshotHash {
		return run, snapshot, simulation.OutcomeEvaluation{}, configInput,
			newErr("improvement_source_market_changed", "source market inputs have changed", nil)
	}
	paths, err := s.sims.ListPathIndex(ctx, run.ID)
	if err != nil || len(paths) != snapshot.Parameters.SimulationRuns || len(paths) != run.Runs {
		return run, snapshot, simulation.OutcomeEvaluation{}, configInput,
			newErr("improvement_source_paths_incomplete", "source path index is incomplete", nil)
	}
	terminals, drawdowns, outcomes := make([]float64, len(paths)), make([]float64, len(paths)), make([]bool, len(paths))
	for i, path := range paths {
		if path.PathNo != i {
			return run, snapshot, simulation.OutcomeEvaluation{}, configInput,
				newErr("improvement_source_paths_incomplete", "source path index is not contiguous", nil)
		}
		terminals[i], drawdowns[i], outcomes[i] = float64(path.TerminalWealthMinor), path.MaxDrawdown, path.Succeeded
	}
	sort.Float64s(terminals)
	sort.Float64s(drawdowns)
	successes := 0
	for _, outcome := range outcomes {
		if outcome {
			successes++
		}
	}
	if successes != run.SuccessCount {
		return run, snapshot, simulation.OutcomeEvaluation{}, configInput,
			newErr("improvement_result_inconsistent", "source success count differs from path index", nil)
	}
	low, high := simulation.WilsonInterval(successes, len(paths), 1.96)
	baseline := simulation.OutcomeEvaluation{
		Runs: len(paths), SuccessCount: successes,
		SuccessProbability: float64(successes) / float64(len(paths)), SuccessWilsonLow: low,
		SuccessWilsonHigh: high, TerminalP50Minor: int64(math.Round(simulation.Quantile(terminals, 0.5))),
		MaxDrawdownP95: simulation.Quantile(drawdowns, 0.95), Outcomes: outcomes,
	}
	return run, snapshot, baseline, configInput, nil
}

func (s *FirePlanImprovementService) loadApplicable(
	ctx context.Context, runID, proposalID string,
) (repository.FirePlanImprovementRun, improvement.FrozenInput, improvement.Result, improvement.Proposal, error) {
	record, err := s.runs.GetByID(ctx, runID)
	if err != nil {
		return record, improvement.FrozenInput{}, improvement.Result{}, improvement.Proposal{},
			newErr("improvement_run_not_found", "improvement run not found", nil)
	}
	if record.TaskStatus != repository.WorkerTaskStatusComplete {
		return record, improvement.FrozenInput{}, improvement.Result{}, improvement.Proposal{},
			newErr("improvement_proposal_not_met", "improvement run is not complete", nil)
	}
	if _, err := s.runs.GetApplication(ctx, record.ID); err == nil {
		return record, improvement.FrozenInput{}, improvement.Result{}, improvement.Proposal{},
			newErr("improvement_proposal_already_applied", "an improvement proposal has already been applied", nil)
	} else if !errors.Is(err, repository.ErrFirePlanImprovementNotFound) {
		return record, improvement.FrozenInput{}, improvement.Result{}, improvement.Proposal{},
			wrapRepo("load improvement application", err)
	}
	var frozen improvement.FrozenInput
	var result improvement.Result
	if json.Unmarshal([]byte(record.InputSnapshotJSON), &frozen) != nil ||
		json.Unmarshal(record.ResultJSON, &result) != nil {
		return record, frozen, result, improvement.Proposal{},
			newErr("improvement_result_inconsistent", "improvement result cannot be decoded", nil)
	}
	for _, proposal := range result.Proposals {
		if proposal.ID == proposalID {
			if proposal.SuccessWilsonLow < result.TargetProbability {
				return record, frozen, result, proposal,
					newErr("improvement_proposal_not_met", "proposal does not meet target", nil)
			}
			return record, frozen, result, proposal, nil
		}
	}
	return record, frozen, result, improvement.Proposal{},
		newErr("improvement_proposal_not_found", "improvement proposal not found", nil)
}

func (s *FirePlanImprovementService) currentState(
	ctx context.Context, record repository.FirePlanImprovementRun, frozen improvement.FrozenInput,
) (repository.Plan, repository.PlanParameters, string, string, error) {
	plan, err := s.plans.GetByID(ctx, record.PlanID)
	if err != nil {
		return plan, repository.PlanParameters{}, "", "", wrapRepo("load improvement plan", err)
	}
	params, err := s.params.Get(ctx, record.PlanID)
	if err != nil {
		return plan, params, "", "", wrapRepo("load improvement parameters", err)
	}
	configInput, err := s.hash.SnapshotReadOnly(ctx, record.PlanID)
	if err != nil {
		return plan, params, "", "", wrapRepo("build improvement config input", err)
	}
	configHash, err := domain.ComputeConfigHash(configInput)
	if err != nil {
		return plan, params, "", "", fmt.Errorf("hash improvement config: %w", err)
	}
	marketHash, err := s.simulation.CurrentMarketSnapshotHashReadOnly(ctx, record.PlanID, frozen.SourceSnapshot)
	return plan, params, configHash, marketHash, wrapRepo("build improvement market identity", err)
}

func (s *FirePlanImprovementService) resultStale(ctx context.Context, record repository.FirePlanImprovementRun) bool {
	var frozen improvement.FrozenInput
	if json.Unmarshal([]byte(record.InputSnapshotJSON), &frozen) != nil {
		return true
	}
	_, _, configHash, marketHash, err := s.currentState(ctx, record, frozen)
	return err != nil || configHash != record.SourceConfigHash || marketHash != record.SourceMarketHash
}

func sourceRunView(run repository.SimulationRun, baseline simulation.OutcomeEvaluation) *ImprovementSourceRun {
	return &ImprovementSourceRun{
		ID: run.ID, EngineVersion: run.EngineVersion, Runs: run.Runs,
		SuccessProbability: baseline.SuccessProbability, SuccessWilsonLow: baseline.SuccessWilsonLow,
		SuccessWilsonHigh: baseline.SuccessWilsonHigh, CreatedAt: run.CreatedAt,
	}
}

func improvementParameters(params repository.PlanParameters) ImprovementParameters {
	return ImprovementParameters{
		RetirementAge: params.RetirementAge, EndAge: params.EndAge,
		AnnualSavingsMinor: params.AnnualSavingsMinor, AnnualSpendingMinor: params.AnnualSpendingMinor,
		AnnualRetirementIncomeMinor: params.AnnualRetirementIncomeMinor,
	}
}

func parameterValues(params repository.PlanParameters) ImprovementParameterValues {
	return ImprovementParameterValues{
		RetirementAge:      params.RetirementAge,
		AnnualSavingsMinor: params.AnnualSavingsMinor, AnnualSpendingMinor: params.AnnualSpendingMinor,
		AnnualRetirementIncomeMinor: params.AnnualRetirementIncomeMinor,
	}
}

func proposalValues(proposal improvement.Proposal) ImprovementParameterValues {
	return ImprovementParameterValues{
		RetirementAge:               proposal.ResultRetirementAge,
		AnnualSavingsMinor:          proposal.ResultAnnualSavingsMinor,
		AnnualSpendingMinor:         proposal.ResultAnnualSpendingMinor,
		AnnualRetirementIncomeMinor: proposal.ResultRetirementIncomeMinor,
	}
}

func proposalAdjustments(proposal improvement.Proposal) improvement.Adjustments {
	return improvement.Adjustments{
		DelayYears:                    proposal.DelayYears,
		SavingsIncreaseMinor:          proposal.SavingsIncreaseMinor,
		SpendingReductionMinor:        proposal.SpendingReductionMinor,
		RetirementIncomeIncreaseMinor: proposal.RetirementIncomeIncreaseMinor,
	}
}

func improvementInputHash(configRaw []byte, snapshot simulation.InputSnapshot,
	configInputRaw []byte, outcomeHash string,
) (string, error) {
	snapshotHash, err := simulation.HashInput(&snapshot)
	if err != nil {
		return "", fmt.Errorf("hash improvement source snapshot: %w", err)
	}
	payload := struct {
		AlgorithmVersion string          `json:"algorithm_version"`
		Config           json.RawMessage `json:"config"`
		SnapshotHash     string          `json:"snapshot_hash"`
		ConfigInput      json.RawMessage `json:"config_input"`
		OutcomeHash      string          `json:"outcome_hash"`
	}{improvement.AlgorithmVersion, configRaw, snapshotHash, configInputRaw, outcomeHash}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func createImprovementResponse(record repository.FirePlanImprovementRun, reused bool) CreateImprovementResponse {
	return CreateImprovementResponse{
		RunID: record.ID, TaskID: record.TaskID,
		Status: record.TaskStatus, Reused: reused,
	}
}

func previewHash(runID, proposalID string, version int, configHash, marketHash string,
	before, after ImprovementParameterValues, expiresAt int64,
) string {
	payload := struct {
		RunID                     string                     `json:"run_id"`
		ProposalID                string                     `json:"proposal_id"`
		ExpectedPlanConfigVersion int                        `json:"expected_plan_config_version"`
		CurrentConfigHash         string                     `json:"current_config_hash"`
		CurrentMarketHash         string                     `json:"current_market_hash"`
		Before                    ImprovementParameterValues `json:"before"`
		After                     ImprovementParameterValues `json:"after"`
		PreviewExpiresAt          int64                      `json:"preview_expires_at"`
	}{runID, proposalID, version, configHash, marketHash, before, after, expiresAt}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *FirePlanImprovementService) SetClockForTest(now func() time.Time) { s.now = now }

func (s *FirePlanImprovementService) ValidateCandidateSnapshot(
	ctx context.Context, runID, proposalID string,
) (string, error) {
	_, frozen, _, proposal, err := s.loadApplicable(ctx, runID, proposalID)
	if err != nil {
		return "", err
	}
	snapshot, err := improvement.ApplyAdjustments(frozen.SourceSnapshot, proposalAdjustments(proposal))
	if err != nil {
		return "", fmt.Errorf("apply candidate adjustments: %w", err)
	}
	snapshot.ConfigHash = proposal.CandidateConfigHash
	hash, err := simulation.HashInput(&snapshot)
	if err != nil {
		return "", fmt.Errorf("hash candidate snapshot: %w", err)
	}
	if hash != proposal.CandidateSnapshotHash {
		return "", errCandidateSnapshotMismatch
	}
	return hash, nil
}
