package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

type codedError struct {
	code string
}

func (e *codedError) Error() string { return e.code }

func newCodedError(code string) error {
	return &codedError{code: code}
}

func errorCode(err error) (string, bool) {
	var ce *codedError
	if errors.As(err, &ce) {
		return ce.code, true
	}
	return "", false
}

// InstrumentFetchRunner executes background instrument history fetches.
type InstrumentFetchRunner struct {
	db         *sql.DB
	instRepo   *repository.InstrumentRepo
	marketRepo *repository.MarketDataRepo
	annualRepo *repository.AnnualReturnsRepo
	provider   *marketdata.ProviderClient
}

func NewInstrumentFetchRunner(
	db *sql.DB,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
	annualRepo *repository.AnnualReturnsRepo,
	provider *marketdata.ProviderClient,
) *InstrumentFetchRunner {
	return &InstrumentFetchRunner{
		db: db, instRepo: instRepo, marketRepo: marketRepo,
		annualRepo: annualRepo, provider: provider,
	}
}

func (r *InstrumentFetchRunner) Run(ctx context.Context, job repository.Job, progress func(done, total int, phase string)) error {
	var payload repository.InstrumentFetchPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return err
	}
	progress(0, 1, "fetching_history")

	end := time.Now().Format("2006-01-02")
	fetchReq := marketdata.FetchRequest{
		Market: payload.Market, InstrumentType: payload.InstrumentType,
		SourceCode: payload.ProviderSymbol, EndDate: end,
		AdjustPolicy: payload.AdjustPolicy,
	}
	data, err := r.provider.Fetch(ctx, fetchReq)
	if err != nil {
		_ = r.instRepo.UpdateStatusTx(ctx, nil, payload.InstrumentID, "fetch_failed")
		return err
	}
	class, err := marketdata.ResolveClassification(payload.Market, payload.InstrumentType, data)
	if err != nil {
		_ = r.instRepo.UpdateStatusTx(ctx, nil, payload.InstrumentID, "fetch_failed")
		return err
	}
	processed := marketdata.ProcessProviderData(data, end)
	if processed.HasAnomaly {
		_ = r.instRepo.UpdateStatusTx(ctx, nil, payload.InstrumentID, "fetch_failed")
		return newCodedError("provider_data_anomaly")
	}

	inst := repository.InstrumentRecord{
		ID: payload.InstrumentID, Code: payload.Code, Name: data.Name,
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

	err = fdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		if err := r.marketRepo.UpsertBatch(ctx, tx, payload.InstrumentID, points); err != nil {
			return err
		}
		if err := r.annualRepo.ReplaceAll(ctx, tx, payload.InstrumentID, annual); err != nil {
			return err
		}
		return r.instRepo.UpdateAfterFetchTx(ctx, tx, inst)
	})
	if err != nil {
		_ = r.instRepo.UpdateStatusTx(ctx, nil, payload.InstrumentID, "fetch_failed")
		return err
	}
	progress(1, 1, "completed")
	slog.InfoContext(ctx, "instrument fetch completed",
		"instrument_id", payload.InstrumentID,
		"points", len(points),
		"source", data.SourceName,
	)
	return nil
}

func toRepoPoints(instrumentID string, points []marketdata.DataPoint) []repository.MarketDataPoint {
	out := make([]repository.MarketDataPoint, len(points))
	for i, p := range points {
		out[i] = repository.MarketDataPoint{
			InstrumentID: instrumentID, TradeDate: p.TradeDate, Value: p.Value,
			PointType: p.PointType, SourceName: p.SourceName, FetchedAt: p.FetchedAt,
		}
	}
	return out
}

func toRepoAnnual(instrumentID string, rows []marketdata.AnnualReturnRow) []repository.AnnualReturnRecord {
	out := make([]repository.AnnualReturnRecord, len(rows))
	for i, r := range rows {
		out[i] = repository.AnnualReturnRecord{
			InstrumentID: instrumentID, Year: r.Year, AnnualReturn: r.AnnualReturn,
			StartDate: r.StartDate, EndDate: r.EndDate,
			StartValue: r.StartValue, EndValue: r.EndValue,
			Observations: r.Observations, IsPartial: r.IsPartial,
		}
	}
	return out
}
