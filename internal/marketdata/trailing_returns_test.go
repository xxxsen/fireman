package marketdata

import (
	"testing"
	"time"
)

func TestTrailingReturnsSharedEndDate(t *testing.T) {
	points := buildTrailingPoints()
	tr := ComputeTrailingReturns(points, "2026-06-12", "adjusted_close", "test")
	for _, key := range trailingPeriodOrder {
		p := tr.Periods[key]
		if p.EndDate != "2026-06-12" {
			t.Fatalf("%s end_date %s", key, p.EndDate)
		}
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
