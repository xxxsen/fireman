package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"

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

// SimulationRetentionLimit is how many Monte Carlo runs are kept per plan; older
// runs (and their attached analysis results) are pruned on each new run.
const SimulationRetentionLimit = 7

// SimulationService orchestrates simulation jobs and results.
type SimulationService struct {
	sql         *sql.DB
	plans       *repository.PlanRepo
	params      *repository.ParametersRepo
	alloc       *repository.AllocationRepo
	holdings    *repository.HoldingsRepo
	snapRepo    *repository.SnapshotRepo
	assetRepo   *repository.MarketAssetRepo
	fx          *marketdata.FXResolver
	jobs        *repository.JobRepo
	sims        *repository.SimulationRepo
	analysis    *repository.AnalysisRepo
	hash        *ConfigHashService
	assumptions *repository.AssumptionProfileRepo
	overrides   *repository.ReturnOverrideRepo
	readiness   *SimulationReadinessService
}

func NewSimulationService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	holdings *repository.HoldingsRepo,
	snapRepo *repository.SnapshotRepo,
	assetRepo *repository.MarketAssetRepo,
	inst *repository.InstrumentRepo,
	market *repository.MarketDataRepo,
	jobs *repository.JobRepo,
	sims *repository.SimulationRepo,
	analysis *repository.AnalysisRepo,
	hash *ConfigHashService,
	readiness *SimulationReadinessService,
) *SimulationService {
	return &SimulationService{
		sql: sqlDB, plans: plans, params: params, alloc: alloc, holdings: holdings,
		snapRepo:  snapRepo,
		assetRepo: assetRepo,
		fx:        marketdata.NewFXResolver(inst, market),
		jobs:      jobs, sims: sims, analysis: analysis, hash: hash,
		assumptions: repository.NewAssumptionProfileRepo(sqlDB),
		overrides:   repository.NewReturnOverrideRepo(sqlDB),
		readiness:   readiness,
	}
}

// AnalysisRunContext is a resolved Monte Carlo run used as the frozen input for
// an attached stress / sensitivity analysis.
type AnalysisRunContext struct {
	RunID     string
	Snapshot  *simulation.InputSnapshot
	InputHash string
}

// ResolveAnalysisRun returns the frozen input snapshot of the Monte Carlo run an
// attached analysis must run against. When runID is empty it falls back to the
// plan's latest run; if the plan has no run it returns a simulation_required
// business error.
func (s *SimulationService) ResolveAnalysisRun(
	ctx context.Context, planID, runID string,
) (AnalysisRunContext, error) {
	run, err := s.loadAnalysisRun(ctx, planID, runID)
	if err != nil {
		return AnalysisRunContext{}, err
	}
	var snap simulation.InputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snap); err != nil {
		return AnalysisRunContext{}, wrapRepo("decode simulation run snapshot", err)
	}
	return AnalysisRunContext{RunID: run.ID, Snapshot: &snap, InputHash: run.InputHash}, nil
}

// EnsureRunInPlan verifies a Monte Carlo run exists and belongs to the plan.
// Used by attached-analysis listing to keep results scoped to the right plan.
func (s *SimulationService) EnsureRunInPlan(ctx context.Context, planID, runID string) error {
	_, err := s.loadAnalysisRun(ctx, planID, runID)
	return err
}

// RunConfigHash returns the plan config hash captured in a run's input snapshot.
// ok is false when the run is missing or its snapshot cannot be decoded; callers
// treat that as "cannot determine staleness".
func (s *SimulationService) RunConfigHash(ctx context.Context, runID string) (string, bool) {
	if runID == "" {
		return "", false
	}
	run, err := s.sims.GetByID(ctx, runID)
	if err != nil {
		return "", false
	}
	var snap simulation.InputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snap); err != nil {
		return "", false
	}
	return snap.ConfigHash, true
}

// analysisResultStale reports whether an attached analysis (stress/sensitivity)
// is stale relative to the current plan config. It compares the config hash
// frozen in the owning Monte Carlo run's snapshot against the current plan hash;
// the run's input_hash is a different hash space and must not be compared with
// the config hash. Legacy rows without a run id are treated as not stale.
func analysisResultStale(ctx context.Context, sims *SimulationService, runID, currentHash string) bool {
	if currentHash == "" || runID == "" {
		return false
	}
	runHash, ok := sims.RunConfigHash(ctx, runID)
	if !ok || runHash == "" {
		return false
	}
	return runHash != currentHash
}

func (s *SimulationService) loadAnalysisRun(
	ctx context.Context, planID, runID string,
) (repository.SimulationRun, error) {
	if runID == "" {
		runs, err := s.sims.ListByPlan(ctx, planID, 1)
		if err != nil {
			return repository.SimulationRun{}, wrapRepo("list simulation runs for analysis", err)
		}
		if len(runs) == 0 {
			return repository.SimulationRun{}, newErr("simulation_required", "请先运行 Monte Carlo 模拟", nil)
		}
		return runs[0], nil
	}
	run, err := s.sims.GetByID(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrSimulationNotFound) {
			return repository.SimulationRun{}, newErr("simulation_not_found", "simulation not found", nil)
		}
		return repository.SimulationRun{}, wrapRepo("get simulation run for analysis", err)
	}
	if run.PlanID != planID {
		return repository.SimulationRun{}, newErr("simulation_not_found",
			"simulation run does not belong to this plan", nil)
	}
	return run, nil
}

func (s *SimulationService) Create(ctx context.Context, req CreateSimulationRequest) (CreateSimulationResponse, error) {
	plan, err := s.plans.GetByID(ctx, req.PlanID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return CreateSimulationResponse{}, newErr("plan_not_found", "plan not found", nil)
		}
		return CreateSimulationResponse{}, wrapRepo("get plan for simulation", err)
	}

	if req.Seed != nil {
		parsed, err := ParseSeedString(req.Seed)
		if err != nil && !errors.Is(err, errSeedNotProvided) {
			return CreateSimulationResponse{}, newErr("parameters_invalid", err.Error(), nil)
		}
		req.seedInt = parsed
	}

	// Simulation readiness gate: lazily-saved holdings get their snapshots
	// built now that history may have arrived; assets that still cannot build
	// a snapshot block the run with market_asset_history_missing.
	if err := s.ensureSimulationReadiness(ctx, req.PlanID); err != nil {
		return CreateSimulationResponse{}, err
	}

	snap, inputHash, err := s.buildInputSnapshot(ctx, plan, req, "")
	if err != nil {
		return CreateSimulationResponse{}, err
	}

	if resp, found, err := s.idempotentSimulation(ctx, req, inputHash); err != nil || found {
		return resp, err
	}

	return s.createSimulationRun(ctx, req, snap, inputHash)
}

func (s *SimulationService) ensureSimulationReadiness(ctx context.Context, planID string) error {
	if s.readiness == nil {
		return nil
	}
	if err := s.readiness.EnsureHoldingSnapshots(ctx, planID); err != nil {
		return err
	}
	readiness, err := s.readiness.Check(ctx, planID)
	if err != nil {
		return err
	}
	if readiness.Ready {
		return nil
	}
	return newErr("market_asset_history_missing",
		"部分计划持仓的市场资产暂时无法用于模拟",
		map[string]any{"blocking_assets": readiness.BlockingAssets})
}

func (s *SimulationService) createSimulationRun(
	ctx context.Context,
	req CreateSimulationRequest,
	snap *simulation.InputSnapshot,
	inputHash string,
) (CreateSimulationResponse, error) {
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
			return wrapRepo("create simulation job", err)
		}
		if err := s.sims.CreatePending(ctx, tx, repository.SimulationRun{
			ID: runID, JobID: jobID, PlanID: req.PlanID, InputHash: inputHash,
			InputSnapshotJSON: string(snapJSON), MarketSnapshotHash: snap.MarketSnapshotHash,
			EngineVersion: snap.EngineVersion, Runs: snap.Parameters.SimulationRuns,
			Seed: snap.RootSeed(), HorizonMonths: snap.HorizonMonths(),
		}); err != nil {
			return wrapRepo("create pending simulation run", err)
		}
		if req.IdempotencyKey != "" {
			if err := s.jobs.SaveIdempotency(ctx, tx, req.PlanID, repository.JobTypeSimulation,
				req.IdempotencyKey, jobID, inputHash); err != nil {
				return wrapRepo("save simulation idempotency", err)
			}
		}
		return s.pruneOldRuns(ctx, tx, req.PlanID)
	})
	if err != nil {
		return CreateSimulationResponse{}, wrapRepo("create simulation tx", err)
	}
	return CreateSimulationResponse{JobID: jobID, RunID: runID, Status: repository.JobStatusQueued}, nil
}

func (s *SimulationService) idempotentSimulation(
	ctx context.Context, req CreateSimulationRequest, inputHash string,
) (CreateSimulationResponse, bool, error) {
	if req.IdempotencyKey == "" {
		return CreateSimulationResponse{}, false, nil
	}
	existing, found, err := findExistingIdempotentJob(
		ctx, s.jobs, req.PlanID, repository.JobTypeSimulation, req.IdempotencyKey, inputHash,
		"find simulation idempotency",
	)
	if err != nil {
		return CreateSimulationResponse{}, false, err
	}
	if !found {
		return CreateSimulationResponse{}, false, nil
	}
	run, _ := s.sims.GetByJobID(ctx, existing.ID)
	return CreateSimulationResponse{JobID: existing.ID, RunID: run.ID, Status: existing.Status}, true, nil
}

// pruneOldRuns keeps only the newest N runs per plan and removes analysis results
// attached to the pruned runs. The just-inserted pending run is part of the count,
// so the oldest is pruned as soon as the (N+1)th run is created.
func (s *SimulationService) pruneOldRuns(ctx context.Context, tx *sql.Tx, planID string) error {
	pruned, err := s.sims.PruneByPlan(ctx, tx, planID, SimulationRetentionLimit)
	if err != nil {
		return wrapRepo("prune simulation runs", err)
	}
	if err := s.analysis.DeleteBySimulationRunIDs(ctx, tx, pruned); err != nil {
		return wrapRepo("delete analysis for pruned runs", err)
	}
	return nil
}

func (s *SimulationService) ListByPlan(ctx context.Context, planID string) ([]SimulationRunView, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("get plan for simulation list", err)
	}
	runs, err := s.sims.ListByPlan(ctx, planID, SimulationRetentionLimit)
	if err != nil {
		return nil, wrapRepo("list simulation runs", err)
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
		return SimulationRunView{}, wrapRepo("get simulation run", err)
	}
	currentHash, _ := s.hash.Compute(ctx, run.PlanID)
	return toRunView(run, currentHash), nil
}

func (s *SimulationService) ListPaths(ctx context.Context, runID string) ([]PathIndexView, error) {
	if _, err := s.sims.GetByID(ctx, runID); err != nil {
		if errors.Is(err, repository.ErrSimulationNotFound) {
			return nil, newErr("simulation_not_found", "simulation not found", nil)
		}
		return nil, wrapRepo("get simulation run for paths", err)
	}
	rows, err := s.sims.ListPathIndex(ctx, runID)
	if err != nil {
		return nil, wrapRepo("list simulation path index", err)
	}
	out := make([]PathIndexView, len(rows))
	for i, r := range rows {
		out[i] = PathIndexToView(r)
	}
	sortPathsByPercentile(out)
	return out, nil
}

var representativePercentileRank = map[string]int{
	"p00": 0, "p25": 1, "p50": 2, "p75": 3, "p95": 4,
}

// sortPathsByPercentile orders paths by representative percentile (P00<P25<P50<
// P75<P95); unknown/empty percentiles sort last, with a stable path_no tiebreak.
// This is the deterministic ordering contract for the path list API.
func sortPathsByPercentile(views []PathIndexView) {
	rank := func(v PathIndexView) int {
		if r, ok := representativePercentileRank[v.RepresentativePercentile]; ok {
			return r
		}
		return len(representativePercentileRank)
	}
	sort.SliceStable(views, func(i, j int) bool {
		ri, rj := rank(views[i]), rank(views[j])
		if ri != rj {
			return ri < rj
		}
		return views[i].PathNo < views[j].PathNo
	})
}

// PathAssetLabel exposes frozen-snapshot identity for a holding so the UI never
// has to display internal holding IDs in path weight breakdowns.
type PathAssetLabel struct {
	InstrumentName string `json:"instrument_name"`
	InstrumentCode string `json:"instrument_code"`
	AssetClass     string `json:"asset_class"`
	IsCash         bool   `json:"is_cash"`
}

// PathDetailView is the API view of a regenerated path plus per-holding labels
// derived from the run's input snapshot.
type PathDetailView struct {
	*simulation.PathDetail
	AssetLabels map[string]PathAssetLabel `json:"asset_labels"`
}

func (s *SimulationService) GetPathDetail(ctx context.Context, runID string, pathNo int) (*PathDetailView,
	error,
) {
	run, err := s.sims.GetByID(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrSimulationNotFound) {
			return nil, newErr("simulation_not_found", "simulation not found", nil)
		}
		return nil, wrapRepo("get simulation run for path detail", err)
	}
	if _, err := s.sims.GetPathIndex(ctx, runID, pathNo); err != nil {
		if errors.Is(err, repository.ErrSimulationNotFound) {
			return nil, newErr("path_not_found", "simulation path not found", nil)
		}
		return nil, wrapRepo("get simulation path index", err)
	}
	var snap simulation.InputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snap); err != nil {
		return nil, err
	}
	detail := simulation.RegeneratePathDetail(&snap, pathNo)
	labels := make(map[string]PathAssetLabel, len(snap.Assets))
	for _, a := range snap.Assets {
		labels[a.HoldingID] = PathAssetLabel{
			InstrumentName: a.InstrumentName,
			InstrumentCode: a.InstrumentCode,
			AssetClass:     a.AssetClass,
			IsCash:         a.IsCash,
		}
	}
	return &PathDetailView{PathDetail: detail, AssetLabels: labels}, nil
}

// AssetParticipationView summarizes which complete years each asset used in simulation.
type AssetParticipationView struct {
	HoldingID     string `json:"holding_id"`
	AssetKey      string `json:"asset_key"`
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
	Assumption         *RunAssumptionView       `json:"assumption,omitempty"`
	CreatedAt          int64                    `json:"created_at"`
	JobStatus          string                   `json:"job_status"`
	JobErrorCode       string                   `json:"job_error_code,omitempty"`
	JobErrorMessage    string                   `json:"job_error_message,omitempty"`
}

// RunAssumptionView exposes the frozen return-calibration and risk-model audit of
// a run so the UI can explain which profile/scenario produced the
// numbers and whether the joint risk model mainly relies on priors. It always
// reflects the run's frozen snapshot, never the plan's current (mutable) values.
type RunAssumptionView struct {
	EngineVersion        string                   `json:"engine_version"`
	RandomFactorModel    string                   `json:"random_factor_model"`
	Mode                 string                   `json:"mode"`
	Scenario             string                   `json:"scenario"`
	ProfileID            string                   `json:"profile_id"`
	ProfileVersion       int                      `json:"profile_version"`
	CorrelationPriorOnly bool                     `json:"correlation_prior_only"`
	MaxRepairDelta       float64                  `json:"max_repair_delta"`
	Assets               []RunAssetAssumptionView `json:"assets"`
}

// RunAssetAssumptionView is one holding's frozen return calibration.
type RunAssetAssumptionView struct {
	HoldingID                       string   `json:"holding_id"`
	InstrumentName                  string   `json:"instrument_name"`
	InstrumentCode                  string   `json:"instrument_code"`
	IsCash                          bool     `json:"is_cash"`
	Region                          string   `json:"region"`
	FeeTreatment                    string   `json:"fee_treatment"`
	FXTreatment                     string   `json:"fx_treatment"`
	HistoricalAnnualGeometricReturn float64  `json:"historical_annual_geometric_return"`
	ForwardAnnualGeometricReturn    float64  `json:"forward_annual_geometric_return"`
	BaseCurrencyForwardReturn       float64  `json:"base_currency_forward_return"`
	AnnualVolatilityUsed            float64  `json:"annual_volatility_used"`
	Source                          string   `json:"source"`
	SampleYears                     int      `json:"sample_years"`
	HistoricalWeight                float64  `json:"historical_weight"`
	Warnings                        []string `json:"warnings,omitempty"`
	// FX forward-calibration audit (present only for cross-currency
	// holdings). FXForwardReturn is the FX drift the engine consumes.
	HasFX              bool     `json:"has_fx"`
	FXForwardReturn    float64  `json:"fx_forward_return"`
	FXHistoricalReturn float64  `json:"fx_historical_return"`
	FXPriorReturn      float64  `json:"fx_prior_return"`
	FXAnnualVolatility float64  `json:"fx_annual_volatility"`
	FXHistoricalWeight float64  `json:"fx_historical_weight"`
	FXSource           string   `json:"fx_source"`
	FXWarnings         []string `json:"fx_warnings,omitempty"`
}

func buildRunAssumptionView(snap simulation.InputSnapshot) *RunAssumptionView {
	if len(snap.Assets) == 0 {
		return nil
	}
	view := &RunAssumptionView{
		EngineVersion:     snap.EngineVersion,
		RandomFactorModel: snap.RandomFactorModel,
		Mode:              snap.ReturnAssumptionMode,
		Scenario:          snap.ReturnAssumptionScenario,
		ProfileID:         snap.ReturnAssumptionSetID,
		ProfileVersion:    snap.ReturnAssumptionSetVersion,
	}
	for _, a := range snap.Assets {
		av := RunAssetAssumptionView{
			HoldingID: a.HoldingID, InstrumentName: a.InstrumentName, InstrumentCode: a.InstrumentCode,
			IsCash:                          a.IsCash,
			Region:                          a.Region,
			FeeTreatment:                    a.FeeTreatment,
			FXTreatment:                     effectiveFXTreatment(a, snap.BaseCurrency),
			HistoricalAnnualGeometricReturn: a.HistoricalAnnualGeometricReturn,
			ForwardAnnualGeometricReturn:    a.ForwardAnnualGeometricReturn,
			AnnualVolatilityUsed:            a.AnnualVolatilityUsed,
			Source:                          a.ReturnAssumptionSource,
			SampleYears:                     a.ReturnSampleYears,
			HistoricalWeight:                a.ReturnHistoricalWeight,
			Warnings:                        a.ReturnWarnings,
		}
		av.BaseCurrencyForwardReturn = av.ForwardAnnualGeometricReturn
		if a.FXSnapshotID != "" {
			av.HasFX = true
			av.FXForwardReturn = a.FXModeledReturn
			av.FXHistoricalReturn = a.FXHistoricalReturn
			av.FXPriorReturn = a.FXPriorReturn
			av.FXAnnualVolatility = a.FXAnnualVolatility
			av.FXHistoricalWeight = a.FXHistoricalWeight
			av.FXSource = a.FXReturnSource
			av.FXWarnings = a.FXReturnWarnings
			av.BaseCurrencyForwardReturn = simulation.CompositeBaseReturn(
				av.ForwardAnnualGeometricReturn, av.FXForwardReturn,
			)
		}
		view.Assets = append(view.Assets, av)
	}
	if snap.FactorModel != nil {
		view.CorrelationPriorOnly = len(snap.FactorModel.Audit.PriorOnlyPairs) > 0
		view.MaxRepairDelta = snap.FactorModel.Audit.MaxRepairDelta
	}
	return view
}

func effectiveFXTreatment(a simulation.SnapshotAsset, baseCurrency string) string {
	if a.FXTreatment != "" {
		return a.FXTreatment
	}
	if a.FXSnapshotID != "" && a.Currency != baseCurrency {
		return simulation.FXTreatmentSeparateFactor
	}
	return simulation.FXTreatmentNone
}

func toRunView(r repository.SimulationRun, currentHash string) SimulationRunView {
	stale := false
	var snap simulation.InputSnapshot
	var participation []AssetParticipationView
	var assumption *RunAssumptionView
	if err := json.Unmarshal([]byte(r.InputSnapshotJSON), &snap); err == nil {
		stale = currentHash != "" && snap.ConfigHash != currentHash
		for _, a := range snap.Assets {
			years := make([]int, 0, len(a.Years))
			for _, y := range a.Years {
				years = append(years, y.Year)
			}
			participation = append(participation, AssetParticipationView{
				HoldingID: a.HoldingID, AssetKey: a.AssetKey, CompleteYears: years,
			})
		}
		assumption = buildRunAssumptionView(snap)
	}
	return SimulationRunView{
		ID: r.ID, JobID: r.JobID, PlanID: r.PlanID, InputHash: r.InputHash,
		CurrentConfigHash: currentHash, ResultStale: stale,
		MarketSnapshotHash: r.MarketSnapshotHash, EngineVersion: r.EngineVersion,
		Runs: r.Runs, Seed: strconv.FormatInt(r.Seed, 10), HorizonMonths: r.HorizonMonths,
		SuccessCount: r.SuccessCount, FailureCount: r.FailureCount,
		Summary: r.SummaryJSON, AssetParticipation: participation,
		Assumption: assumption, CreatedAt: r.CreatedAt,
		JobStatus: r.JobStatus, JobErrorCode: r.JobErrorCode, JobErrorMessage: r.JobErrorMessage,
	}
}

// buildInputSnapshot freezes a plan into a simulation input. When
// scenarioOverride is non-empty the resolved assumption is forced to
// blended_prior + that scenario, which is how the scenario comparison runs the
// same frozen plan under conservative/baseline/optimistic with one shared seed.
// A normal run passes "" to keep the plan's selection.
// resolveAndBuildAssets validates the plan's allocation/holdings, resolves the
// return-assumption selection (optionally forced to a comparison scenario), and
// builds the per-asset frozen snapshot including any active asset-level overrides.
func (s *SimulationService) resolveAndBuildAssets(
	ctx context.Context, plan repository.Plan, params repository.PlanParameters, scenarioOverride string,
) ([]simulation.SnapshotAsset, resolvedAssumption, error) {
	alloc, err := s.alloc.Get(ctx, plan.ID)
	if err != nil {
		return nil, resolvedAssumption{}, wrapRepo("get plan allocation for snapshot", err)
	}
	holds, err := s.holdings.ListByPlan(ctx, plan.ID)
	if err != nil {
		return nil, resolvedAssumption{}, wrapRepo("list plan holdings for snapshot", err)
	}
	da := toDomainAllocation(alloc)
	dh := holdingsToDomain(holds)
	if checks := domain.ValidateAllWeights(da, dh); !checks.Passed {
		return nil, resolvedAssumption{}, newErr("plan_weights_invalid",
			"plan weights are incomplete or invalid", map[string]any{"checks": checks.Checks})
	}
	if err := validateSnapshotHoldings(holds, params.TotalAssetsMinor); err != nil {
		return nil, resolvedAssumption{}, err
	}

	resolved, err := s.ResolveAssumptionProfile(ctx, params)
	if err != nil {
		return nil, resolvedAssumption{}, err
	}
	if scenarioOverride != "" {
		resolved.Mode = repository.ModeBlendedPrior
		resolved.Scenario = scenarioOverride
	}
	customByInstrument := parseCustomReturnAssumptions(params.CustomReturnAssumptionsJSON)
	overrides, err := s.activeReturnOverrides(ctx, plan)
	if err != nil {
		return nil, resolvedAssumption{}, err
	}

	lines := domain.ComputeHoldingTargets(da, dh, holdingMeta(holds), params.TotalAssetsMinor)
	assets, err := s.buildSnapshotAssets(ctx, plan, lines, resolved, customByInstrument, overrides)
	if err != nil {
		return nil, resolvedAssumption{}, err
	}
	return assets, resolved, nil
}

func (s *SimulationService) buildInputSnapshot(ctx context.Context, plan repository.Plan,
	req CreateSimulationRequest, scenarioOverride string,
) (*simulation.InputSnapshot, string, error) {
	params, err := s.params.Get(ctx, plan.ID)
	if err != nil {
		return nil, "", wrapRepo("get plan parameters for snapshot", err)
	}
	if err := applyCreateSimOverrides(&params, req); err != nil {
		return nil, "", err
	}
	if err := validateSimulationReady(params); err != nil {
		return nil, "", newErr("parameters_invalid", err.Error(), nil)
	}

	assets, resolved, err := s.resolveAndBuildAssets(ctx, plan, params, scenarioOverride)
	if err != nil {
		return nil, "", err
	}

	seed := params.Seed
	if seed == nil {
		v, err := randomSeed()
		if err != nil {
			return nil, "", err
		}
		seed = &v
	}

	var effectiveIdentity *EffectiveAssumptionIdentity
	if resolved.Mode != repository.ModeHistoricalCAGR {
		identity := identityFromResolved(resolved)
		effectiveIdentity = &identity
	}
	configHash, err := s.hash.ComputeWithIdentity(ctx, plan.ID, effectiveIdentity)
	if err != nil {
		return nil, "", err
	}

	in, err := buildInputSnapshotStruct(plan, params, *seed, configHash, assets, resolved)
	if err != nil {
		return nil, "", err
	}
	if effectiveIdentity != nil &&
		(in.AssumptionProfileID != effectiveIdentity.ProfileID ||
			in.AssumptionProfileVersion != effectiveIdentity.ProfileVersion ||
			in.AssumptionProfileContentHash != effectiveIdentity.ContentHash ||
			in.ReturnAssumptionScenario != effectiveIdentity.Scenario) {
		return nil, "", newErr("assumption_identity_inconsistent",
			"configuration hash and snapshot resolved different assumption identities", nil)
	}
	inputHash, err := simulation.HashInput(in)
	if err != nil {
		return nil, "", wrapRepo("hash simulation input", err)
	}
	return in, inputHash, nil
}

// activeReturnOverrides loads the plan's asset-level overrides and drops any that
// have expired as of the plan's valuation date. ISO dates compare
// lexicographically, so an override is active while expires_at >= valuation_date.
func (s *SimulationService) activeReturnOverrides(
	ctx context.Context, plan repository.Plan,
) (map[string]repository.PlanReturnOverride, error) {
	rows, err := s.overrides.ListByPlan(ctx, plan.ID)
	if err != nil {
		return nil, wrapRepo("list plan return overrides", err)
	}
	out := make(map[string]repository.PlanReturnOverride, len(rows))
	for _, o := range rows {
		if o.ExpiresAt < plan.ValuationDate {
			continue
		}
		out[o.AssetKey] = o
	}
	return out, nil
}

// validateSimulationReady defers entirely to validateParameters, which now owns
// the advanced-parameter ranges and the (1 - tax*taxable) > 0 rule. Keeping a
// single source of truth prevents a plan that passes creation from failing only
// at simulation time.
func validateSimulationReady(p repository.PlanParameters) error {
	return validateParameters(p)
}

func randomSeed() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return 0, fmt.Errorf("generate random seed: %w", err)
	}
	return n.Int64(), nil
}
