package marketdata

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func metricVal(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

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
	if metricVal(m.ModeledAnnualReturn) != metricVal(m.HistoricalCAGR) {
		t.Fatal("modeled annual return must equal historical cagr")
	}
	vol := metricVal(m.AnnualVolatility)
	dd := metricVal(m.MaxDrawdown)
	if vol < 0 || dd < 0 || dd > 1 {
		t.Fatalf("metrics invalid: vol=%v dd=%v", vol, dd)
	}
	if m.MonthlyReturnCount != 48 {
		t.Fatalf("monthly returns %d want 48", m.MonthlyReturnCount)
	}
	if !m.SimulationEligible {
		t.Fatal("expected simulation eligible")
	}
	if m.SourceHash == "" {
		t.Fatal("expected source hash")
	}
}

func TestOneCompleteYearSimulationEligible(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2025})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2026-06-14")
	if len(years) != 1 {
		t.Fatalf("years %d", len(years))
	}
	m := ComputeMetrics(points, years, "adjusted_close", "golden")
	if m.MonthlyReturnCount != 12 {
		t.Fatalf("monthly count %d", m.MonthlyReturnCount)
	}
	if !m.SimulationEligible {
		t.Fatal("one complete year should be simulation eligible")
	}
	if m.HistoryDepth != HistoryDepthOneYear {
		t.Fatalf("history depth %s", m.HistoryDepth)
	}
}

func TestLabelChangeDoesNotAffectMetrics(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2020, 2021, 2022, 2023})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2025-12-31")
	m1 := ComputeMetrics(points, years, "adjusted_close", "source_a")
	m2 := ComputeMetrics(points, years, "nav", "source_b")
	if math.Abs(metricVal(m1.HistoricalCAGR)-metricVal(m2.HistoricalCAGR)) > 1e-12 {
		t.Fatalf("cagr changed with labels: %v vs %v", m1.HistoricalCAGR, m2.HistoricalCAGR)
	}
	if math.Abs(metricVal(m1.AnnualVolatility)-metricVal(m2.AnnualVolatility)) > 1e-12 {
		t.Fatal("volatility changed with labels")
	}
	if math.Abs(metricVal(m1.MaxDrawdown)-metricVal(m2.MaxDrawdown)) > 1e-12 {
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
	if !mA.SimulationEligible || !mB.SimulationEligible {
		t.Fatalf("eligible A=%v B=%v", mA.SimulationEligible, mB.SimulationEligible)
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

func TestTwoCompleteYearsMonthlySampleCount(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2024, 2025})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2026-06-14")
	m := ComputeMetrics(points, years, "adjusted_close", "golden")
	if m.MonthlyReturnCount != 24 {
		t.Fatalf("monthly count %d want 24", m.MonthlyReturnCount)
	}
}

func TestMissingMonthBreaksMonthlyReturns(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2025})
	filtered := make([]DataPoint, 0, len(points))
	for _, p := range points {
		if strings.HasPrefix(p.TradeDate, "2025-03-") {
			continue
		}
		filtered = append(filtered, p)
	}
	annual := ComputeAnnualReturns(filtered)
	years := SelectSimulationYears(filtered, annual, "2026-06-14")
	monthly := BuildMonthlyReturns(filtered, years)
	if len(monthly) >= 12 {
		t.Fatalf("expected broken monthly series, got %d", len(monthly))
	}
	m := ComputeMetrics(filtered, years, "adjusted_close", "golden")
	if m.VolatilityStatus == MetricStatusAvailable {
		t.Fatalf("vol status %s", m.VolatilityStatus)
	}
}

func TestSegmentedYearsMonthlyReturns(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2020, 2022})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2026-06-14")
	monthly := BuildMonthlyReturns(points, years)
	if len(monthly) != 24 {
		t.Fatalf("segment monthly count %d want 24", len(monthly))
	}
}

func TestZeroVolatilityFlatSeries(t *testing.T) {
	points := buildFlatHistoryForYears([]int{2025})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2026-06-14")
	m := ComputeMetrics(points, years, "adjusted_close", "golden")
	if m.VolatilityStatus != MetricStatusAvailable {
		t.Fatalf("vol status %s", m.VolatilityStatus)
	}
	if metricVal(m.AnnualVolatility) != 0 {
		t.Fatalf("vol %v want 0", m.AnnualVolatility)
	}
}

func buildFlatHistoryForYears(years []int) []DataPoint {
	var points []DataPoint
	value := 100.0
	for _, y := range years {
		anchorDate := fmt.Sprintf("%d-12-31", y-1)
		points = append(points, DataPoint{TradeDate: anchorDate, Value: value})
		for month := 1; month <= 12; month++ {
			for day := 1; day <= 11; day++ {
				d := time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.UTC)
				points = append(points, DataPoint{TradeDate: d.Format("2006-01-02"), Value: value})
			}
		}
	}
	return points
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
