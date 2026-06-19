package repository

import (
	"context"
	"testing"

	"github.com/fireman/fireman/internal/testutil"
)

// TestListPointsByInstrumentsGroupsAndFilters covers td/056 §4.2: the asset list
// computes trailing returns from a single batched query, grouped per instrument,
// ordered by trade_date, and bounded by sinceDate.
func TestListPointsByInstrumentsGroupsAndFilters(t *testing.T) {
	db := testutil.OpenTestDB(t)
	instRepo := NewInstrumentRepo(db)
	repo := NewMarketDataRepo(db)
	ctx := context.Background()

	seedInstrument(t, instRepo, "ins_a", "EQA", "甲", "equity", "domestic", 1000)
	seedInstrument(t, instRepo, "ins_b", "EQB", "乙", "equity", "domestic", 2000)
	seedInstrument(t, instRepo, "ins_c", "EQC", "丙", "equity", "domestic", 3000)

	mustUpsert(t, repo, "ins_a", []MarketDataPoint{
		{InstrumentID: "ins_a", TradeDate: "2020-01-01", Value: 100, PointType: "adjusted_close", SourceName: "test", FetchedAt: 1},
		{InstrumentID: "ins_a", TradeDate: "2026-01-01", Value: 200, PointType: "adjusted_close", SourceName: "test", FetchedAt: 1},
	})
	mustUpsert(t, repo, "ins_b", []MarketDataPoint{
		{InstrumentID: "ins_b", TradeDate: "2026-02-01", Value: 50, PointType: "adjusted_close", SourceName: "test", FetchedAt: 1},
		{InstrumentID: "ins_b", TradeDate: "2026-03-01", Value: 60, PointType: "adjusted_close", SourceName: "test", FetchedAt: 1},
	})
	// ins_c intentionally has no points.

	got, err := repo.ListPointsByInstruments(ctx, []string{"ins_a", "ins_b", "ins_c"}, "2021-01-01")
	if err != nil {
		t.Fatal(err)
	}

	// ins_a's 2020 point is filtered out by sinceDate; only the 2026 point remains.
	if len(got["ins_a"]) != 1 || got["ins_a"][0].TradeDate != "2026-01-01" {
		t.Fatalf("ins_a points = %+v, want only 2026-01-01", got["ins_a"])
	}
	if len(got["ins_b"]) != 2 {
		t.Fatalf("ins_b points = %d, want 2", len(got["ins_b"]))
	}
	if got["ins_b"][0].TradeDate != "2026-02-01" || got["ins_b"][1].TradeDate != "2026-03-01" {
		t.Fatalf("ins_b not ordered by trade_date: %+v", got["ins_b"])
	}
	if _, present := got["ins_c"]; present {
		t.Fatalf("ins_c should have no entry, got %+v", got["ins_c"])
	}

	// No sinceDate returns the full history.
	all, err := repo.ListPointsByInstruments(ctx, []string{"ins_a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all["ins_a"]) != 2 {
		t.Fatalf("ins_a full points = %d, want 2", len(all["ins_a"]))
	}
}

func mustUpsert(t *testing.T, repo *MarketDataRepo, instrumentID string, points []MarketDataPoint) {
	t.Helper()
	if err := repo.UpsertBatch(context.Background(), nil, instrumentID, points); err != nil {
		t.Fatalf("upsert %s: %v", instrumentID, err)
	}
}
