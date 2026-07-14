package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/investmentpath"
	"github.com/fireman/fireman/internal/repository"
)

func investmentPathTestRequest() InvestmentPathRequest {
	return InvestmentPathRequest{
		Mode:         "income_dca",
		Asset:        InvestmentPathAssetInput{AssetKey: "IP1", AdjustPolicy: "hfq", PointType: "adjusted_close"},
		BaseCurrency: "CNY", EvaluationStart: "2020-01-01", EvaluationEnd: "2023-06-30",
		HorizonMonths: 12, MonthlyDay: 15, TransactionCostRate: .001,
		IncomeDCA: &investmentpath.IncomeDCAConfig{},
	}
}

func TestInvestmentPathReadinessCreateExecuteAndPersist(t *testing.T) {
	svc, db := newResearchTestService(t)
	insertResearchFixtureAsset(t, db, "IP1", "投入路径资产", "CNY", "2020-01-01", 1643, growthValue(100))
	request := investmentPathTestRequest()
	request.IncomeDCA.InitialInvestmentMinor = 100_000
	request.IncomeDCA.MonthlyContributionMinor = 10_000

	readiness, err := svc.GetInvestmentPathReadiness(context.Background(), request)
	if err != nil || !readiness.Ready || readiness.Resolved == nil {
		t.Fatalf("readiness=%+v err=%v", readiness, err)
	}
	created, err := svc.CreateInvestmentPathRun(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := svc.CreateInvestmentPathRun(context.Background(), request)
	if err != nil || !duplicate.Reused || duplicate.Run.ID != created.Run.ID {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	var phases []string
	err = svc.ExecuteInvestmentPathTaskOwned(context.Background(), created.Run.TaskID, func() bool { return false },
		func(_, _ int, phase string) { phases = append(phases, phase) }, func(*sql.Tx) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(phases) == 0 || phases[len(phases)-1] != "persisting" {
		t.Fatalf("phases=%v", phases)
	}
	view, err := svc.GetInvestmentPathRun(context.Background(), created.Run.ID)
	if err != nil || view.CompletedAt == nil || len(view.Strategies) != 2 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	points, err := svc.GetInvestmentPathPoints(context.Background(), view.ID, "income_dca")
	if err != nil || len(points) < 360 {
		t.Fatalf("points=%d err=%v", len(points), err)
	}
	windows, err := svc.GetInvestmentPathWindows(context.Background(), view.ID, "income_dca", 1000, 0)
	if err != nil || len(windows) != len(readiness.Resolved.WindowStarts) {
		t.Fatalf("windows=%d want=%d err=%v", len(windows), len(readiness.Resolved.WindowStarts), err)
	}
	if _, _, err := svc.ExportInvestmentPathCSV(context.Background(), view.ID); err != nil {
		t.Fatal(err)
	}
}

func TestInvestmentPathSourceChangeAndCancellationPublishNoPartialRows(t *testing.T) {
	svc, db := newResearchTestService(t)
	insertResearchFixtureAsset(t, db, "IP1", "投入路径资产", "CNY", "2020-01-01", 1643, growthValue(100))
	request := investmentPathTestRequest()
	request.IncomeDCA.MonthlyContributionMinor = 10_000
	created, err := svc.CreateInvestmentPathRun(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ExecuteInvestmentPathTaskOwned(context.Background(), created.Run.TaskID, func() bool { return true },
		func(_, _ int, _ string) {}, func(*sql.Tx) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM research_investment_path_points WHERE run_id=?`, created.Run.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial points=%d err=%v", count, err)
	}

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = repository.NewMarketAssetRepo(db).UpsertPointsTx(ctx, tx, []repository.MarketAssetPoint{{
		AssetKey: "IP1", AdjustPolicy: "hfq", PointType: "adjusted_close", TradeDate: "2022-01-01",
		Value: 999, SourceName: "changed", FetchedAt: researchTestNow.UnixMilli(),
	}})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	err = svc.ExecuteInvestmentPathTaskOwned(ctx, created.Run.TaskID, func() bool { return false },
		func(_, _ int, _ string) {}, func(*sql.Tx) error { return nil })
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != "investment_path_source_changed" {
		t.Fatalf("source change error=%v", err)
	}
}
