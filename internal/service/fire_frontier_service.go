package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/frontier"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	taskcore "github.com/fireman/fireman/internal/task"
	"github.com/google/uuid"
)

const (
	frontierRetentionLimit = 20
	frontierPreviewTTL     = 15 * time.Minute
)

var errFrontierSnapshotSchema = errors.New("source snapshot does not pass the current schema validator")

type FireFrontierService struct {
	db          *sql.DB
	plans       *repository.PlanRepo
	params      *repository.ParametersRepo
	sims        *repository.SimulationRepo
	tasks       *repository.WorkerTaskRepo
	coordinator *taskcore.Coordinator
	runs        *repository.FireFrontierRepo
	hash        *ConfigHashService
	simulation  *SimulationService
	now         func() time.Time
}

func NewFireFrontierService(db *sql.DB, plans *repository.PlanRepo,
	params *repository.ParametersRepo, sims *repository.SimulationRepo,
	tasks *repository.WorkerTaskRepo, coordinator *taskcore.Coordinator,
	runs *repository.FireFrontierRepo, hash *ConfigHashService,
	simulationService *SimulationService,
) *FireFrontierService {
	return &FireFrontierService{
		db: db, plans: plans, params: params, sims: sims, tasks: tasks,
		coordinator: coordinator, runs: runs, hash: hash, simulation: simulationService,
		now: time.Now,
	}
}

type FireFrontierRequest struct {
	SourceSimulationRunID    string               `json:"source_simulation_run_id"`
	FrontierType             string               `json:"frontier_type"`
	TargetSuccessProbability float64              `json:"target_success_probability"`
	EvaluationRuns           int                  `json:"evaluation_runs,omitempty"`
	RetirementAgeRange       *frontier.AgeRange   `json:"retirement_age_range,omitempty"`
	Search                   frontier.MoneySearch `json:"search"`
	IdempotencyKey           string               `json:"-"`
	RequestID                string               `json:"-"`
}

type FrontierIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type FrontierSourceSummary struct {
	ID                 string  `json:"id"`
	EngineVersion      string  `json:"engine_version"`
	Runs               int     `json:"runs"`
	EvaluationRuns     int     `json:"evaluation_runs"`
	SuccessCount       int     `json:"success_count"`
	SuccessProbability float64 `json:"success_probability"`
	SuccessWilsonLow   float64 `json:"success_wilson_low"`
	SuccessWilsonHigh  float64 `json:"success_wilson_high"`
	CreatedAt          int64   `json:"created_at"`
}

type FrontierReadiness struct {
	Ready            bool                   `json:"ready"`
	Issues           []FrontierIssue        `json:"issues"`
	Config           *frontier.Config       `json:"config,omitempty"`
	MoneyLevels      int                    `json:"money_levels"`
	AgePoints        int                    `json:"age_points"`
	EvaluationBudget int                    `json:"evaluation_budget"`
	PathMonthBudget  int64                  `json:"path_month_budget"`
	SourceBaseline   *FrontierSourceSummary `json:"source_baseline,omitempty"`
}

type CreateFrontierResponse struct {
	RunID  string `json:"run_id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Reused bool   `json:"reused"`
}

type FrontierRunView struct {
	ID                    string                              `json:"id"`
	TaskID                string                              `json:"task_id"`
	PlanID                string                              `json:"plan_id"`
	SourceSimulationRunID string                              `json:"source_simulation_run_id"`
	InputHash             string                              `json:"input_hash"`
	AlgorithmVersion      string                              `json:"algorithm_version"`
	FrontierType          string                              `json:"frontier_type"`
	SourceEngineVersion   string                              `json:"source_engine_version"`
	SourceConfigHash      string                              `json:"source_config_hash"`
	SourceMarketHash      string                              `json:"source_market_hash"`
	EvaluationRuns        int                                 `json:"evaluation_runs"`
	Config                frontier.Config                     `json:"config"`
	Result                *frontier.Result                    `json:"result,omitempty"`
	Status                string                              `json:"status"`
	ProgressCurrent       int                                 `json:"progress_current"`
	ProgressTotal         int                                 `json:"progress_total"`
	Phase                 string                              `json:"phase"`
	AttemptCount          int                                 `json:"attempt_count"`
	ErrorCode             string                              `json:"error_code,omitempty"`
	ErrorMessage          string                              `json:"error_message,omitempty"`
	CreatedAt             int64                               `json:"created_at"`
	CompletedAt           *int64                              `json:"completed_at,omitempty"`
	SourceAvailable       bool                                `json:"source_available"`
	CurrentPlanChanged    bool                                `json:"current_plan_changed"`
	FrozenBasis           FrontierFrozenBasis                 `json:"frozen_basis"`
	Application           *repository.FireFrontierApplication `json:"application,omitempty"`
}

// FrontierFrozenBasis is the human-readable subset of the immutable source
// snapshot needed to explain exactly what a historical frontier calculated.
// It deliberately comes from input_snapshot_json, never mutable plan tables.
type FrontierFrozenBasis struct {
	BaseCurrency                string  `json:"base_currency"`
	CurrentAge                  int     `json:"current_age"`
	RetirementAge               int     `json:"retirement_age"`
	EndAge                      int     `json:"end_age"`
	TotalAssetsMinor            int64   `json:"total_assets_minor"`
	AnnualSavingsMinor          int64   `json:"annual_savings_minor"`
	AnnualSavingsGrowthRate     float64 `json:"annual_savings_growth_rate"`
	AnnualSpendingMinor         int64   `json:"annual_spending_minor"`
	AnnualRetirementIncomeMinor int64   `json:"annual_retirement_income_minor"`
	RetirementIncomeGrowthRate  float64 `json:"annual_retirement_income_growth_rate"`
	InflationMode               string  `json:"inflation_mode"`
	WithdrawalType              string  `json:"withdrawal_type"`
	RebalanceFrequency          string  `json:"rebalance_frequency"`
	AssetCount                  int     `json:"asset_count"`
	RandomFactorModel           string  `json:"random_factor_model"`
	ReturnAssumptionMode        string  `json:"return_assumption_mode"`
	ReturnAssumptionScenario    string  `json:"return_assumption_scenario"`
	SourceSimulationRuns        int     `json:"source_simulation_runs"`
	Seed                        string  `json:"seed"`
	AssetScalingBasis           string  `json:"asset_scaling_basis"`
}

type FrontierRunSummary struct {
	ID                    string  `json:"id"`
	TaskID                string  `json:"task_id"`
	SourceSimulationRunID string  `json:"source_simulation_run_id"`
	FrontierType          string  `json:"frontier_type"`
	TargetProbability     float64 `json:"target_probability"`
	EvaluationRuns        int     `json:"evaluation_runs"`
	Status                string  `json:"status"`
	ProgressCurrent       int     `json:"progress_current"`
	ProgressTotal         int     `json:"progress_total"`
	Phase                 string  `json:"phase"`
	ErrorCode             string  `json:"error_code,omitempty"`
	ErrorMessage          string  `json:"error_message,omitempty"`
	CreatedAt             int64   `json:"created_at"`
	CompletedAt           *int64  `json:"completed_at,omitempty"`
}

type preparedFrontier struct {
	run         repository.SimulationRun
	snapshot    simulation.InputSnapshot
	configInput domain.ConfigHashInput
	config      frontier.Config
	baseline    FrontierSourceSummary
	configRaw   []byte
	frozenRaw   []byte
	inputHash   string
}

func (s *FireFrontierService) Readiness(ctx context.Context, planID string,
	req FireFrontierRequest,
) (FrontierReadiness, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return FrontierReadiness{}, newErr("plan_not_found", "plan not found", nil)
		}
		return FrontierReadiness{}, wrapRepo("load frontier plan", err)
	}
	prepared, err := s.prepare(ctx, planID, req)
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			return FrontierReadiness{Issues: []FrontierIssue{{Code: appErr.Code, Message: appErr.Message}}}, nil
		}
		return FrontierReadiness{}, err
	}
	readiness := FrontierReadiness{
		Ready: true, Config: &prepared.config, MoneyLevels: prepared.config.MoneyLevels,
		AgePoints: prepared.config.AgePoints, EvaluationBudget: prepared.config.EvaluationBudget,
		PathMonthBudget: prepared.config.PathMonthBudget, SourceBaseline: &prepared.baseline,
	}
	return readiness, nil
}

func (s *FireFrontierService) Create(ctx context.Context, planID string,
	req FireFrontierRequest,
) (CreateFrontierResponse, error) {
	prepared, err := s.prepare(ctx, planID, req)
	if err != nil {
		return CreateFrontierResponse{}, err
	}
	if response, found, findErr := s.findIdempotentFrontier(ctx, planID, req, prepared.inputHash); findErr != nil {
		return CreateFrontierResponse{}, findErr
	} else if found {
		return response, nil
	}
	record := repository.FireFrontierRun{
		ID: "ffr_" + uuid.NewString(), TaskID: "task_" + uuid.NewString(), PlanID: planID,
		SourceSimulationRunID: prepared.run.ID, InputHash: prepared.inputHash,
		AlgorithmVersion: frontier.AlgorithmVersion, FrontierType: prepared.config.FrontierType,
		SourceEngineVersion: prepared.snapshot.EngineVersion,
		SourceConfigHash:    prepared.snapshot.ConfigHash,
		SourceMarketHash:    prepared.snapshot.MarketSnapshotHash,
		EvaluationRuns:      prepared.config.EvaluationRuns, ConfigJSON: string(prepared.configRaw),
		InputSnapshotJSON: string(prepared.frozenRaw),
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
			Type: repository.WorkerTaskTypeFireFrontier, Status: repository.WorkerTaskStatusPending,
			ScopeType: "plan", ScopeID: planID,
			DedupeKey: frontierAdmissionDedupeKey(planID, req.IdempotencyKey, record.TaskID),
			InputHash: prepared.inputHash, PayloadJSON: string(payload),
			ProgressTotal: prepared.config.EvaluationBudget, Phase: "validating",
		}
		var createErr error
		bound, reused, createErr = createOrReuseActiveTaskTx(ctx, tx, s.tasks, s.coordinator, task, func() error {
			if err := s.runs.CreateTx(ctx, tx, &record); err != nil {
				return fmt.Errorf("create frontier run: %w", err)
			}
			return s.runs.PruneTx(ctx, tx, planID, frontierRetentionLimit)
		})
		if createErr != nil {
			return mapFrontierAdmissionError(req.IdempotencyKey, createErr)
		}
		if req.IdempotencyKey != "" && !reused {
			return s.tasks.SaveIdempotency(ctx, tx, "plan", planID,
				repository.WorkerTaskTypeFireFrontier, req.IdempotencyKey, bound.ID, prepared.inputHash)
		}
		return nil
	})
	if err != nil {
		return CreateFrontierResponse{}, wrapRepo("create frontier transaction", err)
	}
	if reused {
		existing, getErr := s.runs.GetByTaskID(ctx, bound.ID)
		if getErr != nil {
			return CreateFrontierResponse{}, wrapRepo("load active frontier", getErr)
		}
		logFrontierAdmission(existing, true, req.RequestID)
		return createFrontierResponse(existing, true), nil
	}
	record.TaskStatus = repository.WorkerTaskStatusPending
	logFrontierAdmission(record, false, req.RequestID)
	return createFrontierResponse(record, false), nil
}

func (s *FireFrontierService) findIdempotentFrontier(
	ctx context.Context,
	planID string,
	req FireFrontierRequest,
	inputHash string,
) (CreateFrontierResponse, bool, error) {
	if req.IdempotencyKey == "" {
		return CreateFrontierResponse{}, false, nil
	}
	task, found, err := findExistingIdempotentTask(
		ctx, s.tasks, planID, repository.WorkerTaskTypeFireFrontier,
		req.IdempotencyKey, inputHash, "find frontier idempotency",
	)
	if err != nil || !found {
		return CreateFrontierResponse{}, false, err
	}
	existing, err := s.runs.GetByTaskID(ctx, task.ID)
	if err != nil {
		return CreateFrontierResponse{}, false, wrapRepo("load idempotent frontier", err)
	}
	logFrontierAdmission(existing, true, req.RequestID)
	return createFrontierResponse(existing, true), true, nil
}

func (s *FireFrontierService) List(ctx context.Context, planID string, limit, offset int) (
	[]FrontierRunSummary, int, error,
) {
	records, total, err := s.runs.ListByPlan(ctx, planID, limit, offset)
	if err != nil {
		return nil, 0, wrapRepo("list frontier runs", err)
	}
	out := make([]FrontierRunSummary, len(records))
	for i, record := range records {
		var config frontier.Config
		if err := json.Unmarshal([]byte(record.ConfigJSON), &config); err != nil {
			return nil, 0, wrapRepo("decode frontier config", err)
		}
		out[i] = FrontierRunSummary{
			ID: record.ID, TaskID: record.TaskID, SourceSimulationRunID: record.SourceSimulationRunID,
			FrontierType: record.FrontierType, TargetProbability: config.TargetSuccessProbability,
			EvaluationRuns: record.EvaluationRuns, Status: record.TaskStatus,
			ProgressCurrent: record.TaskProgressCurrent, ProgressTotal: record.TaskProgressTotal,
			Phase: record.TaskPhase, ErrorCode: record.TaskErrorCode, ErrorMessage: record.TaskErrorMessage,
			CreatedAt: record.CreatedAt, CompletedAt: record.CompletedAt,
		}
	}
	return out, total, nil
}

func (s *FireFrontierService) Get(ctx context.Context, runID string) (FrontierRunView, error) {
	record, err := s.runs.GetByID(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrFireFrontierNotFound) {
			return FrontierRunView{}, newErr("frontier_run_not_found", "frontier run not found", nil)
		}
		return FrontierRunView{}, wrapRepo("get frontier run", err)
	}
	return s.toView(ctx, record)
}

func (s *FireFrontierService) toView(ctx context.Context,
	record repository.FireFrontierRun,
) (FrontierRunView, error) {
	var config frontier.Config
	if err := json.Unmarshal([]byte(record.ConfigJSON), &config); err != nil {
		return FrontierRunView{}, wrapRepo("decode frontier config", err)
	}
	var frozen frontier.FrozenInput
	if err := json.Unmarshal([]byte(record.InputSnapshotJSON), &frozen); err != nil {
		return FrontierRunView{}, wrapRepo("decode frozen frontier basis", err)
	}
	view := FrontierRunView{
		ID: record.ID, TaskID: record.TaskID, PlanID: record.PlanID,
		SourceSimulationRunID: record.SourceSimulationRunID, InputHash: record.InputHash,
		AlgorithmVersion: record.AlgorithmVersion, FrontierType: record.FrontierType,
		SourceEngineVersion: record.SourceEngineVersion, SourceConfigHash: record.SourceConfigHash,
		SourceMarketHash: record.SourceMarketHash, EvaluationRuns: record.EvaluationRuns,
		Config: config, Status: record.TaskStatus, ProgressCurrent: record.TaskProgressCurrent,
		ProgressTotal: record.TaskProgressTotal, Phase: record.TaskPhase,
		AttemptCount: record.TaskAttemptCount, ErrorCode: record.TaskErrorCode,
		ErrorMessage: record.TaskErrorMessage, CreatedAt: record.CreatedAt, CompletedAt: record.CompletedAt,
		FrozenBasis: frozenBasisFromInput(frozen.SourceSnapshot),
	}
	if record.TaskStatus == repository.WorkerTaskStatusComplete {
		var result frontier.Result
		if err := json.Unmarshal(record.ResultJSON, &result); err != nil {
			return FrontierRunView{}, wrapRepo("decode frontier result", err)
		}
		view.Result = &result
	}
	if _, err := s.sims.GetByID(ctx, record.SourceSimulationRunID); err == nil {
		view.SourceAvailable = true
	} else if !errors.Is(err, repository.ErrSimulationNotFound) {
		return FrontierRunView{}, wrapRepo("check frontier source availability", err)
	}
	configInput, err := s.hash.SnapshotReadOnly(ctx, record.PlanID)
	if err == nil {
		currentHash, hashErr := domain.ComputeConfigHash(configInput)
		view.CurrentPlanChanged = hashErr != nil || currentHash != record.SourceConfigHash
	} else {
		view.CurrentPlanChanged = true
	}
	if application, err := s.runs.GetApplication(ctx, record.ID); err == nil {
		view.Application = &application
	} else if !errors.Is(err, repository.ErrFireFrontierNotFound) {
		return FrontierRunView{}, wrapRepo("load frontier application", err)
	}
	return view, nil
}

func frozenBasisFromInput(snapshot simulation.InputSnapshot) FrontierFrozenBasis {
	p := snapshot.Parameters
	scalingBasis := "source_amount_proportions"
	if p.TotalAssetsMinor == 0 {
		scalingBasis = "frozen_target_weights"
	}
	return FrontierFrozenBasis{
		BaseCurrency: snapshot.BaseCurrency, CurrentAge: p.CurrentAge,
		RetirementAge: p.RetirementAge, EndAge: p.EndAge,
		TotalAssetsMinor: p.TotalAssetsMinor, AnnualSavingsMinor: p.AnnualSavingsMinor,
		AnnualSavingsGrowthRate:     p.AnnualSavingsGrowthRate,
		AnnualSpendingMinor:         p.AnnualSpendingMinor,
		AnnualRetirementIncomeMinor: p.AnnualRetirementIncomeMinor,
		RetirementIncomeGrowthRate:  p.AnnualRetirementIncomeGrowthRate,
		InflationMode:               p.InflationMode, WithdrawalType: p.WithdrawalType,
		RebalanceFrequency: p.RebalanceFrequency, AssetCount: len(snapshot.Assets),
		RandomFactorModel:        snapshot.RandomFactorModel,
		ReturnAssumptionMode:     snapshot.ReturnAssumptionMode,
		ReturnAssumptionScenario: snapshot.ReturnAssumptionScenario,
		SourceSimulationRuns:     p.SimulationRuns, Seed: p.Seed, AssetScalingBasis: scalingBasis,
	}
}

//nolint:funlen,gocognit,gocyclo // Source eligibility is an ordered audit gate shared by readiness/create.
func (s *FireFrontierService) prepare(ctx context.Context, planID string,
	req FireFrontierRequest,
) (preparedFrontier, error) {
	if req.SourceSimulationRunID == "" {
		return preparedFrontier{}, newErr("frontier_source_not_found", "source simulation not found", nil)
	}
	run, err := s.sims.GetByID(ctx, req.SourceSimulationRunID)
	if err != nil || run.PlanID != planID {
		return preparedFrontier{}, newErr("frontier_source_not_found", "source simulation not found", nil)
	}
	if run.TaskStatus != repository.WorkerTaskStatusComplete || run.Runs < 1000 {
		return preparedFrontier{}, newErr("frontier_source_incomplete", "source simulation is incomplete", nil)
	}
	var snapshot simulation.InputSnapshot
	if json.Unmarshal([]byte(run.InputSnapshotJSON), &snapshot) != nil || snapshot.PlanID != planID ||
		snapshot.Parameters.SimulationRuns != run.Runs || snapshot.MarketSnapshotHash != run.MarketSnapshotHash ||
		snapshot.HorizonMonths() != run.HorizonMonths || snapshot.RootSeed() != run.Seed {
		return preparedFrontier{}, newErr("frontier_source_incomplete", "source snapshot is invalid", nil)
	}
	snapshotInputHash, err := simulation.HashInput(&snapshot)
	if err != nil || snapshotInputHash != run.InputHash {
		return preparedFrontier{}, newErr("frontier_source_incomplete",
			"source snapshot does not match its input hash", nil)
	}
	if snapshot.EngineVersion != simulation.EngineVersion || run.EngineVersion != simulation.EngineVersion {
		return preparedFrontier{}, newErr("frontier_source_stale", "source simulation engine has changed", nil)
	}
	if err := validateFrontierSnapshot(snapshot); err != nil {
		return preparedFrontier{}, newErr("frontier_source_incomplete", err.Error(), nil)
	}
	requireWeights := req.FrontierType == frontier.TypeRequiredCurrentAssets ||
		req.FrontierType == frontier.TypeCoastRequiredAssets
	if err := frontier.ValidateSourceAssets(snapshot, requireWeights); err != nil {
		return preparedFrontier{}, newErr("frontier_source_incomplete", err.Error(), nil)
	}
	configInput, err := s.hash.SnapshotReadOnly(ctx, planID)
	if err != nil {
		return preparedFrontier{}, wrapRepo("build current frontier config input", err)
	}
	currentConfigHash, err := domain.ComputeConfigHash(configInput)
	if err != nil || currentConfigHash != snapshot.ConfigHash || run.InputSnapshotJSON == "" {
		return preparedFrontier{}, newErr("frontier_source_stale", "source simulation no longer matches the plan", nil)
	}
	if err := frontier.ValidateConfigAssets(snapshot, configInput); err != nil {
		return preparedFrontier{}, newErr("frontier_source_incomplete", err.Error(), nil)
	}
	marketHash, err := s.simulation.CurrentMarketSnapshotHashReadOnly(ctx, planID, snapshot)
	if err != nil || marketHash != snapshot.MarketSnapshotHash {
		return preparedFrontier{}, newErr("frontier_source_market_changed", "source market inputs have changed", nil)
	}
	paths, err := s.sims.ListPathIndex(ctx, run.ID)
	if err != nil || len(paths) != run.Runs {
		return preparedFrontier{}, newErr("frontier_source_incomplete", "source path index is incomplete", nil)
	}
	successes := 0
	for i := range paths {
		if paths[i].PathNo != i {
			return preparedFrontier{}, newErr("frontier_source_incomplete", "source path index is not contiguous", nil)
		}
		if paths[i].Succeeded {
			successes++
		}
	}
	if successes != run.SuccessCount || run.SuccessCount+run.FailureCount != run.Runs {
		return preparedFrontier{}, newErr("frontier_source_incomplete", "source path aggregates are inconsistent", nil)
	}
	config, err := frontier.Normalize(req.FrontierType, req.TargetSuccessProbability, req.EvaluationRuns,
		req.RetirementAgeRange, req.Search, snapshot.Parameters.CurrentAge, snapshot.Parameters.EndAge,
		run.Runs, snapshot.HorizonMonths())
	if err != nil {
		return preparedFrontier{}, mapFrontierConfigError(err)
	}
	baseline := frontierSourceBaseline(run, paths[:config.EvaluationRuns])
	configRaw, err := json.Marshal(config)
	if err != nil {
		return preparedFrontier{}, err
	}
	configInputRaw, err := json.Marshal(configInput)
	if err != nil {
		return preparedFrontier{}, err
	}
	frozen := frontier.FrozenInput{
		SourceSnapshot: snapshot, Config: config, ConfigHashInputJSON: configInputRaw,
	}
	frozenRaw, err := json.Marshal(frozen)
	if err != nil {
		return preparedFrontier{}, err
	}
	inputHash, err := frontier.HashFrozenIdentity(run.ID, frozen)
	if err != nil {
		return preparedFrontier{}, fmt.Errorf("hash frozen frontier identity: %w", err)
	}
	return preparedFrontier{
		run: run, snapshot: snapshot, configInput: configInput,
		config: config, baseline: baseline, configRaw: configRaw, frozenRaw: frozenRaw,
		inputHash: inputHash,
	}, nil
}

func validateFrontierSnapshot(snapshot simulation.InputSnapshot) error {
	p := snapshot.Parameters
	if p.TotalAssetsMinor < 0 || len(snapshot.Assets) == 0 {
		return errFrontierSnapshotSchema
	}
	// The frontier source contract explicitly permits zero assets so the frozen
	// target-weight fallback can be evaluated. Validate every other field through
	// the same current plan validator, substituting one minor unit only for that
	// single documented exception.
	assetsForValidation := p.TotalAssetsMinor
	if assetsForValidation == 0 {
		assetsForValidation = 1
	}
	params := repository.PlanParameters{
		PlanID: snapshot.PlanID, CurrentAge: p.CurrentAge, RetirementAge: p.RetirementAge,
		EndAge: p.EndAge, TotalAssetsMinor: assetsForValidation,
		AnnualSavingsMinor: p.AnnualSavingsMinor, AnnualSavingsGrowthRate: p.AnnualSavingsGrowthRate,
		AnnualSpendingMinor:              p.AnnualSpendingMinor,
		AnnualRetirementIncomeMinor:      p.AnnualRetirementIncomeMinor,
		AnnualRetirementIncomeGrowthRate: p.AnnualRetirementIncomeGrowthRate,
		TerminalWealthFloorMinor:         p.TerminalWealthFloorMinor,
		InflationMode:                    p.InflationMode, FixedInflationRate: p.FixedInflationRate,
		InflationMu: p.InflationMu, InflationPhi: p.InflationPhi, InflationSigma: p.InflationSigma,
		WithdrawalType: p.WithdrawalType, WithdrawalRate: p.WithdrawalRate,
		WithdrawalFloorRatio: p.WithdrawalFloorRatio, WithdrawalCeilingRatio: p.WithdrawalCeilingRatio,
		WithdrawalTaxRate: p.WithdrawalTaxRate, TaxableWithdrawalRatio: p.TaxableWithdrawalRatio,
		RebalanceFrequency: p.RebalanceFrequency, RebalanceThreshold: p.RebalanceThreshold,
		TransactionCostRate: p.TransactionCostRate, SimulationRuns: p.SimulationRuns,
		StudentTDf: p.StudentTDf,
	}
	if err := validateParameters(params); err != nil {
		return errFrontierSnapshotSchema
	}
	return nil
}

func frontierSourceBaseline(run repository.SimulationRun,
	paths []repository.PathIndexRow,
) FrontierSourceSummary {
	successes := 0
	for _, path := range paths {
		if path.Succeeded {
			successes++
		}
	}
	low, high := simulation.WilsonInterval(successes, len(paths), 1.96)
	return FrontierSourceSummary{
		ID: run.ID, EngineVersion: run.EngineVersion, Runs: run.Runs, EvaluationRuns: len(paths),
		SuccessCount: successes, SuccessProbability: roundAPIFloat(float64(successes) / float64(len(paths))),
		SuccessWilsonLow: roundAPIFloat(low), SuccessWilsonHigh: roundAPIFloat(high), CreatedAt: run.CreatedAt,
	}
}

func mapFrontierConfigError(err error) error {
	switch {
	case errors.Is(err, frontier.ErrBudgetExceeded):
		return newErr("frontier_budget_exceeded", err.Error(), nil)
	case errors.Is(err, frontier.ErrComputeBudgetExceeded):
		return newErr("frontier_compute_budget_exceeded", err.Error(), nil)
	default:
		return newErr("frontier_config_invalid", err.Error(), nil)
	}
}

func createFrontierResponse(record repository.FireFrontierRun, reused bool) CreateFrontierResponse {
	return CreateFrontierResponse{
		RunID: record.ID, TaskID: record.TaskID,
		Status: record.TaskStatus, Reused: reused,
	}
}

func logFrontierAdmission(record repository.FireFrontierRun, reused bool, requestID string) {
	slog.Info("fire_frontier_admitted", "request_id", requestID, "task_id", record.TaskID, "run_id", record.ID,
		"source_run_id", record.SourceSimulationRunID, "input_hash", record.InputHash, "reused", reused)
}

// frontierAdmissionDedupeKey prevents only concurrent replays of the same
// client request from creating two tasks. A new request gets an independent
// task even when its frozen input hash matches a historical or active run.
func frontierAdmissionDedupeKey(planID, idempotencyKey, taskID string) string {
	identity := taskID
	if idempotencyKey != "" {
		sum := sha256.Sum256([]byte(planID + "\x00" + idempotencyKey))
		identity = hex.EncodeToString(sum[:])
	}
	return repository.WorkerTaskTypeFireFrontier + "|request:" + identity
}

func mapFrontierAdmissionError(idempotencyKey string, err error) error {
	var appErr *AppError
	if idempotencyKey != "" && errors.As(err, &appErr) && appErr.Code == "task_already_active" {
		return newErr("idempotency_conflict", "idempotency key reused with different input", nil)
	}
	return err
}

func roundAPIFloat(value float64) float64 { return math.Round(value*1e10) / 1e10 }

func frontierPreviewHash(runID, pointID string, version int, configHash, marketHash string,
	before, after FrontierParameterValues, expiresAt int64,
) string {
	payload := struct {
		RunID, PointID         string
		Version                int
		ConfigHash, MarketHash string
		Before, After          FrontierParameterValues
		ExpiresAt              int64
	}{runID, pointID, version, configHash, marketHash, before, after, expiresAt}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (s *FireFrontierService) SetClockForTest(now func() time.Time) { s.now = now }
