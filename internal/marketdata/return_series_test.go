package marketdata

import "testing"

func dp(date string, value float64) DataPoint {
	return DataPoint{TradeDate: date, Value: value, PointType: "nav", SourceName: "test"}
}

func TestComputeReturnSeriesNormalizesToFirstPoint(t *testing.T) {
	points := []DataPoint{
		dp("2026-03-19", 1.0),
		dp("2026-04-19", 1.05),
		dp("2026-06-19", 1.20),
	}
	got := ComputeReturnSeries(points, "2026-06-19", "all", "nav", "test")
	if got.Status != ReturnSeriesStatusAvailable {
		t.Fatalf("status = %s, want available", got.Status)
	}
	if len(got.Points) != 3 {
		t.Fatalf("points = %d, want 3", len(got.Points))
	}
	if got.Points[0].CumulativeReturn != 0 {
		t.Fatalf("first cumulative = %v, want 0", got.Points[0].CumulativeReturn)
	}
	if diff := got.Points[2].CumulativeReturn - 0.20; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("last cumulative = %v, want 0.20", got.Points[2].CumulativeReturn)
	}
}

func TestComputeReturnSeries3dUsesThreeDayWindow(t *testing.T) {
	points := []DataPoint{
		dp("2026-06-15", 0.90),
		dp("2026-06-17", 1.0),
		dp("2026-06-18", 1.10),
		dp("2026-06-19", 1.21),
	}
	got := ComputeReturnSeries(points, "2026-06-19", "3d", "nav", "test")
	if got.Status != ReturnSeriesStatusAvailable {
		t.Fatalf("status = %s, want available", got.Status)
	}
	if len(got.Points) != 3 {
		t.Fatalf("points = %d, want 3 (points on/after target date)", len(got.Points))
	}
	if got.Points[0].Date != "2026-06-17" {
		t.Fatalf("first point = %s, want 2026-06-17", got.Points[0].Date)
	}
	if diff := got.Points[2].CumulativeReturn - 0.21; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("cumulative = %v, want 0.21", got.Points[2].CumulativeReturn)
	}
}

func TestComputeReturnSeriesInsufficientHistory(t *testing.T) {
	got := ComputeReturnSeries([]DataPoint{dp("2026-06-19", 1.0)}, "2026-06-19", "3d", "nav", "test")
	if got.Status != ReturnSeriesStatusInsufficientHistory {
		t.Fatalf("status = %s, want insufficient_history", got.Status)
	}
	if len(got.Points) != 0 {
		t.Fatalf("points = %d, want 0", len(got.Points))
	}

	gotEmpty := ComputeReturnSeries(nil, "2026-06-19", "3m", "nav", "test")
	if gotEmpty.Status != ReturnSeriesStatusInsufficientHistory || len(gotEmpty.Points) != 0 {
		t.Fatalf("empty input = %+v, want insufficient with no points", gotEmpty)
	}
}

func TestIsValidReturnSeriesRange(t *testing.T) {
	for _, r := range []string{"3d", "1w", "1m", "3m", "6m", "1y", "3y", "5y", "all"} {
		if !IsValidReturnSeriesRange(r) {
			t.Fatalf("range %s should be valid", r)
		}
	}
	if IsValidReturnSeriesRange("1d") {
		t.Fatal("range 1d should be invalid")
	}
}
