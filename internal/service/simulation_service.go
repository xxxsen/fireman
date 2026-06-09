package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

// CreateSimulationRequest starts a Monte Carlo job.
type CreateSimulationRequest struct {
	PlanID         string  `json:"-"`
	IdempotencyKey string  `json:"-"`
	Runs           *int    `json:"runs,omitempty"`
	Seed           *string `json:"seed,omitempty"`
	seedInt        *int64  `json:"-"`
}

// CreateSimulationResponse returns the enqueued job.
type CreateSimulationResponse struct {
	JobID  string `json:"job_id"`
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// SimulationService orchestrates simulation jobs and results.
type SimulationService struct {
	sql      *sql.DB
	plans    *repository.PlanRepo
	params   *repository.ParametersRepo
	alloc    *repository.AllocationRepo
	holdings *repository.HoldingsRepo
	snapRepo *repository.SnapshotRepo
	fx       *marketdata.FXResolver
	jobs     *repository.JobRepo
	sims     *repository.SimulationRepo
	hash     *ConfigHashService
}

func NewSimulationService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	holdings *repository.HoldingsRepo,
	snapRepo *repository.SnapshotRepo,
	inst *repository.InstrumentRepo,
	market *repository.MarketDataRepo,
	jobs *repository.JobRepo,
	sims *repository.SimulationRepo,
	hash *ConfigHashService,
) *SimulationService {
	return &SimulationService{
		sql: sqlDB, plans: plans, params: params, alloc: alloc, holdings: holdings,
		snapRepo: snapRepo,
		fx:       marketdata.NewFXResolver(inst, market),
		jobs:     jobs, sims: sims, hash: hash,
	}
}

// BuildInputSnapshot freezes the current plan configuration for analysis jobs.
func (s *SimulationService) BuildInputSnapshot(ctx context.Context, planID string, runs *int, seed *string) (*simulation.InputSnapshot, string, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, "", newErr("plan_not_found", "plan not found", nil)
		}
		return nil, "", err
	}
	parsed, err := ParseSeedString(seed)
	if err != nil {
		return nil, "", newErr("parameters_invalid", err.Error(), nil)
	}
	return s.buildInputSnapshot(ctx, plan, CreateSimulationRequest{
		PlanID: planID, Runs: runs, Seed: seed, seedInt: parsed,
	})
}

func (s *SimulationService) Create(ctx context.Context, req CreateSimulationRequest) (CreateSimulationResponse, error) {
	plan, err := s.plans.GetByID(ctx, req.PlanID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return CreateSimulationResponse{}, newErr("plan_not_found", "plan not found", nil)
		}
		return CreateSimulationResponse{}, err
	}

	if req.Seed != nil {
		parsed, err := ParseSeedString(req.Seed)
		if err != nil {
			return CreateSimulationResponse{}, newErr("parameters_invalid", err.Error(), nil)
		}
		req.seedInt = parsed
	}

	snap, inputHash, err := s.buildInputSnapshot(ctx, plan, req)
	if err != nil {
		return CreateSimulationResponse{}, err
	}

	if req.IdempotencyKey != "" {
		existing, storedHash, err := s.jobs.FindIdempotency(ctx, req.PlanID, repository.JobTypeSimulation, req.IdempotencyKey)
		if err == nil {
			if storedHash != inputHash {
				return CreateSimulationResponse{}, newErr("idempotency_conflict", "idempotency key reused with different input", nil)
			}
			run, _ := s.sims.GetByJobID(ctx, existing.ID)
			return CreateSimulationResponse{JobID: existing.ID, RunID: run.ID, Status: existing.Status}, nil
		}
		if !errors.Is(err, repository.ErrJobNotFound) {
			return CreateSimulationResponse{}, err
		}
	}

	jobID := "job_" + uuid.New().String()
	runID := "simrun_" + uuid.New().String()
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return CreateSimulationResponse{}, err
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.jobs.Create(ctx, tx, repository.Job{
			ID: jobID, PlanID: req.PlanID, Type: repository.JobTypeSimulation,
			Status: repository.JobStatusQueued, InputHash: inputHash,
			ProgressTotal: snap.Parameters.SimulationRuns,
		}); err != nil {
			return err
		}
		if err := s.sims.CreatePending(ctx, tx, repository.SimulationRun{
			ID: runID, JobID: jobID, PlanID: req.PlanID, InputHash: inputHash,
			InputSnapshotJSON: string(snapJSON), MarketSnapshotHash: snap.MarketSnapshotHash,
			EngineVersion: simulation.EngineVersion, Runs: snap.Parameters.SimulationRuns,
			Seed: snap.RootSeed(), HorizonMonths: snap.HorizonMonths(),
		}); err != nil {
			return err
		}
		if req.IdempotencyKey != "" {
			return s.jobs.SaveIdempotency(ctx, tx, req.PlanID, repository.JobTypeSimulation, req.IdempotencyKey, jobID, inputHash)
		}
		return nil
	})
	if err != nil {
		return CreateSimulationResponse{}, err
	}
	return CreateSimulationResponse{JobID: jobID, RunID: runID, Status: repository.JobStatusQueued}, nil
}

func (s *SimulationService) ListByPlan(ctx context.Context, planID string) ([]SimulationRunView, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, err
	}
	runs, err := s.sims.ListByPlan(ctx, planID, 50)
	if err != nil {
		return nil, err
	}
	currentHash, _ := s.hash.Compute(ctx, planID)
	out := make([]SimulationRunView, len(runs))
	for i, r := range runs {
		out[i] = toRunView(r, currentHash)
	}
	return out, nil
}

func (s *SimulationService) GetRun(ctx context.Context, runID string) (SimulationRunView, error) {
	run, err := s.sims.GetByID(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrSimulationNotFound) {
			return SimulationRunView{}, newErr("simulation_not_found", "simulation not found", nil)
		}
		return SimulationRunView{}, err
	}
	currentHash, _ := s.hash.Compute(ctx, run.PlanID)
	return toRunView(run, currentHash), nil
}

func (s *SimulationService) ListPaths(ctx context.Context, runID string) ([]PathIndexView, error) {
	if _, err := s.sims.GetByID(ctx, runID); err != nil {
		if errors.Is(err, repository.ErrSimulationNotFound) {
			return nil, newErr("simulation_not_found", "simulation not found", nil)
		}
		return nil, err
	}
	rows, err := s.sims.ListPathIndex(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := make([]PathIndexView, len(rows))
	for i, r := range rows {
		out[i] = PathIndexToView(r)
	}
	return out, nil
}

func (s *SimulationService) GetPathDetail(ctx context.Context, runID string, pathNo int) (*simulation.PathDetail, error) {
	run, err := s.sims.GetByID(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrSimulationNotFound) {
			return nil, newErr("simulation_not_found", "simulation not found", nil)
		}
		return nil, err
	}
	if _, err := s.sims.GetPathIndex(ctx, runID, pathNo); err != nil {
		if errors.Is(err, repository.ErrSimulationNotFound) {
			return nil, newErr("path_not_found", "simulation path not found", nil)
		}
		return nil, err
	}
	var snap simulation.InputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snap); err != nil {
		return nil, err
	}
	return simulation.RegeneratePathDetail(&snap, pathNo), nil
}

// AssetParticipationView summarizes which complete years each asset used in simulation.
type AssetParticipationView struct {
	HoldingID     string `json:"holding_id"`
	InstrumentID  string `json:"instrument_id"`
	CompleteYears []int  `json:"complete_years"`
}

// SimulationRunView is the API view of a simulation run.
type SimulationRunView struct {
	ID                 string                   `json:"id"`
	JobID              string                   `json:"job_id"`
	PlanID             string                   `json:"plan_id"`
	InputHash          string                   `json:"input_hash"`
	CurrentConfigHash  string                   `json:"current_config_hash"`
	ResultStale        bool                     `json:"result_stale"`
	MarketSnapshotHash string                   `json:"market_snapshot_hash"`
	EngineVersion      string                   `json:"engine_version"`
	Runs               int                      `json:"runs"`
	Seed               string                   `json:"seed"`
	HorizonMonths      int                      `json:"horizon_months"`
	SuccessCount       int                      `json:"success_count"`
	FailureCount       int                      `json:"failure_count"`
	Summary            json.RawMessage          `json:"summary_json"`
	AssetParticipation []AssetParticipationView `json:"asset_participation,omitempty"`
	CreatedAt          int64                    `json:"created_at"`
}

func toRunView(r repository.SimulationRun, currentHash string) SimulationRunView {
	stale := false
	var snap simulation.InputSnapshot
	var participation []AssetParticipationView
	if err := json.Unmarshal([]byte(r.InputSnapshotJSON), &snap); err == nil {
		stale = currentHash != "" && snap.ConfigHash != currentHash
		for _, a := range snap.Assets {
			years := make([]int, 0, len(a.Years))
			for _, y := range a.Years {
				years = append(years, y.Year)
			}
			participation = append(participation, AssetParticipationView{
				HoldingID: a.HoldingID, InstrumentID: a.InstrumentID, CompleteYears: years,
			})
		}
	}
	return SimulationRunView{
		ID: r.ID, JobID: r.JobID, PlanID: r.PlanID, InputHash: r.InputHash,
		CurrentConfigHash: currentHash, ResultStale: stale,
		MarketSnapshotHash: r.MarketSnapshotHash, EngineVersion: r.EngineVersion,
		Runs: r.Runs, Seed: strconv.FormatInt(r.Seed, 10), HorizonMonths: r.HorizonMonths,
		SuccessCount: r.SuccessCount, FailureCount: r.FailureCount,
		Summary: r.SummaryJSON, AssetParticipation: participation, CreatedAt: r.CreatedAt,
	}
}

func (s *SimulationService) buildInputSnapshot(ctx context.Context, plan repository.Plan, req CreateSimulationRequest) (*simulation.InputSnapshot, string, error) {
	params, err := s.params.Get(ctx, plan.ID)
	if err != nil {
		return nil, "", err
	}
	if req.Runs != nil {
		params.SimulationRuns = *req.Runs
	}
	if req.seedInt != nil {
		params.Seed = req.seedInt
	} else if req.Seed != nil {
		parsed, err := ParseSeedString(req.Seed)
		if err != nil {
			return nil, "", newErr("parameters_invalid", err.Error(), nil)
		}
		params.Seed = parsed
	}
	if err := validateSimulationReady(params); err != nil {
		return nil, "", newErr("simulation_input_invalid", err.Error(), nil)
	}

	alloc, err := s.alloc.Get(ctx, plan.ID)
	if err != nil {
		return nil, "", err
	}
	holds, err := s.holdings.ListByPlan(ctx, plan.ID)
	if err != nil {
		return nil, "", err
	}
	da := toDomainAllocation(alloc)
	dh := holdingsToDomain(holds)
	checks := domain.ValidateAllWeights(da, dh)
	if !checks.Passed {
		return nil, "", newErr("plan_weights_invalid", "plan weights are incomplete or invalid", map[string]any{"checks": checks.Checks})
	}

	enabledSum := int64(0)
	enabledCount := 0
	for _, h := range holds {
		if h.Enabled {
			enabledSum += h.CurrentAmountMinor
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return nil, "", newErr("simulation_input_invalid", "at least one enabled holding is required", nil)
	}
	if abs64(enabledSum-params.TotalAssetsMinor) > 100 {
		return nil, "", newErr("simulation_input_invalid", "total assets must match enabled holdings within 1 CNY", map[string]any{
			"total_assets_minor": params.TotalAssetsMinor, "holdings_sum_minor": enabledSum,
		})
	}

	flows, err := s.params.ListCashFlows(ctx, plan.ID)
	if err != nil {
		return nil, "", err
	}

	lines := domain.ComputeHoldingTargets(da, dh, holdingMeta(holds), params.TotalAssetsMinor)
	fxCache := make(map[string]marketdata.SnapshotMetrics)
	assets := make([]simulation.SnapshotAsset, 0, len(lines))
	for _, line := range lines {
		if !line.Enabled {
			continue
		}
		snap, err := s.snapRepo.GetByID(ctx, line.SimulationSnapshotID)
		if err != nil {
			return nil, "", newErr("snapshot_not_found", "simulation snapshot missing for holding", map[string]any{"holding_id": line.HoldingID})
		}
		if snap.SourceMode != "system_cash" && snap.CompleteYearCount < 2 {
			return nil, "", newErr("instrument_insufficient_history", "holding snapshot needs at least 2 complete years", map[string]any{"holding_id": line.HoldingID})
		}
		years := make([]simulation.SnapshotYear, len(snap.Years))
		for i, y := range snap.Years {
			years[i] = simulation.SnapshotYear{
				Year: y.Year, AnnualReturn: y.AnnualReturn, StartDate: y.StartDate,
				EndDate: y.EndDate, Observations: y.Observations,
			}
		}
		inst, err := s.holdings.GetInstrument(ctx, line.InstrumentID)
		currency := plan.BaseCurrency
		if err == nil {
			currency = inst.Currency
		}
		isCash := snap.SourceMode == "system_cash" || line.AssetClass == domain.AssetClassCash
		sa := simulation.SnapshotAsset{
			HoldingID: line.HoldingID, InstrumentID: line.InstrumentID, SnapshotID: line.SimulationSnapshotID,
			Currency: currency, AssetClass: line.AssetClass, IsCash: isCash,
			InitialMinor: line.CurrentAmountMinor, TargetWeight: line.PortfolioTargetWeight,
			ModeledAnnualReturn: snap.ModeledAnnualReturn, AnnualVolatility: snap.AnnualVolatility,
			MaxDrawdown: snap.MaxDrawdown, FeeTreatment: snap.FeeTreatment, ExpenseRatio: snap.ExpenseRatio,
			SourceHash: snap.SourceHash, Years: years,
		}
		if currency != plan.BaseCurrency {
			fxMetrics, ok := fxCache[currency]
			if !ok {
				var err error
				fxMetrics, err = s.fx.Metrics(ctx, currency, plan.BaseCurrency, plan.ValuationDate)
				if err != nil {
					return nil, "", newErr("fx_snapshot_missing", "FX data unavailable for foreign holding", map[string]any{
						"holding_id": line.HoldingID, "currency": currency, "error": err.Error(),
					})
				}
				if fxMetrics.CompleteYearCount < 2 || fxMetrics.QualityStatus != "available" {
					return nil, "", newErr("fx_insufficient_history", "FX snapshot needs at least 2 complete years", map[string]any{
						"holding_id": line.HoldingID, "currency": currency,
					})
				}
				fxCache[currency] = fxMetrics
			}
			sa.FXSnapshotID = fxMetrics.SourceHash
			sa.FXModeledReturn = fxMetrics.ModeledAnnualReturn
			sa.FXAnnualVolatility = fxMetrics.AnnualVolatility
		}
		assets = append(assets, sa)
	}

	cfSnap := make([]simulation.SnapshotCashFlow, 0, len(flows))
	for _, f := range flows {
		cfSnap = append(cfSnap, simulation.SnapshotCashFlow{
			ID: f.ID, Kind: f.Kind, AmountMinor: f.AmountMinor, StartMonthOffset: f.StartMonthOffset,
			EndMonthOffset: f.EndMonthOffset, Recurrence: f.Recurrence, InflationLinked: f.InflationLinked,
			AnnualGrowthRate: f.AnnualGrowthRate, Enabled: f.Enabled,
		})
	}

	seed := params.Seed
	if seed == nil {
		v, err := randomSeed()
		if err != nil {
			return nil, "", err
		}
		seed = &v
	}

	configHash, err := s.hash.Compute(ctx, plan.ID)
	if err != nil {
		return nil, "", err
	}

	in := &simulation.InputSnapshot{
		EngineVersion:     simulation.EngineVersion,
		PlanID:            plan.ID,
		BaseCurrency:      plan.BaseCurrency,
		RandomFactorModel: "independent_student_t",
		ConfigHash:        configHash,
		Parameters: simulation.SnapshotParameters{
			CurrentAge: params.CurrentAge, RetirementAge: params.RetirementAge, EndAge: params.EndAge,
			TotalAssetsMinor: params.TotalAssetsMinor, AnnualSavingsMinor: params.AnnualSavingsMinor,
			AnnualSavingsGrowthRate: params.AnnualSavingsGrowthRate, AnnualSpendingMinor: params.AnnualSpendingMinor,
			TerminalWealthFloorMinor: params.TerminalWealthFloorMinor,
			InflationMode:            params.InflationMode, FixedInflationRate: params.FixedInflationRate,
			InflationMu: params.InflationMu, InflationPhi: params.InflationPhi, InflationSigma: params.InflationSigma,
			WithdrawalType: params.WithdrawalType, WithdrawalRate: params.WithdrawalRate,
			WithdrawalFloorRatio: params.WithdrawalFloorRatio, WithdrawalCeilingRatio: params.WithdrawalCeilingRatio,
			WithdrawalTaxRate: params.WithdrawalTaxRate, TaxableWithdrawalRatio: params.TaxableWithdrawalRatio,
			RebalanceFrequency: params.RebalanceFrequency, RebalanceThreshold: params.RebalanceThreshold,
			TransactionCostRate: params.TransactionCostRate, SimulationRuns: params.SimulationRuns,
			StudentTDf: params.StudentTDf, Seed: strconv.FormatInt(*seed, 10),
		},
		Assets:    assets,
		CashFlows: cfSnap,
	}
	in.MarketSnapshotHash = simulation.MarketHashFromAssets(assets)

	inputHash, err := simulation.HashInput(in)
	if err != nil {
		return nil, "", err
	}
	return in, inputHash, nil
}

func validateSimulationReady(p repository.PlanParameters) error {
	if err := validateParameters(p); err != nil {
		return err
	}
	denom := 1 - p.WithdrawalTaxRate*p.TaxableWithdrawalRatio
	if denom <= 0 {
		return fmt.Errorf("withdrawal tax parameters invalid: denominator must be > 0")
	}
	return nil
}

func randomSeed() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return time.Now().UnixNano(), nil
	}
	return n.Int64(), nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
