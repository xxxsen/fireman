package marketdata

import (
	"math"
	"testing"
)

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
