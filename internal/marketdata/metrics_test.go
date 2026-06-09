package marketdata

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestMetricsGoldenCAGRVolatilityDrawdown(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2020, 2021, 2022, 2023})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2026-06-09")
	if len(years) < 4 {
		t.Fatalf("expected >=4 simulation years, got %d", len(years))
	}
	m := ComputeMetrics(points, years, "adjusted_close", "golden")
	if m.CompleteYearCount < 4 {
		t.Fatalf("complete years %d", m.CompleteYearCount)
	}
	if m.ModeledAnnualReturn != m.HistoricalCAGR {
		t.Fatal("modeled annual return must equal historical cagr")
	}
	if m.AnnualVolatility < 0 || m.MaxDrawdown < 0 || m.MaxDrawdown > 1 {
		t.Fatalf("metrics invalid: vol=%v dd=%v", m.AnnualVolatility, m.MaxDrawdown)
	}
	if m.SourceHash == "" {
		t.Fatal("expected source hash")
	}
}

func TestLabelChangeDoesNotAffectMetrics(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2020, 2021, 2022, 2023})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2025-12-31")
	m1 := ComputeMetrics(points, years, "adjusted_close", "source_a")
	m2 := ComputeMetrics(points, years, "nav", "source_b")
	if math.Abs(m1.HistoricalCAGR-m2.HistoricalCAGR) > 1e-12 {
		t.Fatalf("cagr changed with labels: %v vs %v", m1.HistoricalCAGR, m2.HistoricalCAGR)
	}
	if math.Abs(m1.AnnualVolatility-m2.AnnualVolatility) > 1e-12 {
		t.Fatal("volatility changed with labels")
	}
	if math.Abs(m1.MaxDrawdown-m2.MaxDrawdown) > 1e-12 {
		t.Fatal("drawdown changed with labels")
	}
}

func TestNonOverlappingFundYearsStillSimulateIndependently(t *testing.T) {
	fundA := buildSyntheticHistoryForYears([]int{2018, 2019, 2020})
	fundB := buildSyntheticHistoryForYears([]int{2021, 2022, 2023})

	annualA := ComputeAnnualReturns(fundA)
	annualB := ComputeAnnualReturns(fundB)
	yearsA := SelectSimulationYears(fundA, annualA, "2026-06-09")
	yearsB := SelectSimulationYears(fundB, annualB, "2026-06-09")
	if len(yearsA) < 3 || len(yearsB) < 3 {
		t.Fatal("each fund should have independent complete years")
	}
	mA := ComputeMetrics(fundA, yearsA, "adjusted_close", "a")
	mB := ComputeMetrics(fundB, yearsB, "adjusted_close", "b")
	if mA.QualityStatus != "available" || mB.QualityStatus != "available" {
		t.Fatalf("quality A=%s B=%s", mA.QualityStatus, mB.QualityStatus)
	}
}

func TestIncompleteYearsExcluded(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2020, 2021, 2022, 2025})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2025-06-09")
	for _, y := range years {
		if y.Year >= 2025 {
			t.Fatalf("incomplete/current year included: %d", y.Year)
		}
	}
}

func TestDetectDailyAnomaly(t *testing.T) {
	points := []DataPoint{
		{TradeDate: "2020-01-01", Value: 1},
		{TradeDate: "2020-01-02", Value: 2.5},
	}
	if !DetectDailyAnomaly(points) {
		t.Fatal("expected anomaly")
	}
}

func buildSyntheticHistoryForYears(years []int) []DataPoint {
	var points []DataPoint
	value := 100.0
	for _, y := range years {
		anchorDate := fmt.Sprintf("%d-12-31", y-1)
		points = append(points, DataPoint{TradeDate: anchorDate, Value: value})
		for month := 1; month <= 12; month++ {
			for day := 1; day <= 11; day++ {
				value *= 1.0005
				d := time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.UTC)
				points = append(points, DataPoint{TradeDate: d.Format("2006-01-02"), Value: value})
			}
		}
	}
	return points
}
