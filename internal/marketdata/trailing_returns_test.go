package marketdata

import (
	"testing"
	"time"
)

func TestTrailingReturnsSharedEndDate(t *testing.T) {
	points := buildTrailingPoints()
	tr := ComputeTrailingReturns(points, "2026-06-12", "adjusted_close", "test")
	if tr.AsOfDate != "2026-06-12" {
		t.Fatalf("top as_of_date %s", tr.AsOfDate)
	}
	for _, key := range trailingPeriodOrder {
		p := tr.Periods[key]
		if p.EndDate != "2026-06-12" {
			t.Fatalf("%s end_date %s", key, p.EndDate)
		}
	}
}

func TestTrailingReturnsWeekendUsesLastTradeDate(t *testing.T) {
	points := []DataPoint{
		{TradeDate: "2026-06-10", Value: 100},
		{TradeDate: "2026-06-12", Value: 105},
	}
	tr := ComputeTrailingReturns(points, "2026-06-14", "adjusted_close", "test")
	if tr.AsOfDate != "2026-06-12" {
		t.Fatalf("as_of_date %s want 2026-06-12", tr.AsOfDate)
	}
	if tr.Periods["1m"].EndDate != "2026-06-12" {
		t.Fatalf("period end_date %s", tr.Periods["1m"].EndDate)
	}
}

func TestTrailingMonthClip(t *testing.T) {
	end := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	target := subtractTrailingOffset(end, trailingPeriodOffsets["1m"])
	if target.Format("2006-01-02") != "2026-02-28" {
		t.Fatalf("got %s", target.Format("2006-01-02"))
	}
	leap := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	yr := subtractTrailingOffset(leap, trailingPeriodOffsets["1y"])
	if yr.Format("2006-01-02") != "2023-02-28" {
		t.Fatalf("got %s", yr.Format("2006-01-02"))
	}
}

func TestTrailingStartPointTooStale(t *testing.T) {
	points := []DataPoint{
		{TradeDate: "2020-01-01", Value: 100},
		{TradeDate: "2026-06-12", Value: 150},
	}
	tr := ComputeTrailingReturns(points, "2026-06-12", "adjusted_close", "test")
	if tr.Periods["1y"].Status != TrailingStatusStartPointTooStale {
		t.Fatalf("status %s", tr.Periods["1y"].Status)
	}
}

func TestTrailingTenDayTolerance(t *testing.T) {
	points := []DataPoint{
		{TradeDate: "2025-06-03", Value: 100},
		{TradeDate: "2026-06-12", Value: 110},
	}
	tr := ComputeTrailingReturns(points, "2026-06-12", "adjusted_close", "test")
	if tr.Periods["1y"].Status != TrailingStatusAvailable {
		t.Fatalf("status %s", tr.Periods["1y"].Status)
	}
	stale := []DataPoint{
		{TradeDate: "2025-05-01", Value: 100},
		{TradeDate: "2026-06-12", Value: 110},
	}
	trStale := ComputeTrailingReturns(stale, "2026-06-12", "adjusted_close", "test")
	if trStale.Periods["1y"].Status != TrailingStatusStartPointTooStale {
		t.Fatalf("status %s", trStale.Periods["1y"].Status)
	}
}

func TestTrailingInsufficientHistory(t *testing.T) {
	tr := ComputeTrailingReturns(nil, "2026-06-12", "adjusted_close", "test")
	if tr.Periods["1m"].Status != TrailingStatusInsufficientHistory {
		t.Fatalf("status %s", tr.Periods["1m"].Status)
	}
}

// The asset-library list view annualizes 1y/3y/5y. 3y/5y must equal
// the detail-page annualized values; 1y is annualized from its own cumulative
// return. Insufficient windows surface as nil rather than zero.
func TestComputeListTrailingReturnsAnnualizes(t *testing.T) {
	points := buildTrailingPoints() // ~6.5y of ~+0.01%/day compounding
	detail := ComputeTrailingReturns(points, "2026-06-12", "adjusted_close", "test")
	list := ComputeListTrailingReturns(points, "2026-06-12", "adjusted_close", "test")

	if list.AsOfDate != detail.AsOfDate {
		t.Fatalf("as_of_date list=%s detail=%s", list.AsOfDate, detail.AsOfDate)
	}
	if list.ThreeYear == nil || detail.Periods["3y"].AnnualizedReturn == nil ||
		*list.ThreeYear != *detail.Periods["3y"].AnnualizedReturn {
		t.Fatalf("3y annualized mismatch: list=%v detail=%v", list.ThreeYear, detail.Periods["3y"].AnnualizedReturn)
	}
	if list.FiveYear == nil || detail.Periods["5y"].AnnualizedReturn == nil ||
		*list.FiveYear != *detail.Periods["5y"].AnnualizedReturn {
		t.Fatalf("5y annualized mismatch: list=%v detail=%v", list.FiveYear, detail.Periods["5y"].AnnualizedReturn)
	}
	if list.OneYear == nil {
		t.Fatalf("1y annualized should be computable")
	}
	// ~0.01%/day over a year annualizes to roughly e^(0.0001*365)-1 ≈ 3.7%.
	if *list.OneYear < 0.02 || *list.OneYear > 0.06 {
		t.Fatalf("1y annualized out of expected band: %f", *list.OneYear)
	}
}

func TestComputeListTrailingReturnsNilWhenInsufficient(t *testing.T) {
	// Only ~12 months of history: 1y available, 3y/5y insufficient -> nil.
	points := []DataPoint{
		{TradeDate: "2025-06-09", Value: 100},
		{TradeDate: "2026-06-12", Value: 110},
	}
	list := ComputeListTrailingReturns(points, "2026-06-12", "adjusted_close", "test")
	if list.OneYear == nil {
		t.Fatalf("1y should be available")
	}
	if list.ThreeYear != nil || list.FiveYear != nil {
		t.Fatalf("3y/5y should be nil: 3y=%v 5y=%v", list.ThreeYear, list.FiveYear)
	}
}

func buildTrailingPoints() []DataPoint {
	var points []DataPoint
	value := 100.0
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		value *= 1.0001
		points = append(points, DataPoint{TradeDate: d.Format("2006-01-02"), Value: value})
	}
	return points
}

func buildTrailingPointsN(days int) []DataPoint {
	var points []DataPoint
	value := 100.0
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		value *= 1.0001
		points = append(points, DataPoint{TradeDate: d.Format("2006-01-02"), Value: value})
	}
	return points
}

func BenchmarkComputeTrailingReturns(b *testing.B) {
	points := buildTrailingPointsN(5000)
	asOf := points[len(points)-1].TradeDate
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeTrailingReturns(points, asOf, "adjusted_close", "bench")
	}
}

func BenchmarkComputeTrailingReturns_10k(b *testing.B) {
	points := buildTrailingPointsN(10000)
	asOf := points[len(points)-1].TradeDate
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeTrailingReturns(points, asOf, "adjusted_close", "bench")
	}
}

func TestTrailingReturnsScalingIsSubQuadratic(t *testing.T) {
	small := buildTrailingPointsN(2000)
	large := buildTrailingPointsN(4000)
	asOfSmall := small[len(small)-1].TradeDate
	asOfLarge := large[len(large)-1].TradeDate

	start := time.Now()
	for i := 0; i < 50; i++ {
		ComputeTrailingReturns(small, asOfSmall, "adjusted_close", "scale")
	}
	smallElapsed := time.Since(start)

	start = time.Now()
	for i := 0; i < 50; i++ {
		ComputeTrailingReturns(large, asOfLarge, "adjusted_close", "scale")
	}
	largeElapsed := time.Since(start)

	ratio := float64(largeElapsed) / float64(smallElapsed)
	if ratio > 3.5 {
		t.Fatalf("2x data took %.2fx time (small=%s large=%s), likely worse than O(n log n)",
			ratio, smallElapsed, largeElapsed)
	}
}
