package marketdata

import (
	"strings"
	"testing"
)

func TestSnapshotPointSetExcludesGapYears(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2018, 2019, 2021, 2022})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2026-06-09")
	set := BuildSnapshotPointSet(points, years, "adjusted_close", "test")
	if len(set) == 0 {
		t.Fatal("expected points")
	}
	for _, p := range set {
		y := yearOf(p.TradeDate)
		if y == 2020 && !strings.HasSuffix(p.TradeDate, "-12-31") {
			t.Fatalf("gap year 2020 daily point included: %s", p.TradeDate)
		}
	}
}

func TestSecondSegmentAnchorAffectsSourceHash(t *testing.T) {
	years := []int{2010, 2011, 2013, 2014}
	pointsA := buildSyntheticHistoryForYears(years)
	pointsB := buildSyntheticHistoryForYears(years)
	// Change anchor before second segment (2013)
	for i := range pointsB {
		if pointsB[i].TradeDate == "2012-12-31" {
			pointsB[i].Value *= 1.05
		}
	}
	annualA := ComputeAnnualReturns(pointsA)
	annualB := ComputeAnnualReturns(pointsB)
	simA := SelectSimulationYears(pointsA, annualA, "2026-06-09")
	simB := SelectSimulationYears(pointsB, annualB, "2026-06-09")
	mA := ComputeMetrics(pointsA, simA, "adjusted_close", "test")
	mB := ComputeMetrics(pointsB, simB, "adjusted_close", "test")
	if mA.SourceHash == mB.SourceHash {
		t.Fatal("source hash should change when second segment anchor changes")
	}
}

func TestIncompleteYearDailyChangeIgnored(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2020, 2021, 2022, 2023})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2026-06-09")
	m1 := ComputeMetrics(points, years, "adjusted_close", "test")
	points2 := append([]DataPoint{}, points...)
	for i := range points2 {
		if yearOf(points2[i].TradeDate) == 2024 {
			points2[i].Value *= 2
		}
	}
	m2 := ComputeMetrics(points2, years, "adjusted_close", "test")
	if m1.DailyObservationCount != m2.DailyObservationCount || m1.SourceHash != m2.SourceHash {
		t.Fatal("incomplete year daily change should not affect snapshot metrics")
	}
}

func TestSourceHashMatchesPointSet(t *testing.T) {
	points := buildSyntheticHistoryForYears([]int{2020, 2021, 2022, 2023})
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, "2026-06-09")
	set := BuildSnapshotPointSet(points, years, "adjusted_close", "golden")
	m := ComputeMetrics(points, years, "adjusted_close", "golden")
	if m.SourceHash != ComputeMetricsSourceHash(set, "adjusted_close", "golden", years, MetricsVersionMonthlyLogReturnV1) {
		t.Fatal("source_hash must match canonical hash of snapshot point set")
	}
}
