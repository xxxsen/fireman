package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/libmetrics"
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
		return libmetrics.SyncTx(ctx, r.libMetrics, tx, payload.InstrumentID, processed.Points)
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
