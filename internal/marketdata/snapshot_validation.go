package marketdata

import (
	"errors"
	"fmt"

	"github.com/fireman/fireman/internal/repository"
)

var (
	ErrSnapshotQualityUnavailable   = errors.New("snapshot quality_status is not available")
	ErrSnapshotMetricsVersion       = errors.New("snapshot metrics_version mismatch")
	ErrSnapshotVolatilityMethod     = errors.New("snapshot volatility_method mismatch")
	ErrSnapshotCompleteYears        = errors.New("snapshot complete_year_count below minimum")
	ErrSnapshotMonthlyCountMismatch = errors.New("snapshot monthly_return_count inconsistent with complete years")
)

// ValidateSimulationSnapshot checks frozen snapshot semantics for non-cash simulation assets.
func ValidateSimulationSnapshot(snap repository.SimulationSnapshot) error {
	if snap.SourceMode == "system_cash" {
		if snap.MetricsVersion != MetricsVersionSystemCashV1 {
			return fmt.Errorf("%w: got %q", ErrSnapshotMetricsVersion, snap.MetricsVersion)
		}
		return nil
	}
	if snap.QualityStatus != QualityStatusAvailable {
		return fmt.Errorf("%w: got %q", ErrSnapshotQualityUnavailable, snap.QualityStatus)
	}
	if snap.MetricsVersion != MetricsVersionMonthlyLogReturnV1 {
		return fmt.Errorf("%w: got %q", ErrSnapshotMetricsVersion, snap.MetricsVersion)
	}
	if snap.VolatilityMethod != VolatilityMethodMonthlyLogReturn {
		return fmt.Errorf("%w: got %q", ErrSnapshotVolatilityMethod, snap.VolatilityMethod)
	}
	if snap.CompleteYearCount < 1 {
		return fmt.Errorf("%w: got %d", ErrSnapshotCompleteYears, snap.CompleteYearCount)
	}
	expectedMonthly := snap.CompleteYearCount * 12
	if snap.MonthlyReturnCount != expectedMonthly {
		return fmt.Errorf(
			"%w: complete_year_count=%d monthly_return_count=%d",
			ErrSnapshotMonthlyCountMismatch, snap.CompleteYearCount, snap.MonthlyReturnCount,
		)
	}
	return nil
}
