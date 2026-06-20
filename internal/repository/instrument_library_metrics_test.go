package repository

import (
	"context"
	"testing"

	"github.com/fireman/fireman/internal/testutil"
)

func f64(v float64) *float64 { return &v }

// TestListWithMetricsJoinsProjection covers td/057 P1: ListWithMetrics returns
// the precomputed projection via a single LEFT JOIN. Instruments with a
// projection row expose data_as_of / quality / trailing returns; instruments
// without one keep empty/nil list fields (rendered "—") and never trigger a
// per-row history read.
func TestListWithMetricsJoinsProjection(t *testing.T) {
	db := testutil.OpenTestDB(t)
	instRepo := NewInstrumentRepo(db)
	libRepo := NewInstrumentLibraryMetricsRepo(db)
	ctx := context.Background()

	seedInstrument(t, instRepo, "ins_proj", "EQA", "有投影", "equity", "domestic", 2000)
	seedInstrument(t, instRepo, "ins_bare", "EQB", "无投影", "equity", "domestic", 1000)

	if err := libRepo.Upsert(ctx, nil, LibraryMetricsRecord{
		InstrumentID: "ins_proj", DataAsOf: "2024-01-01", DataSourceName: "test_src",
		PointType: "adjusted_close", QualityStatus: "available", SimulationEligible: true,
		HistoryDepth: "long", CompleteYearCount: 5, MonthlyReturnCount: 60,
		MetricsVersion: "v1", WarningsJSON: `["w1"]`, TrailingAsOf: "2024-01-01",
		OneYearAnnualized: f64(0.1), ThreeYearAnnualized: f64(0.2), FiveYearAnnualized: f64(0.3),
	}); err != nil {
		t.Fatalf("upsert projection: %v", err)
	}

	items, err := instRepo.ListWithMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]InstrumentRecord{}
	for _, it := range items {
		byID[it.ID] = it
	}

	proj := byID["ins_proj"]
	if proj.DataAsOf != "2024-01-01" || proj.DataSourceName != "test_src" || proj.PointType != "adjusted_close" {
		t.Fatalf("projection metadata not joined: %+v", proj)
	}
	if proj.QualityStatus != "available" || !proj.SimulationEligible || proj.CompleteYearCount != 5 {
		t.Fatalf("projection eligibility not joined: %+v", proj)
	}
	if proj.HistoryDepth != "long" || proj.MonthlyReturnCount != 60 || proj.MetricsVersion != "v1" {
		t.Fatalf("projection metrics not joined: %+v", proj)
	}
	if len(proj.Warnings) != 1 || proj.Warnings[0] != "w1" {
		t.Fatalf("warnings not joined: %+v", proj.Warnings)
	}
	if proj.TrailingReturns == nil ||
		proj.TrailingReturns.OneYearAnnualizedReturn == nil || *proj.TrailingReturns.OneYearAnnualizedReturn != 0.1 ||
		proj.TrailingReturns.ThreeYearAnnualizedReturn == nil || *proj.TrailingReturns.ThreeYearAnnualizedReturn != 0.2 ||
		proj.TrailingReturns.FiveYearAnnualizedReturn == nil || *proj.TrailingReturns.FiveYearAnnualizedReturn != 0.3 {
		t.Fatalf("trailing returns not joined: %+v", proj.TrailingReturns)
	}

	bare := byID["ins_bare"]
	if bare.DataAsOf != "" || bare.QualityStatus != "" || bare.TrailingReturns != nil {
		t.Fatalf("instrument without projection should have empty list fields: %+v", bare)
	}
}

// TestSearchJoinsProjection ensures the paginated search path also serves the
// projection (and nil trailing returns persist through the JOIN as "—").
func TestSearchJoinsProjection(t *testing.T) {
	db := testutil.OpenTestDB(t)
	instRepo := NewInstrumentRepo(db)
	libRepo := NewInstrumentLibraryMetricsRepo(db)
	ctx := context.Background()

	seedInstrument(t, instRepo, "ins_s", "EQS", "搜索资产", "equity", "domestic", 1000)
	if err := libRepo.Upsert(ctx, nil, LibraryMetricsRecord{
		InstrumentID: "ins_s", DataAsOf: "2025-06-01", QualityStatus: "insufficient_history",
		TrailingAsOf: "2025-06-01", OneYearAnnualized: f64(0.05),
	}); err != nil {
		t.Fatalf("upsert projection: %v", err)
	}

	res, err := instRepo.Search(ctx, InstrumentSearchOptions{ExcludeSystem: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Instruments) != 1 {
		t.Fatalf("search len = %d, want 1", len(res.Instruments))
	}
	got := res.Instruments[0]
	if got.DataAsOf != "2025-06-01" || got.QualityStatus != "insufficient_history" {
		t.Fatalf("search projection metadata not joined: %+v", got)
	}
	if got.TrailingReturns == nil || got.TrailingReturns.OneYearAnnualizedReturn == nil ||
		*got.TrailingReturns.OneYearAnnualizedReturn != 0.05 {
		t.Fatalf("search 1y trailing not joined: %+v", got.TrailingReturns)
	}
	if got.TrailingReturns.ThreeYearAnnualizedReturn != nil || got.TrailingReturns.FiveYearAnnualizedReturn != nil {
		t.Fatalf("missing 3y/5y must stay nil: %+v", got.TrailingReturns)
	}
}

// TestUpsertReplacesProjection verifies the ON CONFLICT update path keeps a
// single row per instrument and overwrites stale values.
func TestUpsertReplacesProjection(t *testing.T) {
	db := testutil.OpenTestDB(t)
	instRepo := NewInstrumentRepo(db)
	libRepo := NewInstrumentLibraryMetricsRepo(db)
	ctx := context.Background()

	seedInstrument(t, instRepo, "ins_up", "EQU", "更新资产", "equity", "domestic", 1000)
	if err := libRepo.Upsert(ctx, nil, LibraryMetricsRecord{
		InstrumentID: "ins_up", DataAsOf: "2024-01-01", QualityStatus: "available",
		FiveYearAnnualized: f64(0.3),
	}); err != nil {
		t.Fatal(err)
	}
	if err := libRepo.Upsert(ctx, nil, LibraryMetricsRecord{
		InstrumentID: "ins_up", DataAsOf: "2025-12-31", QualityStatus: "insufficient_history",
		FiveYearAnnualized: nil,
	}); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM instrument_library_metrics WHERE instrument_id='ins_up'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected single projection row, got %d", count)
	}

	items, err := instRepo.ListWithMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID != "ins_up" {
			continue
		}
		if it.DataAsOf != "2025-12-31" || it.QualityStatus != "insufficient_history" {
			t.Fatalf("projection not overwritten: %+v", it)
		}
		if it.TrailingReturns == nil || it.TrailingReturns.FiveYearAnnualizedReturn != nil {
			t.Fatalf("5y should be cleared to nil after update: %+v", it.TrailingReturns)
		}
	}
}
