package marketdata

import "testing"

func TestCleanPointsDedupesSameDate(t *testing.T) {
	in := []HistoricalPoint{
		{Date: "2024-01-02", Value: 10},
		{Date: "2024-01-01", Value: 9},
		{Date: "2024-01-02", Value: 11},
	}
	out := CleanPoints(in)
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}
	if out[1].Value != 11 {
		t.Fatalf("expected last value kept, got %v", out[1].Value)
	}
}
