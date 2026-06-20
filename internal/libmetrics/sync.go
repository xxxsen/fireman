// Package libmetrics is the single source of truth for keeping the
// instrument_library_metrics projection in lock-step with stored market data.
// Both the async import/fetch path (internal/jobs) and the manual refresh path
// (internal/service) call SyncTx inside the transaction that writes
// market_data_points, so the projection can never diverge from the history a
// detail page or plan snapshot would compute.
package libmetrics

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// SyncTx recomputes the asset-library list projection from an instrument's full
// cleaned history and makes instrument_library_metrics exactly match the
// market_data_points written in the same transaction:
//
//   - non-empty history -> upsert the recomputed projection;
//   - empty history (e.g. a full replace cleared every point) -> delete the
//     stale projection row instead of leaving it behind.
//
// Empty history is never a silent no-op. When the surrounding transaction rolls
// back, neither the upsert nor the delete is applied, so a failed refresh keeps
// the previous valid projection.
func SyncTx(
	ctx context.Context,
	repo *repository.InstrumentLibraryMetricsRepo,
	tx *sql.Tx,
	instrumentID string,
	points []marketdata.DataPoint,
) error {
	proj, ok := marketdata.ComputeLibraryProjection(points)
	if !ok {
		if err := repo.DeleteTx(ctx, tx, instrumentID); err != nil {
			return fmt.Errorf("delete library metrics projection: %w", err)
		}
		return nil
	}
	if err := repo.Upsert(ctx, tx, repository.LibraryMetricsRecord{
		InstrumentID:        instrumentID,
		DataAsOf:            proj.DataAsOf,
		DataSourceName:      proj.SourceName,
		PointType:           proj.PointType,
		QualityStatus:       proj.QualityStatus,
		SimulationEligible:  proj.SimulationEligible,
		HistoryDepth:        proj.HistoryDepth,
		CompleteYearCount:   proj.CompleteYearCount,
		MonthlyReturnCount:  proj.MonthlyReturnCount,
		MetricsVersion:      proj.MetricsVersion,
		WarningsJSON:        proj.WarningsJSON(),
		TrailingAsOf:        proj.Trailing.AsOfDate,
		OneYearAnnualized:   proj.Trailing.OneYear,
		ThreeYearAnnualized: proj.Trailing.ThreeYear,
		FiveYearAnnualized:  proj.Trailing.FiveYear,
	}); err != nil {
		return fmt.Errorf("upsert library metrics projection: %w", err)
	}
	return nil
}
