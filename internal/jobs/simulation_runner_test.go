package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

func TestSimulationCompleteRollsBackOnQuantileSeriesFailure(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewSimulationRepo(db)

	runID := seedPendingSimulationRun(t, db, "run_tx_q_fail", "job_tx_q", "plan_tx_q")

	snap := simulation.Run(testSnapshot(), simulation.RunOptions{Runs: 10})
	summaryJSON, err := json.Marshal(snap.Summary)
	if err != nil {
		t.Fatal(err)
	}
	rows := pathRowsFromResult(runID, snap)
	qRows := quantileRowsFromResult(runID, snap)

	err = fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := repo.Complete(ctx, tx, runID, snap.SuccessCount, snap.FailureCount, summaryJSON); err != nil {
			return err
		}
		if err := repo.ReplacePathIndex(ctx, tx, runID, rows); err != nil {
			return err
		}
		if err := repo.ReplaceQuantileSeries(ctx, tx, runID, qRows); err != nil {
			return err
		}
		return errors.New("injected failure after quantile series write")
	})
	if err == nil {
		t.Fatal("expected transaction failure")
	}

	assertSimulationRunRolledBack(ctx, t, repo, runID)
}

func TestSimulationCompleteRollsBackOnPathIndexFailure(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewSimulationRepo(db)

	runID := seedPendingSimulationRun(t, db, "run_tx_fail", "job_tx", "plan_tx")

	snap := simulation.Run(testSnapshot(), simulation.RunOptions{Runs: 10})
	summaryJSON, err := json.Marshal(snap.Summary)
	if err != nil {
		t.Fatal(err)
	}
	rows := pathRowsFromResult(runID, snap)

	err = fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := repo.Complete(ctx, tx, runID, snap.SuccessCount, snap.FailureCount, summaryJSON); err != nil {
			return err
		}
		if err := repo.ReplacePathIndex(ctx, tx, runID, rows); err != nil {
			return err
		}
		return errors.New("injected failure after path index write")
	})
	if err == nil {
		t.Fatal("expected transaction failure")
	}

	assertSimulationRunRolledBack(ctx, t, repo, runID)
}

func TestSimulationCompleteCommitsSummaryPathIndexAndQuantileSeries(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewSimulationRepo(db)

	runID := seedPendingSimulationRun(t, db, "run_tx_ok", "job_tx_ok", "plan_tx_ok")

	snap := simulation.Run(testSnapshot(), simulation.RunOptions{Runs: 10})
	summaryJSON, err := json.Marshal(snap.Summary)
	if err != nil {
		t.Fatal(err)
	}
	rows := pathRowsFromResult(runID, snap)
	qRows := quantileRowsFromResult(runID, snap)

	realRows := realQuantileRowsFromResult(runID, snap)

	err = fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := repo.Complete(ctx, tx, runID, snap.SuccessCount, snap.FailureCount, summaryJSON); err != nil {
			return err
		}
		if err := repo.ReplacePathIndex(ctx, tx, runID, rows); err != nil {
			return err
		}
		if err := repo.ReplaceQuantileSeries(ctx, tx, runID, qRows); err != nil {
			return err
		}
		return repo.ReplaceRealQuantileSeries(ctx, tx, runID, realRows)
	})
	if err != nil {
		t.Fatal(err)
	}

	run, err := repo.GetByID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.SuccessCount != snap.SuccessCount || run.FailureCount != snap.FailureCount {
		t.Fatalf("expected summary counts success=%d failure=%d, got success=%d failure=%d",
			snap.SuccessCount, snap.FailureCount, run.SuccessCount, run.FailureCount)
	}
	if len(run.SummaryJSON) <= 2 {
		t.Fatal("expected non-empty summary_json")
	}

	paths, err := repo.ListPathIndex(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(snap.Paths) {
		t.Fatalf("expected %d path index rows, got %d", len(snap.Paths), len(paths))
	}

	series, err := repo.ListQuantileSeries(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != len(snap.QuantileSeries) {
		t.Fatalf("expected %d quantile rows, got %d", len(snap.QuantileSeries), len(series))
	}

	// The real quantile series lands in its own table, aligned
	// with the nominal series and below it under positive inflation.
	realSeries, err := repo.ListRealQuantileSeries(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(realSeries) != len(snap.RealQuantileSeries) {
		t.Fatalf("expected %d real quantile rows, got %d", len(snap.RealQuantileSeries), len(realSeries))
	}
	if len(realSeries) > 0 && len(series) > 0 {
		if realSeries[len(realSeries)-1].P50Minor > series[len(series)-1].P50Minor {
			t.Fatalf("real terminal P50 %d should not exceed nominal %d",
				realSeries[len(realSeries)-1].P50Minor, series[len(series)-1].P50Minor)
		}
	}
}

func seedPendingSimulationRun(t *testing.T, db *sql.DB, runID, jobID, planID string) string {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES (?,'test','CNY','2026-06-09','active',1,0,0)`, planID)
	if err != nil {
		t.Fatal(err)
	}
	jobs := repository.NewJobRepo(db)
	if err := jobs.Create(ctx, nil, repository.Job{
		ID: jobID, PlanID: planID, Type: repository.JobTypeSimulation,
		Status: repository.JobStatusQueued, InputHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSimulationRepo(db)
	if err := repo.CreatePending(ctx, nil, repository.SimulationRun{
		ID: runID, JobID: jobID, PlanID: planID, InputHash: "h",
		InputSnapshotJSON: "{}", MarketSnapshotHash: "m", EngineVersion: "v1",
		Runs: 10, Seed: 1, HorizonMonths: 120,
	}); err != nil {
		t.Fatal(err)
	}
	return runID
}

func assertSimulationRunRolledBack(ctx context.Context, t *testing.T, repo *repository.SimulationRepo, runID string) {
	t.Helper()
	run, err := repo.GetByID(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.SuccessCount != 0 || run.FailureCount != 0 {
		t.Fatalf("expected summary rollback, got success=%d failure=%d", run.SuccessCount, run.FailureCount)
	}
	paths, err := repo.ListPathIndex(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected no path index rows, got %d", len(paths))
	}
	series, err := repo.ListQuantileSeries(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 0 {
		t.Fatalf("expected no quantile series rows, got %d", len(series))
	}
}

func pathRowsFromResult(runID string, snap simulation.RunResult) []repository.PathIndexRow {
	repPath := make(map[int]string)
	for label, pathNo := range snap.Representative {
		repPath[pathNo] = label
	}
	rows := make([]repository.PathIndexRow, 0, len(snap.Paths))
	for _, p := range snap.Paths {
		rows = append(rows, repository.PathIndexRow{
			RunID: runID, PathNo: p.PathNo, PathSeed: p.PathSeed, Succeeded: p.Succeeded,
			FailureMonth: p.FailureMonth, TerminalWealthMinor: p.TerminalWealthMinor,
			MaxDrawdown: p.MaxDrawdown, RepresentativePercentile: repPath[p.PathNo],
		})
	}
	return rows
}

func realQuantileRowsFromResult(runID string, snap simulation.RunResult) []repository.QuantileSeriesRow {
	rows := make([]repository.QuantileSeriesRow, len(snap.RealQuantileSeries))
	for i, q := range snap.RealQuantileSeries {
		rows[i] = repository.QuantileSeriesRow{
			RunID: runID, MonthOffset: q.MonthOffset,
			P00Minor: q.P00Minor, P05Minor: q.P05Minor, P25Minor: q.P25Minor,
			P50Minor: q.P50Minor, P75Minor: q.P75Minor, P95Minor: q.P95Minor,
		}
	}
	return rows
}

func quantileRowsFromResult(runID string, snap simulation.RunResult) []repository.QuantileSeriesRow {
	qRows := make([]repository.QuantileSeriesRow, len(snap.QuantileSeries))
	for i, q := range snap.QuantileSeries {
		qRows[i] = repository.QuantileSeriesRow{
			RunID: runID, MonthOffset: q.MonthOffset,
			P00Minor: q.P00Minor, P05Minor: q.P05Minor, P25Minor: q.P25Minor,
			P50Minor: q.P50Minor, P75Minor: q.P75Minor, P95Minor: q.P95Minor,
		}
	}
	return qRows
}

func testSnapshot() *simulation.InputSnapshot {
	return &simulation.InputSnapshot{
		EngineVersion: simulation.EngineVersion,
		BaseCurrency:  "CNY",
		Parameters: simulation.SnapshotParameters{
			CurrentAge: 30, RetirementAge: 55, EndAge: 60,
			TotalAssetsMinor: 1_000_000_00, AnnualSavingsMinor: 100_000_00,
			AnnualSpendingMinor: 400_000_00, TerminalWealthFloorMinor: 0,
			InflationMode: "fixed", FixedInflationRate: 0.03,
			WithdrawalType: "fixed_real", WithdrawalRate: 0.04,
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			SimulationRuns: 10, StudentTDf: 7, Seed: "42",
		},
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "h1", AssetKey: "i1", SnapshotID: "s1",
			Currency: "CNY", AssetClass: "equity", IsCash: false,
			InitialMinor: 1_000_000_00, TargetWeight: 1.0,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15, MaxDrawdown: 0.30,
			SourceHash: "eq",
		}},
	}
}
