package service

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

func refreshOverlapStart(fullReplace bool, lastDate string) *string {
	if fullReplace || lastDate == "" {
		return nil
	}
	overlap, err := marketdata.RefreshStartDate(lastDate)
	if err != nil {
		return nil
	}
	return &overlap
}

func mergeRefreshProcessedData(
	ctx context.Context,
	instrumentID string,
	existing []marketdata.DataPoint,
	processed marketdata.ProcessFetchResult,
	force bool,
	end string,
) marketdata.ProcessFetchResult {
	fullReplace := marketdata.ShouldFullReplaceOnRefresh(force, existing, processed.SourceName)
	if fullReplace {
		if force {
			slog.InfoContext(
				ctx, "instrument full history replace on force refresh",
				"instrument_id", instrumentID,
				"source", processed.SourceName,
				"points", len(processed.Points),
			)
		} else {
			slog.InfoContext(
				ctx, "instrument full history replace on source change",
				"instrument_id", instrumentID,
				"from", marketdata.DominantSourceName(existing),
				"to", processed.SourceName,
			)
		}
		return processed
	}
	merged := marketdata.MergeRefreshedPoints(existing, processed.Points)
	return marketdata.ProcessProviderData(&marketdata.FetchData{
		Points: toHistorical(merged), PointType: processed.PointType, SourceName: processed.SourceName,
	}, end)
}

func persistRefreshMarketDataTx(
	ctx context.Context,
	s *InstrumentService,
	tx *sql.Tx,
	instrumentID string,
	fullReplace bool,
	reprocessed marketdata.ProcessFetchResult,
	shouldUpdateName bool,
	newName string,
) error {
	if fullReplace {
		if err := s.marketRepo.DeleteAllTx(ctx, tx, instrumentID); err != nil {
			return wrapRepo("delete market data", err)
		}
	}
	if err := s.marketRepo.UpsertBatch(ctx, tx, instrumentID, toRepoPoints(instrumentID,
		reprocessed.Points)); err != nil {
		return wrapRepo("upsert market data", err)
	}
	annual := toRepoAnnual(instrumentID, reprocessed.Annual)
	if err := s.annualRepo.ReplaceAll(ctx, tx, instrumentID, annual); err != nil {
		return wrapRepo("replace annual returns", err)
	}
	if shouldUpdateName {
		if err := s.instRepo.UpdateNameTx(ctx, tx, instrumentID, newName); err != nil {
			return wrapRepo("update instrument name", err)
		}
	}
	if err := upsertLibraryMetricsTx(ctx, s.libMetrics, tx, instrumentID, reprocessed.Points); err != nil {
		return err
	}
	return wrapRepo("touch instrument", s.instRepo.TouchUpdated(ctx, tx, instrumentID))
}

// upsertLibraryMetricsTx recomputes and persists the asset-library list
// projection for instrumentID from its full cleaned history, inside the same
// transaction that stored market_data_points (td/057 P1). It is a no-op when the
// history is empty so callers never write an empty projection.
func upsertLibraryMetricsTx(
	ctx context.Context,
	repo *repository.InstrumentLibraryMetricsRepo,
	tx *sql.Tx,
	instrumentID string,
	points []marketdata.DataPoint,
) error {
	rec, ok := buildLibraryMetricsRecord(instrumentID, points)
	if !ok {
		return nil
	}
	return wrapRepo("upsert library metrics", repo.Upsert(ctx, tx, rec))
}

// buildLibraryMetricsRecord maps a computed marketdata projection onto the
// repository row. ok is false when there is no usable history.
func buildLibraryMetricsRecord(
	instrumentID string, points []marketdata.DataPoint,
) (repository.LibraryMetricsRecord, bool) {
	proj, ok := marketdata.ComputeLibraryProjection(points)
	if !ok {
		return repository.LibraryMetricsRecord{}, false
	}
	return repository.LibraryMetricsRecord{
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
	}, true
}
