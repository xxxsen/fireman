package service

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/fireman/fireman/internal/libmetrics"
	"github.com/fireman/fireman/internal/marketdata"
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
	// Keep the list projection exactly in sync with the history just written: a
	// full replace that cleared every point must drop the stale projection, not
	// leave the previous date/returns/eligibility behind (td/058 P1).
	if err := libmetrics.SyncTx(ctx, s.libMetrics, tx, instrumentID, reprocessed.Points); err != nil {
		return wrapRepo("sync library metrics", err)
	}
	return wrapRepo("touch instrument", s.instRepo.TouchUpdated(ctx, tx, instrumentID))
}
