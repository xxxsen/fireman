package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

// research_run_executor.go executes one research_backtest task: it reloads
// the frozen input, verifies the source hash, runs the pure engine and
// persists all outputs in a single transaction.

// ErrResearchSourceChanged aborts a run whose underlying market data changed
// between snapshot freeze and execution.
var ErrResearchSourceChanged = errors.New(
	"research backtest source data changed since the run was created",
)

const researchJobPhases = 4

// ExecuteBacktestTask is the entry point called by the task worker for one
// research_backtest task. Run status converges with the task outcome: engine
// or verification errors mark the run failed; a pending cancel marks it
// canceled; worker shutdown leaves it running for the requeue reconciler.
func (s *ResearchService) ExecuteBacktestTask(
	ctx context.Context,
	taskID string,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
) error {
	return s.ExecuteBacktestTaskOwned(ctx, taskID, cancelCheck, progress, nil)
}

func (s *ResearchService) ExecuteBacktestTaskOwned(
	ctx context.Context,
	taskID string,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
	complete func(*sql.Tx) error,
) error {
	run, err := s.research.GetRunByTaskID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load research run for task %s: %w", taskID, err)
	}
	if run.Status == repository.ResearchRunStatusSucceeded {
		return nil
	}
	snapshot, ds, err := s.loadBacktestExecution(ctx, run, progress)
	if err != nil {
		return err
	}
	if cancelCheck != nil && cancelCheck() {
		s.markRunCanceled(ctx, run.ID)
		return context.Canceled
	}

	if progress != nil {
		progress(2, researchJobPhases, "computing")
	}
	input := backtestInputFromDataset(snapshot, ds)
	result, err := RunResearchBacktestWithOptions(input, BacktestRunOptions{CancelCheck: cancelCheck})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.markRunCanceled(ctx, run.ID)
			return context.Canceled
		}
		s.markRunFailed(ctx, run.ID)
		return fmt.Errorf("run research backtest: %w", err)
	}
	if cancelCheck != nil && cancelCheck() {
		s.markRunCanceled(ctx, run.ID)
		return context.Canceled
	}

	if progress != nil {
		progress(3, researchJobPhases, "persisting")
	}
	if err := s.persistBacktestResult(ctx, run.ID, result, complete); err != nil {
		if ctx.Err() != nil {
			return err
		}
		s.markRunFailed(ctx, run.ID)
		return err
	}
	if progress != nil {
		progress(researchJobPhases, researchJobPhases, "done")
	}
	return nil
}

func (s *ResearchService) loadBacktestExecution(
	ctx context.Context,
	run repository.ResearchBacktestRun,
	progress func(int, int, string),
) (researchInputSnapshot, *researchDataset, error) {
	var snapshot researchInputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snapshot); err != nil {
		s.markRunFailed(ctx, run.ID)
		return snapshot, nil, fmt.Errorf("decode research input snapshot: %w", err)
	}
	if err := s.research.MarkRunRunning(ctx, run.ID); err != nil {
		return snapshot, nil, fmt.Errorf("mark research run running: %w", err)
	}
	if progress != nil {
		progress(0, researchJobPhases, "loading")
	}
	ds, err := s.loadDatasetFromSnapshot(ctx, snapshot)
	if err != nil {
		if ctx.Err() == nil {
			s.markRunFailed(ctx, run.ID)
		}
		return snapshot, nil, fmt.Errorf("load research snapshot dataset: %w", err)
	}
	if progress != nil {
		progress(1, researchJobPhases, "verifying")
	}
	rebuilt := buildResearchSnapshot(ds, ResearchReadiness{
		CommonStart: snapshot.CommonStart, CommonEnd: snapshot.CommonEnd,
		WindowStart: snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
	})
	if rebuilt.SourceHash != snapshot.SourceHash {
		s.markRunFailed(ctx, run.ID)
		return snapshot, nil, fmt.Errorf("%w (run %s)", ErrResearchSourceChanged, run.ID)
	}
	return snapshot, ds, nil
}

func (s *ResearchService) markRunFailed(ctx context.Context, runID string) {
	writeCtx := context.WithoutCancel(ctx)
	_ = s.research.FailRun(writeCtx, runID, repository.ResearchRunStatusFailed, s.now().UnixMilli())
}

func (s *ResearchService) markRunCanceled(ctx context.Context, runID string) {
	writeCtx := context.WithoutCancel(ctx)
	_ = s.research.FailRun(writeCtx, runID, repository.ResearchRunStatusCanceled, s.now().UnixMilli())
}

// loadDatasetFromSnapshot reloads points for the frozen item set. Collection
// parameters and item weights come from the snapshot, never from the live
// collection, so later edits cannot change a queued run.
func (s *ResearchService) loadDatasetFromSnapshot(
	ctx context.Context, snapshot researchInputSnapshot,
) (*researchDataset, error) {
	collection := repository.ResearchCollection{
		ID:                  snapshot.Collection.CollectionID,
		BaseCurrency:        snapshot.Collection.BaseCurrency,
		InitialAmountMinor:  snapshot.Collection.InitialAmountMinor,
		RebalancePolicy:     snapshot.Collection.RebalancePolicy,
		RebalanceThreshold:  snapshot.Collection.RebalanceThreshold,
		StartPolicy:         snapshot.Collection.StartPolicy,
		RiskFreeRate:        snapshot.Collection.RiskFreeRate,
		TransactionCostRate: snapshot.Collection.TransactionCostRate,
		TailRiskConfidence:  snapshot.Collection.TailRiskConfidence,
		TailRiskHorizonDays: snapshot.Collection.TailRiskHorizonDays,
		BenchmarkAssetKey:   snapshot.Collection.BenchmarkAssetKey,
		WindowStart:         snapshot.WindowStart,
		WindowEnd:           snapshot.WindowEnd,
	}
	ds := &researchDataset{
		Collection: collection,
		FX:         map[string]*researchFXData{},
	}
	fxNeeded := map[string]bool{}
	for _, a := range snapshot.Assets {
		item := repository.ResearchCollectionItem{
			ID:           a.ItemID,
			AssetKey:     a.AssetKey,
			Enabled:      true,
			Weight:       a.Weight,
			WeightLocked: a.WeightLocked,
			AdjustPolicy: a.AdjustPolicy,
			PointType:    a.PointType,
		}
		data, err := s.loadResearchAssetData(ctx, collection, item)
		if err != nil {
			return nil, err
		}
		for _, pair := range data.FXPairs {
			fxNeeded[pair] = true
		}
		ds.Enabled = append(ds.Enabled, data)
	}
	if b := snapshot.Benchmark; b != nil {
		item := repository.ResearchCollectionItem{
			AssetKey:     b.AssetKey,
			Enabled:      true,
			AdjustPolicy: b.AdjustPolicy,
			PointType:    b.PointType,
		}
		bench, err := s.loadResearchAssetData(ctx, collection, item)
		if err != nil {
			return nil, err
		}
		for _, pair := range bench.FXPairs {
			fxNeeded[pair] = true
		}
		ds.Benchmark = &bench
	}
	for pair := range fxNeeded {
		fx, err := s.loadFXData(ctx, pair)
		if err != nil {
			return nil, err
		}
		ds.FX[pair] = fx
		ds.FXPairs = append(ds.FXPairs, pair)
	}
	sort.Strings(ds.FXPairs)
	return ds, nil
}

// backtestInputFromDataset maps the reloaded dataset onto the engine input.
func backtestInputFromDataset(
	snapshot researchInputSnapshot, ds *researchDataset,
) BacktestInput {
	input := BacktestInput{
		BaseCurrency:        snapshot.Collection.BaseCurrency,
		InitialAmountMinor:  snapshot.Collection.InitialAmountMinor,
		RebalancePolicy:     snapshot.Collection.RebalancePolicy,
		RebalanceThreshold:  snapshot.Collection.RebalanceThreshold,
		RiskFreeRate:        snapshot.Collection.RiskFreeRate,
		TransactionCostRate: snapshot.Collection.TransactionCostRate,
		WindowStart:         snapshot.WindowStart,
		WindowEnd:           snapshot.WindowEnd,
		FX:                  map[string][]ResearchSeriesPoint{},
	}
	if snapshot.Collection.TailRiskConfidence != 0 || snapshot.Collection.TailRiskHorizonDays != 0 {
		input.TailRisk = &TailRiskSpec{
			Confidence: snapshot.Collection.TailRiskConfidence, HorizonDays: snapshot.Collection.TailRiskHorizonDays,
		}
	}
	for _, a := range ds.Enabled {
		input.Assets = append(input.Assets, BacktestAssetInput{
			AssetKey:       a.Item.AssetKey,
			Name:           a.Asset.Name,
			Currency:       a.Asset.Currency,
			Weight:         a.Item.Weight,
			IsCash:         a.IsCash,
			MaxFillGapDays: ResearchFillGapToleranceDays(a.Asset.InstrumentType),
			Points:         assetSeriesPoints(a.Points),
		})
	}
	for pair, fx := range ds.FX {
		if fx == nil || !fx.Found {
			continue
		}
		points := make([]ResearchSeriesPoint, 0, len(fx.Points))
		for _, p := range fx.Points {
			points = append(points, ResearchSeriesPoint{Date: p.TradeDate, Value: p.Value})
		}
		input.FX[pair] = points
	}
	if b := ds.Benchmark; b != nil {
		input.Benchmark = &BacktestBenchmarkInput{
			AssetKey:       b.Item.AssetKey,
			Name:           b.Asset.Name,
			Currency:       b.Asset.Currency,
			IsCash:         b.IsCash,
			MaxFillGapDays: ResearchFillGapToleranceDays(b.Asset.InstrumentType),
			Points:         assetSeriesPoints(b.Points),
		}
	}
	return input
}

func assetSeriesPoints(points []repository.MarketAssetPoint) []ResearchSeriesPoint {
	out := make([]ResearchSeriesPoint, 0, len(points))
	for _, p := range points {
		out = append(out, ResearchSeriesPoint{Date: p.TradeDate, Value: p.Value})
	}
	return out
}

// persistBacktestResult writes points/years/months and the run summary in
// one transaction so a run is never half-visible.
func (s *ResearchService) persistBacktestResult(
	ctx context.Context, runID string, result *BacktestResult, complete func(*sql.Tx) error,
) error {
	summaryJSON, err := json.Marshal(result.Summary)
	if err != nil {
		return fmt.Errorf("marshal research summary: %w", err)
	}
	dqJSON, err := json.Marshal(result.DataQuality)
	if err != nil {
		return fmt.Errorf("marshal research data quality: %w", err)
	}

	points := make([]repository.ResearchBacktestPoint, 0, len(result.Points))
	for _, p := range result.Points {
		weightsJSON, err := json.Marshal(p.Weights)
		if err != nil {
			return fmt.Errorf("marshal point weights: %w", err)
		}
		contribJSON, err := json.Marshal(p.Contributions)
		if err != nil {
			return fmt.Errorf("marshal point contributions: %w", err)
		}
		points = append(points, repository.ResearchBacktestPoint{
			RunID:             runID,
			TradeDate:         p.Date,
			NAV:               p.NAV,
			CumulativeReturn:  p.CumulativeReturn,
			PeriodReturn:      p.PeriodReturn,
			Drawdown:          p.Drawdown,
			BenchmarkNAV:      p.BenchmarkNAV,
			BenchmarkReturn:   p.BenchmarkReturn,
			WeightsJSON:       string(weightsJSON),
			ContributionsJSON: string(contribJSON),
		})
	}
	years := make([]repository.ResearchBacktestYear, 0, len(result.Years))
	for _, y := range result.Years {
		years = append(years, repository.ResearchBacktestYear{
			RunID: runID, Year: y.Year, AnnualReturn: y.AnnualReturn,
			Volatility: y.Volatility, MaxDrawdown: y.MaxDrawdown,
			StartNAV: y.StartNAV, EndNAV: y.EndNAV, IsPartial: y.IsPartial,
		})
	}
	months := make([]repository.ResearchBacktestMonth, 0, len(result.Months))
	for _, m := range result.Months {
		months = append(months, repository.ResearchBacktestMonth{
			RunID: runID, Year: m.Year, Month: m.Month, MonthlyReturn: m.MonthlyReturn,
		})
	}

	completedAt := s.now().UnixMilli()
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.research.ReplacePointsTx(ctx, tx, runID, points); err != nil {
			return fmt.Errorf("replace research run points: %w", err)
		}
		if err := s.research.ReplaceYearsTx(ctx, tx, runID, years); err != nil {
			return fmt.Errorf("replace research run years: %w", err)
		}
		if err := s.research.ReplaceMonthsTx(ctx, tx, runID, months); err != nil {
			return fmt.Errorf("replace research run months: %w", err)
		}
		if err := s.research.CompleteRunTx(
			ctx, tx, runID, string(summaryJSON), string(dqJSON), completedAt,
		); err != nil {
			return fmt.Errorf("complete research run: %w", err)
		}
		if complete != nil {
			if err := complete(tx); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist research run result: %w", err)
	}
	return nil
}
