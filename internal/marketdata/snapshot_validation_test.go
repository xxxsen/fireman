package marketdata

import (
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func TestValidateSimulationSnapshotRejectsMonthlyCountMismatch(t *testing.T) {
	snap := repository.SimulationSnapshot{
		SourceMode:         "akshare_historical",
		QualityStatus:      QualityStatusAvailable,
		MetricsVersion:     MetricsVersionMonthlyLogReturnV1,
		VolatilityMethod:   VolatilityMethodMonthlyLogReturn,
		CompleteYearCount:  2,
		MonthlyReturnCount: 12,
	}
	if err := ValidateSimulationSnapshot(snap); err == nil {
		t.Fatal("expected validation error for monthly count mismatch")
	}
}

func TestValidateSimulationSnapshotRejectsBadMetricsVersion(t *testing.T) {
	snap := repository.SimulationSnapshot{
		SourceMode:         "akshare_historical",
		QualityStatus:      QualityStatusAvailable,
		MetricsVersion:     "legacy_v0",
		VolatilityMethod:   VolatilityMethodMonthlyLogReturn,
		CompleteYearCount:  1,
		MonthlyReturnCount: 12,
	}
	if err := ValidateSimulationSnapshot(snap); err == nil {
		t.Fatal("expected metrics version rejection")
	}
}

func TestValidateSimulationSnapshotAcceptsOneCompleteYear(t *testing.T) {
	snap := repository.SimulationSnapshot{
		SourceMode:         "akshare_historical",
		QualityStatus:      QualityStatusAvailable,
		MetricsVersion:     MetricsVersionMonthlyLogReturnV1,
		VolatilityMethod:   VolatilityMethodMonthlyLogReturn,
		CompleteYearCount:  1,
		MonthlyReturnCount: 12,
	}
	if err := ValidateSimulationSnapshot(snap); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
}

func TestValidateSimulationSnapshotSystemCash(t *testing.T) {
	snap := repository.SimulationSnapshot{
		SourceMode:     "system_cash",
		MetricsVersion: MetricsVersionSystemCashV1,
	}
	if err := ValidateSimulationSnapshot(snap); err != nil {
		t.Fatalf("system cash rejected: %v", err)
	}
}

func TestDetermineHistoryDepthZeroCompleteYears(t *testing.T) {
	if got := DetermineHistoryDepth(0); got != HistoryDepthInsufficient {
		t.Fatalf("got %s want %s", got, HistoryDepthInsufficient)
	}
}

func TestDetermineHistoryDepthMappings(t *testing.T) {
	cases := map[int]string{
		1: HistoryDepthOneYear,
		2: HistoryDepthTwoToFourYears,
		3: HistoryDepthTwoToFourYears,
		5: HistoryDepthFivePlusYears,
	}
	for count, want := range cases {
		if got := DetermineHistoryDepth(count); got != want {
			t.Fatalf("count %d: got %s want %s", count, got, want)
		}
	}
}

func TestEvaluateSimulationEligibilityRequiresMonthlyConsistency(t *testing.T) {
	m := SnapshotMetrics{
		QualityStatus:      QualityStatusAvailable,
		MetricsVersion:     MetricsVersionMonthlyLogReturnV1,
		VolatilityMethod:   VolatilityMethodMonthlyLogReturn,
		CompleteYearCount:  2,
		MonthlyReturnCount: 12,
		CAGRStatus:         MetricStatusAvailable,
		VolatilityStatus:   MetricStatusAvailable,
		DrawdownStatus:     MetricStatusAvailable,
	}
	if EvaluateSimulationEligibility(m, false) {
		t.Fatal("expected ineligible when monthly count mismatches complete years")
	}
}
