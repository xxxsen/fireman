package service

import (
	"context"
	"testing"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func f64(v float64) *float64 { return &v }

func seedActiveInstrument(t *testing.T, repo *repository.InstrumentRepo, id, status string) {
	t.Helper()
	if err := repo.Create(context.Background(), nil, repository.InstrumentRecord{
		ID: id, Code: id, Name: id, Market: "CN", InstrumentType: "fund",
		AssetClass: "equity", Region: "domestic", Currency: "CNY",
		Provider: "akshare", ProviderSymbol: id, AdjustPolicy: "qfq",
		ExpenseRatioStatus: "unknown", FeeTreatment: "net", Status: status, CreatedAt: 1000,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// TestListServesProjectionWithoutPerRowReads verifies that the asset library
// list reads its metadata/trailing returns from the precomputed projection
// (single JOIN) and never recomputes full history per row. The active instrument
// carries a projection row but zero market_data_points, so populated trailing
// returns prove the list used the projection rather than a per-row history read.
func TestListServesProjectionWithoutPerRowReads(t *testing.T) {
	db := testutil.OpenTestDB(t)
	instRepo := repository.NewInstrumentRepo(db)
	libRepo := repository.NewInstrumentLibraryMetricsRepo(db)
	ctx := context.Background()

	seedActiveInstrument(t, instRepo, "ins_eq", "active")
	seedActiveInstrument(t, instRepo, "ins_inactive", "delisted")
	// Active instrument whose projection is missing must render "—" rather than
	// falling back to a per-row synchronous computation.
	seedActiveInstrument(t, instRepo, "ins_noproj", "active")

	// Active row: projection present, but no market_data_points exist at all.
	if err := libRepo.Upsert(ctx, nil, repository.LibraryMetricsRecord{
		InstrumentID: "ins_eq", DataAsOf: "2024-01-01", DataSourceName: "test_src",
		PointType: "adjusted_close", QualityStatus: "available", SimulationEligible: true,
		TrailingAsOf:      "2024-01-01",
		OneYearAnnualized: f64(0.1), ThreeYearAnnualized: f64(0.2), FiveYearAnnualized: f64(0.3),
	}); err != nil {
		t.Fatal(err)
	}
	// Inactive row carries a stale projection that must be hidden (render "—").
	if err := libRepo.Upsert(ctx, nil, repository.LibraryMetricsRecord{
		InstrumentID: "ins_inactive", DataAsOf: "2020-01-01", QualityStatus: "available",
		TrailingAsOf: "2020-01-01", OneYearAnnualized: f64(0.9),
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewInstrumentService(db, instRepo, repository.NewMarketDataRepo(db), nil, nil, nil, nil)
	items, err := svc.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]repository.InstrumentRecord{}
	for _, it := range items {
		byID[it.ID] = it
	}

	eq := byID["ins_eq"]
	if eq.DataAsOf != "2024-01-01" || eq.DataSourceName != "test_src" || eq.QualityStatus != "available" {
		t.Fatalf("active projection not served: %+v", eq)
	}
	if eq.TrailingReturns == nil || eq.TrailingReturns.FiveYearAnnualizedReturn == nil ||
		*eq.TrailingReturns.FiveYearAnnualizedReturn != 0.3 {
		t.Fatalf("active 5y trailing must come from projection: %+v", eq.TrailingReturns)
	}

	inactive := byID["ins_inactive"]
	if inactive.TrailingReturns != nil || inactive.DataAsOf != "" || inactive.QualityStatus != "" {
		t.Fatalf("inactive instrument must hide stale projection: %+v", inactive)
	}

	noproj := byID["ins_noproj"]
	if noproj.TrailingReturns != nil || noproj.DataAsOf != "" || noproj.QualityStatus != "" {
		t.Fatalf("active instrument without projection must render — (no fallback): %+v", noproj)
	}
}

// TestSearchServesProjection ensures the paginated search path (holdings picker)
// surfaces the projection for active rows and clears it for inactive rows.
func TestSearchServesProjection(t *testing.T) {
	db := testutil.OpenTestDB(t)
	instRepo := repository.NewInstrumentRepo(db)
	libRepo := repository.NewInstrumentLibraryMetricsRepo(db)
	ctx := context.Background()

	seedActiveInstrument(t, instRepo, "ins_search", "active")
	if err := libRepo.Upsert(ctx, nil, repository.LibraryMetricsRecord{
		InstrumentID: "ins_search", DataAsOf: "2026-01-01", QualityStatus: "available",
		SimulationEligible: true, TrailingAsOf: "2026-01-01", ThreeYearAnnualized: f64(0.12),
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewInstrumentService(db, instRepo, repository.NewMarketDataRepo(db), nil, nil, nil, nil)
	view, err := svc.Search(ctx, repository.InstrumentSearchOptions{ExcludeSystem: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Instruments) != 1 {
		t.Fatalf("search len = %d, want 1", len(view.Instruments))
	}
	got := view.Instruments[0]
	if !got.SimulationEligible || got.TrailingReturns == nil ||
		got.TrailingReturns.ThreeYearAnnualizedReturn == nil || *got.TrailingReturns.ThreeYearAnnualizedReturn != 0.12 {
		t.Fatalf("search did not serve projection: %+v", got)
	}
}
