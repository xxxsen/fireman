package marketdata

import (
	"math"
	"testing"
)

// Anchors sampled from the 600036.SH unadjusted daily close series: each
// complete year must be computed as "last trade day of previous year -> last
// trade day of the year", the listing year is partial, and the current year
// stays a YTD row.
func TestComputeAnnualReturns_600036AnchorFixture(t *testing.T) {
	points := []DataPoint{
		{TradeDate: "2002-04-09", Value: 10.66}, // listing day
		{TradeDate: "2002-12-31", Value: 8.14},
		{TradeDate: "2003-06-30", Value: 9.50},
		{TradeDate: "2003-12-31", Value: 10.81},
		{TradeDate: "2023-12-29", Value: 27.82},
		{TradeDate: "2024-06-28", Value: 33.10},
		{TradeDate: "2024-12-31", Value: 39.30},
		{TradeDate: "2025-12-31", Value: 42.10},
		{TradeDate: "2026-07-03", Value: 36.83},
	}
	rows := ComputeAnnualReturns(points)
	byYear := make(map[int]AnnualReturnRow, len(rows))
	for _, r := range rows {
		byYear[r.Year] = r
	}

	assertYear := func(year int, wantReturn float64, wantStart, wantEnd string, wantPartial bool) {
		t.Helper()
		row, ok := byYear[year]
		if !ok {
			t.Fatalf("missing %d row", year)
		}
		if math.Abs(row.AnnualReturn-wantReturn) > 1e-9 {
			t.Fatalf("%d annual_return=%v want %v", year, row.AnnualReturn, wantReturn)
		}
		if row.StartDate != wantStart || row.EndDate != wantEnd {
			t.Fatalf("%d range=%s..%s want %s..%s", year, row.StartDate, row.EndDate, wantStart, wantEnd)
		}
		if row.IsPartial != wantPartial {
			t.Fatalf("%d is_partial=%v want %v", year, row.IsPartial, wantPartial)
		}
	}

	// Listing year has no prior-year anchor: partial, from the listing day.
	assertYear(2002, 8.14/10.66-1, "2002-04-09", "2002-12-31", true)
	// Complete years anchor on the previous year's last trade day.
	assertYear(2003, 10.81/8.14-1, "2002-12-31", "2003-12-31", false)
	assertYear(2024, 39.30/27.82-1, "2023-12-29", "2024-12-31", false)
	assertYear(2025, 42.10/39.30-1, "2024-12-31", "2025-12-31", false)
	// Current year computes YTD against the 2025 year-end anchor...
	assertYear(2026, 36.83/42.10-1, "2025-12-31", "2026-07-03", false)

	// ...but never enters the complete-year simulation sample.
	years := SelectSimulationYears(points, rows, "2026-07-04")
	for _, y := range years {
		if y.Year == 2026 {
			t.Fatal("current year must not be selected as a complete simulation year")
		}
	}
}

func TestComputeAnnualReturns_YTDUsesYearEndAnchor(t *testing.T) {
	points := []DataPoint{
		{TradeDate: "2025-12-31", Value: 4.753},
		{TradeDate: "2026-01-05", Value: 4.700},
		{TradeDate: "2026-06-09", Value: 4.826},
	}
	rows := ComputeAnnualReturns(points)
	var y2026 *AnnualReturnRow
	for i := range rows {
		if rows[i].Year == 2026 {
			y2026 = &rows[i]
			break
		}
	}
	if y2026 == nil {
		t.Fatal("missing 2026 row")
	}
	want := 4.826/4.753 - 1
	if math.Abs(y2026.AnnualReturn-want) > 1e-9 {
		t.Fatalf("annual_return=%v want %v", y2026.AnnualReturn, want)
	}
	if y2026.StartDate != "2025-12-31" {
		t.Fatalf("start_date=%q want 2025-12-31 anchor", y2026.StartDate)
	}
	if y2026.EndDate != "2026-06-09" {
		t.Fatalf("end_date=%q", y2026.EndDate)
	}
	if y2026.IsPartial {
		t.Fatal("expected complete anchor year row")
	}
}
