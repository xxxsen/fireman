package marketdata

import (
	"testing"
)

func TestHKAssetTwentyCompleteYearsGoldenMetrics(t *testing.T) {
	years := make([]int, 20)
	for i := range years {
		years[i] = 2005 + i
	}
	points := buildSyntheticHistoryForYears(years)
	annual := ComputeAnnualReturns(points)
	selected := SelectSimulationYears(points, annual, "2026-06-09")
	if len(selected) != 20 {
		t.Fatalf("expected 20 complete simulation years, got %d", len(selected))
	}

	m := ComputeMetrics(points, selected, "adjusted_close", "golden_hk")
	if m.CompleteYearCount != 20 {
		t.Fatalf("complete years=%d want 20", m.CompleteYearCount)
	}
	if m.QualityStatus != "available" {
		t.Fatalf("quality=%s want available", m.QualityStatus)
	}
	if metricVal(m.ModeledAnnualReturn) != metricVal(m.HistoricalCAGR) {
		t.Fatal("modeled annual return must equal historical cagr")
	}
	if m.SourceHash == "" {
		t.Fatal("expected source hash")
	}
	if m.MonthlyReturnCount != 240 {
		t.Fatalf("monthly count %d", m.MonthlyReturnCount)
	}

	set := BuildSnapshotPointSet(points, selected, "adjusted_close", "golden_hk")
	if m.SourceHash != ComputeMetricsSourceHash(set, "adjusted_close", "golden_hk", selected, MetricsVersionMonthlyLogReturnV1) {
		t.Fatal("HK golden source_hash must match canonical hash of snapshot point set")
	}
}

func TestHKUserClassificationForeign(t *testing.T) {
	// Classification comes from the user's plan holdings only; the provider
	// never returns a FIRE asset class for HK instruments.
	cls, err := UserClassification("HK", "hk_stock", "equity", "foreign", "HKD")
	if err != nil {
		t.Fatal(err)
	}
	if cls.Region != "foreign" {
		t.Fatalf("region=%s want foreign", cls.Region)
	}
	if cls.Currency != "HKD" {
		t.Fatalf("currency=%s want HKD", cls.Currency)
	}
}
