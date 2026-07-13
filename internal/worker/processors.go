package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/improvement"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/sensitivity"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/stress"
	taskcore "github.com/fireman/fireman/internal/task"
)

type Attempt struct {
	WorkerID string
	Token    string
	Progress func(done, total int, phase string)
	Canceled func() bool
}

type ProcessorSet struct {
	db                     *sql.DB
	coordinator            *taskcore.Coordinator
	sims                   *repository.SimulationRepo
	analysis               *repository.AnalysisRepo
	improvements           *repository.FirePlanImprovementRepo
	research               *service.ResearchService
	autoUpdates            *service.AutoUpdateService
	resources              *resourcedb.DB
	improvementParallelism int
	processors             map[string]func(context.Context, repository.WorkerTask, Attempt) error
}

func NewProcessorSet(
	db *sql.DB, coordinator *taskcore.Coordinator, research *service.ResearchService,
	autoUpdates *service.AutoUpdateService, resources ...*resourcedb.DB,
) *ProcessorSet {
	var resourceStore *resourcedb.DB
	if len(resources) > 0 {
		resourceStore = resources[0]
	}
	set := &ProcessorSet{
		db: db, coordinator: coordinator, sims: repository.NewSimulationRepo(db),
		analysis: repository.NewAnalysisRepo(db), improvements: repository.NewFirePlanImprovementRepo(db),
		research: research, autoUpdates: autoUpdates, resources: resourceStore,
		improvementParallelism: 4,
	}
	set.processors = map[string]func(context.Context, repository.WorkerTask, Attempt) error{
		repository.WorkerTaskTypeSimulation:           set.simulation,
		repository.WorkerTaskTypeStress:               set.stress,
		repository.WorkerTaskTypeSensitivity:          set.sensitivity,
		repository.WorkerTaskTypeFirePlanImprovement:  set.firePlanImprovement,
		repository.WorkerTaskTypeResearchBacktest:     set.researchBacktest,
		repository.WorkerTaskTypeResearchOptimization: set.researchOptimization,
		repository.WorkerTaskTypeAutoUpdateScan:       set.autoUpdateScan,
	}
	definitions := coordinator.Registry().DefinitionsFor(repository.WorkerTypeGo)
	if len(definitions) != len(set.processors) {
		panic("go processor registry does not match TaskDefinition registry")
	}
	for _, definition := range definitions {
		if _, ok := set.processors[definition.ProcessorKey]; !ok {
			panic("missing Go processor for " + definition.Type)
		}
	}
	return set
}

func (p *ProcessorSet) SetImprovementParallelism(value int) {
	if value >= 1 && value <= 16 {
		p.improvementParallelism = value
	}
}

//nolint:lll // Registry dispatch includes the unsupported type in the protocol error detail.
func (p *ProcessorSet) Execute(ctx context.Context, item repository.WorkerTask, attempt Attempt) error {
	processor, ok := p.processors[item.Type]
	if !ok {
		return taskcore.NewError(taskcore.ErrTypeUnsupported, "go processor is not registered", map[string]any{"type": item.Type})
	}
	return processor(ctx, item, attempt)
}

func (p *ProcessorSet) stress(ctx context.Context, item repository.WorkerTask, attempt Attempt) error {
	return p.analysisTask(ctx, item, attempt, repository.AnalysisTypeStress)
}

func (p *ProcessorSet) sensitivity(ctx context.Context, item repository.WorkerTask, attempt Attempt) error {
	return p.analysisTask(ctx, item, attempt, repository.AnalysisTypeSensitivity)
}

//nolint:wrapcheck // Typed search and transaction errors must reach the supervisor classifier.
func (p *ProcessorSet) firePlanImprovement(
	ctx context.Context, item repository.WorkerTask, attempt Attempt,
) error {
	run, err := p.improvements.GetByTaskID(ctx, item.ID)
	if err != nil {
		return fmt.Errorf("load fire plan improvement input: %w", err)
	}
	var frozen improvement.FrozenInput
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &frozen); err != nil {
		return taskcore.NewError(taskcore.ErrPayloadInvalid,
			"decode fire plan improvement input: "+err.Error(), nil)
	}
	attempt.Progress(0, improvement.SearchUpperBound(frozen.Config), "searching")
	result, err := improvement.Search(ctx, run.ID, frozen, improvement.SearchOptions{
		Parallelism: p.improvementParallelism, Progress: attempt.Progress,
	})
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return context.Canceled
		case errors.Is(err, improvement.ErrMonotonicityViolation):
			return service.NewPublicError("improvement_monotonicity_violation", err.Error(), nil)
		case errors.Is(err, improvement.ErrResultInconsistent), errors.Is(err, improvement.ErrOutcomeBitsInvalid):
			return service.NewPublicError("improvement_result_inconsistent", err.Error(), nil)
		default:
			return err
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal fire plan improvement result: %w", err)
	}
	completedAt := time.Now().UnixMilli()
	return fdb.WithTx(ctx, p.db, func(tx *sql.Tx) error {
		if err := p.improvements.CompleteTx(ctx, tx, item.ID, raw, completedAt); err != nil {
			return err
		}
		return p.complete(ctx, tx, item, attempt, "fire_plan_improvement_run:"+run.ID,
			map[string]any{"run_id": run.ID, "target_reached": result.TargetReached})
	})
}

//nolint:wrapcheck // Coordinator errors retain lease and cancellation codes for the supervisor.
func (p *ProcessorSet) complete(
	ctx context.Context, tx *sql.Tx, item repository.WorkerTask, attempt Attempt,
	resultKey string, meta any,
) error {
	return p.coordinator.CompleteOwnedTx(ctx, tx, item.ID, attempt.WorkerID,
		repository.HashClaimToken(attempt.Token), resultKey, meta, time.Now().UnixMilli())
}

//nolint:lll,wrapcheck // Simulation result tables and task completion commit in one transaction.
func (p *ProcessorSet) simulation(ctx context.Context, item repository.WorkerTask, attempt Attempt) error {
	run, err := p.sims.GetByTaskID(ctx, item.ID)
	if err != nil {
		return fmt.Errorf("load simulation run: %w", err)
	}
	var snapshot simulation.InputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snapshot); err != nil {
		return taskcore.NewError(taskcore.ErrPayloadInvalid,
			"decode simulation snapshot: "+err.Error(), nil)
	}
	attempt.Progress(0, snapshot.Parameters.SimulationRuns, "simulating")
	result := simulation.Run(&snapshot, simulation.RunOptions{
		Runs: snapshot.Parameters.SimulationRuns, Progress: attempt.Progress, CancelCheck: attempt.Canceled,
	})
	if attempt.Canceled() {
		return context.Canceled
	}
	summary, err := json.Marshal(result.Summary)
	if err != nil {
		return fmt.Errorf("marshal simulation summary: %w", err)
	}
	representative := make(map[int]string, len(result.Representative))
	for label, pathNo := range result.Representative {
		representative[pathNo] = label
	}
	paths := make([]repository.PathIndexRow, 0, len(result.Paths))
	for _, path := range result.Paths {
		paths = append(paths, repository.PathIndexRow{
			RunID: run.ID, PathNo: path.PathNo, PathSeed: path.PathSeed, Succeeded: path.Succeeded,
			FailureMonth: path.FailureMonth, TerminalWealthMinor: path.TerminalWealthMinor,
			MaxDrawdown: path.MaxDrawdown, RepresentativePercentile: representative[path.PathNo],
		})
	}
	return fdb.WithTx(ctx, p.db, func(tx *sql.Tx) error {
		if err := p.sims.Complete(ctx, tx, run.ID, result.SuccessCount, result.FailureCount, summary); err != nil {
			return err
		}
		if err := p.sims.ReplacePathIndex(ctx, tx, run.ID, paths); err != nil {
			return err
		}
		if err := p.sims.ReplaceQuantileSeries(ctx, tx, run.ID, quantileRows(run.ID, result.QuantileSeries)); err != nil {
			return err
		}
		if err := p.sims.ReplaceRealQuantileSeries(ctx, tx, run.ID, quantileRows(run.ID, result.RealQuantileSeries)); err != nil {
			return err
		}
		return p.complete(ctx, tx, item, attempt, "simulation_run:"+run.ID, map[string]any{"run_id": run.ID})
	})
}

type pendingAnalysisPayload struct {
	Pending       bool                     `json:"pending"`
	InputSnapshot simulation.InputSnapshot `json:"input_snapshot"`
}

//nolint:wrapcheck // Engine and repository errors are classified by the supervisor at this boundary.
func (p *ProcessorSet) analysisTask(
	ctx context.Context, item repository.WorkerTask, attempt Attempt, analysisType string,
) error {
	record, err := p.analysis.GetByTaskID(ctx, item.ID)
	if err != nil {
		return fmt.Errorf("load analysis input: %w", err)
	}
	var pending pendingAnalysisPayload
	if err := json.Unmarshal([]byte(record.ResultJSON), &pending); err != nil {
		return taskcore.NewError(taskcore.ErrPayloadInvalid,
			"decode analysis input: "+err.Error(), nil)
	}
	var report any
	switch analysisType {
	case repository.AnalysisTypeStress:
		report = stress.Run(&pending.InputSnapshot, stress.RunOptions{
			Runs:     pending.InputSnapshot.Parameters.SimulationRuns,
			Progress: attempt.Progress, CancelCheck: attempt.Canceled,
		})
	case repository.AnalysisTypeSensitivity:
		report, err = sensitivity.Run(&pending.InputSnapshot, sensitivity.RunOptions{
			Runs:     pending.InputSnapshot.Parameters.SimulationRuns,
			Progress: attempt.Progress, CancelCheck: attempt.Canceled,
		})
	}
	if err != nil {
		return err
	}
	if attempt.Canceled() {
		return context.Canceled
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal analysis result: %w", err)
	}
	return fdb.WithTx(ctx, p.db, func(tx *sql.Tx) error {
		if err := p.analysis.CompleteTx(ctx, tx, item.ID, string(raw)); err != nil {
			return err
		}
		return p.complete(ctx, tx, item, attempt, "analysis_result:"+item.ID,
			map[string]any{"analysis_type": analysisType})
	})
}

//nolint:wrapcheck // Research service returns typed cancellation and stale-input errors.
func (p *ProcessorSet) researchBacktest(ctx context.Context, item repository.WorkerTask, attempt Attempt) error {
	run, err := p.researchRun(ctx, item.ID, false)
	if err != nil {
		return err
	}
	return p.research.ExecuteBacktestTaskOwned(ctx, item.ID, attempt.Canceled, attempt.Progress,
		func(tx *sql.Tx) error {
			return p.complete(ctx, tx, item, attempt, "research_backtest_run:"+run,
				map[string]any{"run_id": run})
		})
}

//nolint:wrapcheck // Research service returns typed cancellation and persistence errors.
func (p *ProcessorSet) researchOptimization(ctx context.Context, item repository.WorkerTask, attempt Attempt) error {
	run, err := p.researchRun(ctx, item.ID, true)
	if err != nil {
		return err
	}
	return p.research.ExecuteOptimizationTaskOwned(ctx, item.ID, attempt.Canceled, attempt.Progress,
		func(tx *sql.Tx) error {
			return p.complete(ctx, tx, item, attempt, "research_optimization_run:"+run,
				map[string]any{"run_id": run})
		})
}

//nolint:wrapcheck // Repository task-to-run lookup errors are preserved for processor classification.
func (p *ProcessorSet) researchRun(ctx context.Context, taskID string, optimization bool) (string, error) {
	repo := repository.NewResearchRepo(p.db)
	if optimization {
		run, err := repo.GetOptimizationRunByTaskID(ctx, taskID)
		return run.ID, err
	}
	run, err := repo.GetRunByTaskID(ctx, taskID)
	return run.ID, err
}

//nolint:wrapcheck // Auto-update service errors are reported verbatim on the scan task.
func (p *ProcessorSet) autoUpdateScan(ctx context.Context, item repository.WorkerTask, attempt Attempt) error {
	attempt.Progress(0, 1, "scanning")
	summary, err := p.autoUpdates.RunOnceSummary(ctx)
	if err != nil {
		return err
	}
	if attempt.Canceled() {
		return context.Canceled
	}
	if p.resources == nil {
		return taskcore.NewError(taskcore.ErrTypeUnsupported,
			"auto-update scan resource store is not configured", nil)
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal auto-update scan summary: %w", err)
	}
	envelope, err := p.resources.InsertContent(ctx, "application/json", "",
		resourcedb.SupportedSchemaVersion, raw, time.Now(), 7*24*time.Hour)
	if err != nil {
		return fmt.Errorf("store auto-update scan summary: %w", err)
	}
	return fdb.WithTx(ctx, p.db, func(tx *sql.Tx) error {
		return p.complete(ctx, tx, item, attempt, "resource:"+envelope.ResourceKey, envelope)
	})
}

func quantileRows(runID string, series []simulation.QuantilePoint) []repository.QuantileSeriesRow {
	out := make([]repository.QuantileSeriesRow, len(series))
	for i, point := range series {
		out[i] = repository.QuantileSeriesRow{
			RunID: runID, MonthOffset: point.MonthOffset,
			P00Minor: point.P00Minor, P05Minor: point.P05Minor, P25Minor: point.P25Minor,
			P50Minor: point.P50Minor, P75Minor: point.P75Minor, P95Minor: point.P95Minor,
		}
	}
	return out
}
