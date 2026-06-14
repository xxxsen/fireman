package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// ErrFetchCanceled indicates the job was finalized as canceled by markFetchCanceled.
var ErrFetchCanceled = errors.New("instrument fetch canceled")

// InstrumentFetchRunner executes background instrument history fetches.
type InstrumentFetchRunner struct {
	db         *sql.DB
	jobs       *repository.JobRepo
	instRepo   *repository.InstrumentRepo
	marketRepo *repository.MarketDataRepo
	annualRepo *repository.AnnualReturnsRepo
	provider   *marketdata.ProviderClient
}

func NewInstrumentFetchRunner(
	db *sql.DB,
	jobs *repository.JobRepo,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
	annualRepo *repository.AnnualReturnsRepo,
	provider *marketdata.ProviderClient,
) *InstrumentFetchRunner {
	return &InstrumentFetchRunner{
		db: db, jobs: jobs, instRepo: instRepo, marketRepo: marketRepo,
		annualRepo: annualRepo, provider: provider,
	}
}

func (r *InstrumentFetchRunner) markFetchCanceled(
	ctx context.Context,
	jobID string,
	payload repository.InstrumentFetchPayload,
) error {
	writeCtx, cancel := jobWriteCtx(ctx)
	defer cancel()
	if err := fdb.WithTx(writeCtx, r.db, func(tx *sql.Tx) error {
		if err := r.instRepo.UpdateStatusTx(writeCtx, tx, payload.InstrumentID, "fetch_failed"); err != nil {
			return fmt.Errorf("mark instrument fetch failed: %w", err)
		}
		return r.jobs.FinishTx(
			writeCtx, tx, jobID, repository.JobStatusCanceled, "fetch_canceled", "instrument fetch canceled by user",
		)
	}); err != nil {
		return fmt.Errorf("finalize canceled fetch: %w", err)
	}
	return ErrFetchCanceled
}

func (r *InstrumentFetchRunner) Run(
	ctx context.Context,
	job repository.Job,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
) error {
	var payload repository.InstrumentFetchPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("decode instrument fetch payload: %w", err)
	}
	progress(0, 1, "fetching_history")

	if cancelCheck != nil && cancelCheck() {
		return r.markFetchCanceled(ctx, job.ID, payload)
	}

	fetchCtx, cancelFetch := context.WithCancel(ctx)
	defer cancelFetch()
	r.watchFetchCancel(fetchCtx, cancelFetch, cancelCheck)

	end := time.Now().Format("2006-01-02")
	fetchReq := marketdata.FetchRequest{
		Market: payload.Market, InstrumentType: payload.InstrumentType,
		SourceCode: payload.ProviderSymbol, EndDate: end,
		AdjustPolicy: payload.AdjustPolicy, ResolvedName: payload.ResolvedName,
	}
	data, err := r.provider.Fetch(fetchCtx, fetchReq)
	if cancelCheck != nil && cancelCheck() {
		return r.markFetchCanceled(ctx, job.ID, payload)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if cancelCheck != nil && cancelCheck() {
				return r.markFetchCanceled(ctx, job.ID, payload)
			}
			return fmt.Errorf("fetch instrument history: %w", err)
		}
		_ = r.instRepo.UpdateStatusTx(ctx, nil, payload.InstrumentID, "fetch_failed")
		return fmt.Errorf("fetch instrument history: %w", err)
	}
	class, err := resolveFetchClassification(payload, data)
	if err != nil {
		_ = r.instRepo.UpdateStatusTx(ctx, nil, payload.InstrumentID, "fetch_failed")
		return fmt.Errorf("resolve instrument classification: %w", err)
	}
	if err := validateFetchSourceForPayload(payload, class, data); err != nil {
		_ = r.instRepo.UpdateStatusTx(ctx, nil, payload.InstrumentID, "fetch_failed")
		return err
	}
	processed := marketdata.ProcessProviderData(data, end)
	if processed.HasAnomaly {
		_ = r.instRepo.UpdateStatusTx(ctx, nil, payload.InstrumentID, "fetch_failed")
		return newCodedError("provider_data_anomaly")
	}

	if err := r.persistFetchedInstrument(ctx, payload, data, processed, class); err != nil {
		return err
	}
	progress(1, 1, "completed")
	return nil
}

func (r *InstrumentFetchRunner) watchFetchCancel(
	fetchCtx context.Context,
	cancelFetch context.CancelFunc,
	cancelCheck func() bool,
) {
	if cancelCheck == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-fetchCtx.Done():
				return
			case <-ticker.C:
				if cancelCheck() {
					cancelFetch()
					return
				}
			}
		}
	}()
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
	for i, row := range rows {
		out[i] = repository.AnnualReturnRecord{
			InstrumentID: instrumentID, Year: row.Year, AnnualReturn: row.AnnualReturn,
			StartDate: row.StartDate, EndDate: row.EndDate,
			StartValue: row.StartValue, EndValue: row.EndValue,
			Observations: row.Observations, IsPartial: row.IsPartial,
		}
	}
	return out
}

func resolveFetchClassification(
	payload repository.InstrumentFetchPayload,
	data *marketdata.FetchData,
) (marketdata.Classification, error) {
	if payload.UserAssetClass != "" && payload.UserRegion != "" {
		out, err := marketdata.UserClassification(
			payload.Market, payload.InstrumentType, payload.UserAssetClass, payload.UserRegion, data.Currency,
		)
		if err != nil {
			return out, fmt.Errorf("apply user classification: %w", err)
		}
		return out, nil
	}
	out, err := marketdata.ResolveClassification(payload.Market, payload.InstrumentType, data)
	if err != nil {
		return out, fmt.Errorf("resolve classification: %w", err)
	}
	return out, nil
}

func validateFetchSourceForPayload(
	payload repository.InstrumentFetchPayload,
	class marketdata.Classification,
	data *marketdata.FetchData,
) error {
	assetClass := class.AssetClass
	if payload.UserAssetClass != "" {
		assetClass = payload.UserAssetClass
	}
	if err := marketdata.ValidateFetchSourceCompatibility(payload.InstrumentType, assetClass, data); err != nil {
		return newCodedError("market_data_source_type_conflict")
	}
	return nil
}
