package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

func (r *InstrumentFetchRunner) persistFetchedInstrument(
	ctx context.Context,
	payload repository.InstrumentFetchPayload,
	data *marketdata.FetchData,
	processed marketdata.ProcessFetchResult,
	class marketdata.Classification,
) error {
	inst := repository.InstrumentRecord{
		ID: payload.InstrumentID, Code: payload.Code,
		Name:   marketdata.PreferInstrumentName(payload.Code, payload.ResolvedName, data.Name),
		Market: payload.Market, InstrumentType: payload.InstrumentType,
		AssetClass: class.AssetClass, Region: class.Region, Currency: class.Currency,
		ProviderSymbol:     payload.ProviderSymbol,
		ExpenseRatio:       marketdata.ExpenseRatioFromComponents(data.ExpenseRatioComponents),
		ExpenseRatioStatus: data.ExpenseRatioStatus,
		FeeTreatment:       marketdata.FeeTreatmentForType(payload.InstrumentType),
		Status:             "active",
	}
	points := toRepoPoints(payload.InstrumentID, processed.Points)
	annual := toRepoAnnual(payload.InstrumentID, processed.Annual)
	err := fdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		if err := r.marketRepo.UpsertBatch(ctx, tx, payload.InstrumentID, points); err != nil {
			return fmt.Errorf("upsert market data: %w", err)
		}
		if err := r.annualRepo.ReplaceAll(ctx, tx, payload.InstrumentID, annual); err != nil {
			return fmt.Errorf("replace annual returns: %w", err)
		}
		if err := r.instRepo.UpdateAfterFetchTx(ctx, tx, inst); err != nil {
			return fmt.Errorf("update instrument after fetch: %w", err)
		}
		return r.upsertLibraryMetricsTx(ctx, tx, payload.InstrumentID, processed.Points)
	})
	if err != nil {
		_ = r.instRepo.UpdateStatusTx(ctx, nil, payload.InstrumentID, "fetch_failed")
		return fmt.Errorf("persist instrument fetch: %w", err)
	}
	slog.InfoContext(
		ctx, "instrument fetch completed",
		"instrument_id", payload.InstrumentID,
		"points", len(points),
		"source", data.SourceName,
	)
	return nil
}

// upsertLibraryMetricsTx recomputes and persists the asset-library list
// projection (market metadata, simulation eligibility and trailing 1/3/5y
// returns) for the freshly fetched instrument, inside the same transaction that
// stored market_data_points (td/057 P1). It is a no-op when history is empty.
func (r *InstrumentFetchRunner) upsertLibraryMetricsTx(
	ctx context.Context, tx *sql.Tx, instrumentID string, points []marketdata.DataPoint,
) error {
	proj, ok := marketdata.ComputeLibraryProjection(points)
	if !ok {
		return nil
	}
	rec := repository.LibraryMetricsRecord{
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
	}
	if err := r.libMetrics.Upsert(ctx, tx, rec); err != nil {
		return fmt.Errorf("upsert library metrics: %w", err)
	}
	return nil
}
