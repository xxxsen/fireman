package marketdata

import "testing"

func TestFindPriceDiscontinuityThresholds(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		found bool
	}{
		{name: "below threshold", value: 124.99, found: false},
		{name: "above threshold", value: 125.01, found: true},
		{name: "two for one split", value: 50, found: true},
		{name: "510310 consolidation", value: 200.94, found: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			points := []DataPoint{
				{TradeDate: "2024-09-20", Value: 100},
				{TradeDate: "2024-09-23", Value: tt.value},
			}
			got, found := FindPriceDiscontinuity(points, CNAdjustedPriceMaxDailyMove)
			if found != tt.found {
				t.Fatalf("found=%v, want %v (%+v)", found, tt.found, got)
			}
			if found && (got.PreviousDate != "2024-09-20" || got.Date != "2024-09-23") {
				t.Fatalf("wrong discontinuity dates: %+v", got)
			}
		})
	}
}
