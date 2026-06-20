package marketdata

import "testing"

func dailyPoints(start, end string, daily float64) []DataPoint {
	startT := parseDate(start)
	endT := parseDate(end)
	var pts []DataPoint
	value := 100.0
	for d := startT; !d.After(endT); d = d.AddDate(0, 0, 1) {
		pts = append(pts, DataPoint{
			TradeDate: formatDate(d), Value: value,
			PointType: "adjusted_close", SourceName: "lib_src", FetchedAt: 1,
		})
		value *= 1 + daily
	}
	return pts
}

// TestComputeLibraryProjectionStaleAsOf is the core td/057 P1 guard: trailing
// windows end at the instrument's own last trade date (data_as_of), not the
// server's current date. A 停更 instrument with full 2018-12-28..2024-01-01
// history still reports its near-5y annualized return, and that value matches the
// detail page's ComputeTrailingReturns for the same 2024-01-01 as-of date.
func TestComputeLibraryProjectionStaleAsOf(t *testing.T) {
	points := dailyPoints("2018-12-28", "2024-01-01", 0.0003)

	proj, ok := ComputeLibraryProjection(points)
	if !ok {
		t.Fatal("expected projection for non-empty history")
	}
	if proj.DataAsOf != "2024-01-01" {
		t.Fatalf("data_as_of = %q, want 2024-01-01", proj.DataAsOf)
	}
	if proj.SourceName != "lib_src" || proj.PointType != "adjusted_close" {
		t.Fatalf("projection metadata = %q/%q", proj.SourceName, proj.PointType)
	}
	if proj.Trailing.AsOfDate != "2024-01-01" {
		t.Fatalf("trailing as_of = %q, want 2024-01-01", proj.Trailing.AsOfDate)
	}
	if proj.Trailing.OneYear == nil || proj.Trailing.ThreeYear == nil || proj.Trailing.FiveYear == nil {
		t.Fatalf("stale instrument must still expose 1y/3y/5y: %+v", proj.Trailing)
	}

	// Must equal the detail-page computation for the same effective as-of date.
	detail := ComputeListTrailingReturns(points, "2024-01-01", "adjusted_close", "lib_src")
	if detail.FiveYear == nil || *detail.FiveYear != *proj.Trailing.FiveYear {
		t.Fatalf("projection 5y %v must match detail 5y %v", proj.Trailing.FiveYear, detail.FiveYear)
	}
	if *detail.ThreeYear != *proj.Trailing.ThreeYear {
		t.Fatalf("projection 3y %v must match detail 3y %v", proj.Trailing.ThreeYear, detail.ThreeYear)
	}
}

// TestComputeLibraryProjectionEmpty returns ok=false for empty history so callers
// write no projection (the list renders "—").
func TestComputeLibraryProjectionEmpty(t *testing.T) {
	if _, ok := ComputeLibraryProjection(nil); ok {
		t.Fatal("expected ok=false for empty history")
	}
}

// TestComputeLibraryProjectionShortHistory exposes near-1y but leaves near-5y nil
// when only ~2y of history exists, while still producing a projection.
func TestComputeLibraryProjectionShortHistory(t *testing.T) {
	points := dailyPoints("2022-01-03", "2024-01-01", 0.0003)
	proj, ok := ComputeLibraryProjection(points)
	if !ok {
		t.Fatal("expected projection for non-empty history")
	}
	if proj.Trailing.OneYear == nil {
		t.Fatalf("near-1y should be available: %+v", proj.Trailing)
	}
	if proj.Trailing.FiveYear != nil {
		t.Fatalf("near-5y should be nil with ~2y history: %+v", proj.Trailing.FiveYear)
	}
}
