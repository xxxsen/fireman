package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func TestIsImportableCandidate(t *testing.T) {
	if !IsImportableCandidate("cn_exchange_fund", "etf") {
		t.Fatal("etf should be importable for cn_exchange_fund")
	}
	if IsImportableCandidate("cn_exchange_fund", "stock") {
		t.Fatal("stock must not be importable for cn_exchange_fund")
	}
	if !IsImportableCandidate("cn_exchange_stock", "stock") {
		t.Fatal("stock should be importable for cn_exchange_stock")
	}
}

func TestEvaluateInstrumentForPlan_SystemCash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	marketRepo := repository.NewMarketDataRepo(db)
	inst, err := repository.NewInstrumentRepo(db).GetByID(context.Background(), repository.SystemCashInstrumentID)
	if err != nil {
		t.Fatal(err)
	}
	eval, err := EvaluateInstrumentForPlan(context.Background(), inst, marketRepo, "2020-01-01")
	if err != nil {
		t.Fatalf("system cash should be available: %v", err)
	}
	if !eval.Available || eval.QualityStatus != "available" {
		t.Fatalf("eval=%+v", eval)
	}
}

func TestEvaluateInstrumentForPlan_RejectsOtherSystemInstrument(t *testing.T) {
	db := testutil.OpenTestDB(t)
	marketRepo := repository.NewMarketDataRepo(db)
	instRepo := repository.NewInstrumentRepo(db)
	inst, err := instRepo.GetByID(context.Background(), "system_fx_usdcny")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateInstrumentForPlan(context.Background(), inst, marketRepo, "2026-06-09"); err == nil {
		t.Fatal("expected system FX instrument to be rejected")
	}
}

func TestLibraryMetricsAtDate_EmptyValuationUsesLatestTradeDate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	instID := "ins_library_metrics_empty_valuation"
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO instruments (
			id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, is_system, expense_ratio, expense_ratio_status,
			fee_treatment, status, created_at, updated_at
		) VALUES (?, '510300', '测试ETF', 'CN', 'cn_exchange_fund', 'equity', 'domestic', 'CNY',
			'akshare', '510300', 'none', 0, NULL, 'unavailable', 'embedded', 'active', ?, ?)`,
		instID, now, now); err != nil {
		t.Fatal(err)
	}

	points := buildTestSyntheticHistoryForYears([]int{2020, 2021, 2022, 2023, 2024})
	marketRepo := repository.NewMarketDataRepo(db)
	batch := make([]repository.MarketDataPoint, len(points))
	for i, p := range points {
		batch[i] = repository.MarketDataPoint{
			InstrumentID: instID,
			TradeDate:    p.TradeDate,
			Value:        p.Value,
			PointType:    "adjusted_close",
			SourceName:   "test",
			FetchedAt:    now,
		}
	}
	if err := marketRepo.UpsertBatch(ctx, nil, instID, batch); err != nil {
		t.Fatal(err)
	}

	metrics, quality := libraryMetricsAtDate(ctx, marketRepo, instID, "")
	if quality != marketdata.QualityStatusAvailable {
		t.Fatalf("quality=%s want available", quality)
	}
	if !metrics.SimulationEligible {
		t.Fatal("expected simulation eligible with empty valuation date")
	}
	if metrics.CompleteYearCount < 3 {
		t.Fatalf("complete years=%d want >=3", metrics.CompleteYearCount)
	}
}

func buildTestSyntheticHistoryForYears(years []int) []marketdata.DataPoint {
	var points []marketdata.DataPoint
	value := 100.0
	for _, y := range years {
		anchorDate := fmt.Sprintf("%d-12-31", y-1)
		points = append(points, marketdata.DataPoint{TradeDate: anchorDate, Value: value})
		for month := 1; month <= 12; month++ {
			for day := 1; day <= 11; day++ {
				value *= 1.0005
				d := time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.UTC)
				points = append(points, marketdata.DataPoint{TradeDate: d.Format("2006-01-02"), Value: value})
			}
		}
	}
	return points
}
