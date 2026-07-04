package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/libmetrics"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// InstrumentImportRequest imports a user instrument from the global market
// asset directory (td/078 P4). asset_class and region are the only
// user-writable classification fields.
type InstrumentImportRequest struct {
	AssetKey   string `json:"asset_key"`
	AssetClass string `json:"asset_class"`
	Region     string `json:"region"`
}

// InstrumentService manages the asset library.
type InstrumentService struct {
	sql        *sql.DB
	instRepo   *repository.InstrumentRepo
	marketRepo *repository.MarketDataRepo
	annualRepo *repository.AnnualReturnsRepo
	libMetrics *repository.InstrumentLibraryMetricsRepo
	assets     *repository.MarketAssetRepo
}

func NewInstrumentService(
	sqlDB *sql.DB,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
	annualRepo *repository.AnnualReturnsRepo,
	assets *repository.MarketAssetRepo,
) *InstrumentService {
	return &InstrumentService{
		sql: sqlDB, instRepo: instRepo, marketRepo: marketRepo,
		annualRepo: annualRepo, libMetrics: repository.NewInstrumentLibraryMetricsRepo(sqlDB),
		assets: assets,
	}
}

func (s *InstrumentService) List(ctx context.Context, valuationDate string) ([]repository.InstrumentRecord, error) {
	// Plan-context listing (valuation_date set) truncates quality to the plan's
	// valuation date, which the precomputed list projection cannot express, so it
	// keeps the per-row path. The asset library itself (no valuation_date) reads
	// the projection in a single JOIN.
	if valuationDate != "" {
		return s.listAtValuationDate(ctx, valuationDate)
	}
	items, err := s.instRepo.ListWithMetrics(ctx)
	if err != nil {
		return nil, wrapRepo("list instruments", err)
	}
	refCounts, err := s.instRepo.ReferenceCounts(ctx)
	if err != nil {
		return nil, wrapRepo("list instrument reference counts", err)
	}
	for i := range items {
		items[i].ReferencingPlanCount = refCounts[items[i].ID]
		s.applyProjectedListMeta(&items[i])
	}
	return items, nil
}

func (s *InstrumentService) listAtValuationDate(
	ctx context.Context, valuationDate string,
) ([]repository.InstrumentRecord, error) {
	items, err := s.instRepo.List(ctx)
	if err != nil {
		return nil, wrapRepo("list instruments", err)
	}
	refCounts, err := s.instRepo.ReferenceCounts(ctx)
	if err != nil {
		return nil, wrapRepo("list instrument reference counts", err)
	}
	for i := range items {
		items[i].ReferencingPlanCount = refCounts[items[i].ID]
		if items[i].ID == repository.SystemCashInstrumentID {
			items[i].QualityStatus = "available"
			items[i].SimulationEligible = true
			continue
		}
		if items[i].IsSystem {
			items[i].QualityStatus = "unavailable"
			continue
		}
		metrics, quality := libraryMetricsAtDate(ctx, s.marketRepo, items[i].ID, valuationDate)
		items[i].QualityStatus = quality
		applySimulationMeta(&items[i], metrics)
	}
	return items, nil
}

// applyProjectedListMeta finalizes one row from ListWithMetrics/Search: it
// applies system overrides, clears projected fields for non-active rows (which
// must render "—" instead of stale values), and derives the time-relative stale
// flag from the projection's data_as_of. It issues no per-instrument query.
func (s *InstrumentService) applyProjectedListMeta(inst *repository.InstrumentRecord) {
	if inst.ID == repository.SystemCashInstrumentID {
		clearProjectedListMeta(inst)
		inst.QualityStatus = "available"
		inst.SimulationEligible = true
		return
	}
	if inst.IsSystem {
		clearProjectedListMeta(inst)
		inst.QualityStatus = "unavailable"
		return
	}
	// Pending/failed instruments must not surface a previous projection; the
	// library renders "—" until a fetch/refresh succeeds and rewrites it.
	if inst.Status != "active" {
		clearProjectedListMeta(inst)
		return
	}
	applyDataStale(inst, inst.DataAsOf)
}

func clearProjectedListMeta(inst *repository.InstrumentRecord) {
	inst.QualityStatus = ""
	inst.DataAsOf = ""
	inst.DataSourceName = ""
	inst.PointType = ""
	inst.SimulationEligible = false
	inst.HistoryDepth = ""
	inst.CompleteYearCount = 0
	inst.MonthlyReturnCount = 0
	inst.MetricsVersion = ""
	inst.Warnings = nil
	inst.TrailingReturns = nil
	inst.DataStale = false
	inst.StaleWarning = ""
}

// InstrumentSearchView is a paginated asset-library search response.
type InstrumentSearchView struct {
	Instruments []repository.InstrumentRecord `json:"instruments"`
	NextCursor  *int                          `json:"next_cursor"`
	Total       int                           `json:"total"`
}

// Search returns one filtered, paginated page of instruments with simulation
// metadata enriched per row.
func (s *InstrumentService) Search(
	ctx context.Context, opts repository.InstrumentSearchOptions,
) (InstrumentSearchView, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	res, err := s.instRepo.Search(ctx, opts)
	if err != nil {
		return InstrumentSearchView{}, wrapRepo("search instruments", err)
	}
	refCounts, err := s.instRepo.ReferenceCounts(ctx)
	if err != nil {
		return InstrumentSearchView{}, wrapRepo("list instrument reference counts", err)
	}
	for i := range res.Instruments {
		res.Instruments[i].ReferencingPlanCount = refCounts[res.Instruments[i].ID]
		s.applyProjectedListMeta(&res.Instruments[i])
	}
	view := InstrumentSearchView{Instruments: res.Instruments, Total: res.Total}
	if view.Instruments == nil {
		view.Instruments = []repository.InstrumentRecord{}
	}
	if opts.Offset+len(res.Instruments) < res.Total {
		next := opts.Offset + len(res.Instruments)
		view.NextCursor = &next
	}
	return view, nil
}

func applySimulationMeta(inst *repository.InstrumentRecord, metrics marketdata.SnapshotMetrics) {
	inst.SimulationEligible = metrics.SimulationEligible
	inst.HistoryDepth = metrics.HistoryDepth
	inst.CompleteYearCount = metrics.CompleteYearCount
	inst.MonthlyReturnCount = metrics.MonthlyReturnCount
	inst.MetricsVersion = metrics.MetricsVersion
	inst.Warnings = metrics.Warnings
}

func (s *InstrumentService) enrichMarketMeta(ctx context.Context, inst *repository.InstrumentRecord) {
	last, _ := s.marketRepo.LastTradeDate(ctx, inst.ID)
	inst.DataAsOf = last
	src, pt, _ := s.marketRepo.LatestPointMeta(ctx, inst.ID)
	inst.DataSourceName = src
	inst.PointType = pt
	metrics, quality := libraryMetricsAtDate(ctx, s.marketRepo, inst.ID, "")
	inst.QualityStatus = quality
	applySimulationMeta(inst, metrics)
	applyDataStale(inst, last)
}

func (s *InstrumentService) Get(ctx context.Context, id string) (repository.InstrumentRecord, error) {
	inst, err := s.instRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return repository.InstrumentRecord{}, newErr("instrument_not_found", "instrument not found", nil)
		}
		return repository.InstrumentRecord{}, wrapRepo("load instrument", err)
	}
	if !inst.IsSystem {
		s.enrichMarketMeta(ctx, &inst)
	} else {
		inst.QualityStatus = "available"
	}
	return inst, nil
}

// ImportFromMarketAsset creates a user instrument from a market asset
// directory entry and projects its stored history into the legacy instrument
// tables. It performs no remote calls: assets without synced history are
// rejected with market_asset_history_empty so the UI can direct the user to
// the asset detail page first.
func (s *InstrumentService) ImportFromMarketAsset(
	ctx context.Context, req InstrumentImportRequest,
) (repository.InstrumentRecord, error) {
	req.AssetKey = strings.TrimSpace(req.AssetKey)
	req.AssetClass = strings.TrimSpace(req.AssetClass)
	req.Region = strings.TrimSpace(req.Region)
	if req.AssetKey == "" {
		return repository.InstrumentRecord{}, newErr("invalid_request", "asset_key is required", nil)
	}
	asset, err := s.assets.GetByKey(ctx, req.AssetKey)
	if err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return repository.InstrumentRecord{}, newErr("market_asset_not_found",
				"market asset not found; sync the asset directory first", nil)
		}
		return repository.InstrumentRecord{}, wrapRepo("load market asset", err)
	}
	if req.AssetClass == "" {
		req.AssetClass = defaultImportAssetClass(asset.InstrumentType)
	}
	if req.Region == "" {
		req.Region = defaultImportRegion(asset.Market)
	}
	if err := validateUserAssetClass(req.AssetClass); err != nil {
		return repository.InstrumentRecord{}, err
	}
	if err := validateUserRegion(req.Region); err != nil {
		return repository.InstrumentRecord{}, err
	}

	adjust := marketdata.DefaultAdjustPolicy(asset.InstrumentType)
	dimAdjust, dimPointType, err := s.importHistoryDimension(ctx, asset)
	if err != nil {
		return repository.InstrumentRecord{}, err
	}
	points, err := s.assets.ListPoints(ctx, asset.AssetKey, dimAdjust, dimPointType)
	if err != nil {
		return repository.InstrumentRecord{}, wrapRepo("list market asset points", err)
	}
	if len(points) == 0 {
		return repository.InstrumentRecord{}, newErr("market_asset_history_empty",
			"market asset has no synced history; sync history on the asset detail page first",
			map[string]any{"asset_key": asset.AssetKey})
	}

	if existing, findErr := s.instRepo.FindByKey(
		ctx, asset.Market, asset.InstrumentType, asset.Symbol, adjust,
	); findErr == nil {
		return repository.InstrumentRecord{}, newErr("instrument_already_exists",
			"instrument already imported", map[string]any{"instrument_id": existing.ID})
	} else if !errors.Is(findErr, repository.ErrInstrumentNotFound) {
		return repository.InstrumentRecord{}, wrapRepo("find instrument by key", findErr)
	}

	currency := asset.Currency
	if currency == "" {
		currency = defaultCurrency(asset.Market)
	}
	providerSymbol := asset.Symbol
	if asset.RegionCode != "" && strings.EqualFold(asset.Market, "CN") {
		providerSymbol = asset.RegionCode + asset.Symbol
	}
	inst := repository.InstrumentRecord{
		ID: "ins_" + uuid.New().String(), Code: asset.Symbol, Name: asset.Name,
		Market: asset.Market, InstrumentType: asset.InstrumentType,
		AssetClass: req.AssetClass, Region: req.Region, Currency: currency,
		Provider: "akshare", ProviderSymbol: providerSymbol,
		AssetKey: asset.AssetKey, AdjustPolicy: adjust,
		InstrumentKind:     asset.InstrumentKind,
		ExpenseRatioStatus: "unavailable",
		FeeTreatment:       marketdata.FeeTreatmentForType(asset.InstrumentType),
		Status:             "active",
	}

	dp := make([]marketdata.DataPoint, len(points))
	for i, p := range points {
		dp[i] = marketdata.DataPoint{
			TradeDate: p.TradeDate, Value: p.Value,
			PointType: p.PointType, SourceName: p.SourceName, FetchedAt: p.FetchedAt,
		}
	}
	annual := marketdata.ComputeAnnualReturns(dp)

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.instRepo.Create(ctx, tx, inst); err != nil {
			return wrapRepo("create instrument", err)
		}
		if err := s.marketRepo.UpsertBatch(ctx, tx, inst.ID, toRepoPoints(inst.ID, dp)); err != nil {
			return wrapRepo("project market data", err)
		}
		if err := s.annualRepo.ReplaceAll(ctx, tx, inst.ID, toRepoAnnual(inst.ID, annual)); err != nil {
			return wrapRepo("project annual returns", err)
		}
		return libmetrics.SyncTx(ctx, s.libMetrics, tx, inst.ID, dp)
	})
	if err != nil {
		if isUniqueConstraintErr(err) {
			return repository.InstrumentRecord{}, newErr("instrument_already_exists",
				"instrument already imported", nil)
		}
		var ae *AppError
		if errors.As(err, &ae) {
			return repository.InstrumentRecord{}, ae
		}
		return repository.InstrumentRecord{}, wrapRepo("import instrument from market asset", err)
	}
	slog.InfoContext(ctx, "instrument imported from market asset",
		"instrument_id", inst.ID, "asset_key", asset.AssetKey, "points", len(dp))
	return s.Get(ctx, inst.ID)
}

// importHistoryDimension picks the history dimension to project at import
// time: the asset's synced history state wins, falling back to type defaults.
func (s *InstrumentService) importHistoryDimension(
	ctx context.Context, asset repository.MarketAsset,
) (string, string, error) {
	states, err := s.assets.ListHistoryStatesByAsset(ctx, asset.AssetKey)
	if err != nil {
		return "", "", wrapRepo("list history states", err)
	}
	for _, st := range states {
		if st.PointCount > 0 {
			return st.AdjustPolicy, st.PointType, nil
		}
	}
	return "none", DefaultPointType(asset.InstrumentType, asset.InstrumentKind), nil
}

func (s *InstrumentService) Delete(ctx context.Context, instrumentID string) error {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return newErr("instrument_not_found", "instrument not found", nil)
		}
		return wrapRepo("load instrument", err)
	}
	if inst.IsSystem || inst.Provider != "akshare" {
		return newErr("instrument_not_deletable", "system instruments cannot be deleted", nil)
	}
	ref, err := s.instRepo.IsReferencedByPlan(ctx, instrumentID)
	if err != nil {
		return wrapRepo("check instrument references", err)
	}
	if ref {
		return newErr("instrument_in_use", "instrument is referenced by a plan holding", nil)
	}
	if err := s.instRepo.Delete(ctx, instrumentID); err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return newErr("instrument_not_found", "instrument not found", nil)
		}
		return wrapRepo("delete instrument", err)
	}
	return nil
}

func (s *InstrumentService) AnnualReturns(ctx context.Context, instrumentID string,
	inclusionDate string,
) ([]repository.AnnualReturnRecord, error) {
	if _, err := s.instRepo.GetByID(ctx, instrumentID); err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return nil, newErr("instrument_not_found", "instrument not found", nil)
		}
		return nil, wrapRepo("load instrument for annual returns", err)
	}
	rows, err := s.annualRepo.ListByInstrument(ctx, instrumentID)
	if err != nil {
		return nil, wrapRepo("list annual returns", err)
	}
	if inclusionDate == "" {
		inclusionDate = time.Now().Format("2006-01-02")
	}
	points, _ := s.marketRepo.ListByInstrument(ctx, instrumentID)
	simYears := marketdata.SelectSimulationYears(repoToDataPoints(points), toMarketAnnual(rows), inclusionDate)
	simSet := map[int]struct{}{}
	for _, y := range simYears {
		simSet[y.Year] = struct{}{}
	}
	for i := range rows {
		_, rows[i].InSimulation = simSet[rows[i].Year]
	}
	return rows, nil
}

// HistoricalSnapshotView is a plan-specific frozen snapshot summary for the asset detail UI.
type HistoricalSnapshotView struct {
	ID                 string   `json:"id"`
	PlanID             *string  `json:"plan_id,omitempty"`
	InclusionDate      string   `json:"inclusion_date"`
	CompleteYearCount  int      `json:"complete_year_count"`
	MonthlyReturnCount int      `json:"monthly_return_count"`
	HistoryDepth       string   `json:"history_depth"`
	MetricsVersion     string   `json:"metrics_version"`
	Warnings           []string `json:"warnings"`
	CreatedAt          int64    `json:"created_at"`
}

func toHistoricalSnapshotViews(snaps []repository.SimulationSnapshot) []HistoricalSnapshotView {
	out := make([]HistoricalSnapshotView, len(snaps))
	for i, snap := range snaps {
		out[i] = HistoricalSnapshotView{
			ID:                 snap.ID,
			PlanID:             snap.PlanID,
			InclusionDate:      snap.InclusionDate,
			CompleteYearCount:  snap.CompleteYearCount,
			MonthlyReturnCount: snap.MonthlyReturnCount,
			HistoryDepth:       snap.HistoryDepth,
			MetricsVersion:     snap.MetricsVersion,
			Warnings:           parseHistoricalSnapshotWarnings(snap.WarningsJSON),
			CreatedAt:          snap.CreatedAt,
		}
	}
	return out
}

func parseHistoricalSnapshotWarnings(raw string) []string {
	var out []string
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// InstrumentDetailView aggregates asset library detail for the UI.
type InstrumentDetailView struct {
	Instrument          repository.InstrumentRecord          `json:"instrument"`
	AnnualReturns       []repository.AnnualReturnRecord      `json:"annual_returns"`
	SimulationWindow    map[string]any                       `json:"simulation_window"`
	TrailingReturns     map[string]any                       `json:"trailing_returns"`
	HistoricalSnapshots []HistoricalSnapshotView             `json:"historical_snapshots"`
	ReferencingPlans    []repository.PlanInstrumentReference `json:"referencing_plans"`
}

func (s *InstrumentService) GetDetail(ctx context.Context, id string) (InstrumentDetailView, error) {
	inst, err := s.Get(ctx, id)
	if err != nil {
		return InstrumentDetailView{}, err
	}
	inclusionDate := time.Now().Format("2006-01-02")
	returns, err := s.AnnualReturns(ctx, id, inclusionDate)
	if err != nil {
		return InstrumentDetailView{}, err
	}
	points, _ := s.marketRepo.ListByInstrument(ctx, id)
	dp := repoToDataPoints(points)
	annualRows := toMarketAnnualFromRepo(returns)
	simYears := marketdata.SelectSimulationYears(dp, annualRows, inclusionDate)
	pointType, sourceName := "adjusted_close", "library"
	if len(dp) > 0 {
		pointType = dp[0].PointType
		sourceName = dp[0].SourceName
	}
	simMetrics := marketdata.ComputeMetrics(dp, simYears, pointType, sourceName)
	excluded := marketdata.BuildExcludedYears(dp, marketdata.ComputeAnnualReturns(dp), simYears, inclusionDate)
	selectedYears := make([]int, len(simYears))
	for i, y := range simYears {
		selectedYears[i] = y.Year
	}
	trailing := marketdata.ComputeTrailingReturns(dp, inclusionDate, pointType, sourceName)
	snapRepo := repository.NewSnapshotRepo(s.sql)
	snaps, _ := snapRepo.ListByInstrument(ctx, id)
	holdRepo := repository.NewHoldingsRepo(s.sql)
	refs, _ := holdRepo.ListReferencingPlans(ctx, id)
	if snaps == nil {
		snaps = []repository.SimulationSnapshot{}
	}
	if refs == nil {
		refs = []repository.PlanInstrumentReference{}
	}
	if returns == nil {
		returns = []repository.AnnualReturnRecord{}
	}
	if excluded == nil {
		excluded = []marketdata.ExcludedYear{}
	}
	return InstrumentDetailView{
		Instrument: inst, AnnualReturns: returns,
		SimulationWindow:    buildSimulationWindowMap(inclusionDate, selectedYears, excluded, simMetrics, inst),
		TrailingReturns:     trailingReturnsToMap(trailing),
		HistoricalSnapshots: toHistoricalSnapshotViews(snaps), ReferencingPlans: refs,
	}, nil
}

// ReturnSeries builds a normalized cumulative-return curve for a fixed range.
func (s *InstrumentService) ReturnSeries(ctx context.Context, id, rangeKey string) (marketdata.ReturnSeries, error) {
	if !marketdata.IsValidReturnSeriesRange(rangeKey) {
		return marketdata.ReturnSeries{}, newErr("invalid_request", "invalid return-series range", nil)
	}
	if _, err := s.Get(ctx, id); err != nil {
		return marketdata.ReturnSeries{}, err
	}
	points, err := s.marketRepo.ListByInstrument(ctx, id)
	if err != nil {
		return marketdata.ReturnSeries{}, wrapRepo("list market data points", err)
	}
	dp := repoToDataPoints(points)
	pointType, sourceName := "adjusted_close", "library"
	if len(dp) > 0 {
		pointType = dp[len(dp)-1].PointType
		sourceName = dp[len(dp)-1].SourceName
	}
	asOf := time.Now().Format("2006-01-02")
	return marketdata.ComputeReturnSeries(dp, asOf, rangeKey, pointType, sourceName), nil
}

func buildSimulationWindowMap(
	inclusionDate string,
	selectedYears []int,
	excluded []marketdata.ExcludedYear,
	simMetrics marketdata.SnapshotMetrics,
	inst repository.InstrumentRecord,
) map[string]any {
	out := map[string]any{
		"inclusion_date": inclusionDate, "selected_years": selectedYears,
		"excluded_years": excluded, "complete_year_count": simMetrics.CompleteYearCount,
		"daily_observation_count": simMetrics.DailyObservationCount,
		"monthly_return_count":    simMetrics.MonthlyReturnCount,
		"cagr_status":             simMetrics.CAGRStatus, "volatility_status": simMetrics.VolatilityStatus,
		"drawdown_status": simMetrics.DrawdownStatus,
		"quality_status":  simMetrics.QualityStatus, "simulation_eligible": simMetrics.SimulationEligible,
		"history_depth":     simMetrics.HistoryDepth,
		"volatility_method": simMetrics.VolatilityMethod, "metrics_version": simMetrics.MetricsVersion,
		"warnings":      simMetrics.Warnings,
		"fee_treatment": inst.FeeTreatment, "expense_ratio_status": inst.ExpenseRatioStatus,
	}
	if simMetrics.HistoricalCAGR != nil {
		out["historical_cagr"] = *simMetrics.HistoricalCAGR
	} else {
		out["historical_cagr"] = nil
	}
	if simMetrics.AnnualVolatility != nil {
		out["annual_volatility"] = *simMetrics.AnnualVolatility
	} else {
		out["annual_volatility"] = nil
	}
	if simMetrics.MaxDrawdown != nil {
		out["max_drawdown"] = *simMetrics.MaxDrawdown
	} else {
		out["max_drawdown"] = nil
	}
	return out
}

func trailingReturnsToMap(t marketdata.TrailingReturns) map[string]any {
	periods := map[string]any{}
	for key, p := range t.Periods {
		periods[key] = map[string]any{
			"status": p.Status, "target_start_date": p.TargetStartDate,
			"start_date": p.StartDate, "end_date": p.EndDate,
			"actual_days": p.ActualDays, "cumulative_return": p.CumulativeReturn,
			"annualized_return": p.AnnualizedReturn,
		}
	}
	return map[string]any{
		"as_of_date": t.AsOfDate, "point_type": t.PointType, "source_name": t.SourceName,
		"periods": periods,
	}
}

func toMarketAnnualFromRepo(rows []repository.AnnualReturnRecord) []marketdata.AnnualReturnRow {
	out := make([]marketdata.AnnualReturnRow, len(rows))
	for i, r := range rows {
		out[i] = marketdata.AnnualReturnRow{
			Year: r.Year, AnnualReturn: r.AnnualReturn,
			StartDate: r.StartDate, EndDate: r.EndDate,
			Observations: r.Observations, IsPartial: r.IsPartial,
		}
	}
	return out
}

func (s *InstrumentService) libraryQuality(ctx context.Context, instrumentID string) string {
	return LibraryQualityFromRepos(ctx, s.marketRepo, instrumentID)
}

// LibraryQualityFromRepos computes library quality from stored market data points.
func LibraryQualityFromRepos(ctx context.Context, marketRepo *repository.MarketDataRepo, instrumentID string) string {
	points, err := marketRepo.ListByInstrument(ctx, instrumentID)
	if err != nil || len(points) == 0 {
		return "insufficient_history"
	}
	dp := repoToDataPoints(points)
	annual := marketdata.ComputeAnnualReturns(dp)
	if marketdata.DetectDailyAnomaly(dp) {
		return "provider_data_anomaly"
	}
	return marketdata.DetermineLibraryQuality(dp, annual, time.Now().Format("2006-01-02"), false)
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

func repoToDataPoints(rows []repository.MarketDataPoint) []marketdata.DataPoint {
	out := make([]marketdata.DataPoint, len(rows))
	for i, r := range rows {
		out[i] = marketdata.DataPoint{
			TradeDate: r.TradeDate, Value: r.Value,
			PointType: r.PointType, SourceName: r.SourceName, FetchedAt: r.FetchedAt,
		}
	}
	return out
}

func toMarketAnnual(rows []repository.AnnualReturnRecord) []marketdata.AnnualReturnRow {
	out := make([]marketdata.AnnualReturnRow, len(rows))
	for i, r := range rows {
		out[i] = marketdata.AnnualReturnRow{
			Year: r.Year, AnnualReturn: r.AnnualReturn,
			StartDate: r.StartDate, EndDate: r.EndDate,
			StartValue: r.StartValue, EndValue: r.EndValue,
			Observations: r.Observations, IsPartial: r.IsPartial,
		}
	}
	return out
}

func applyDataStale(inst *repository.InstrumentRecord, lastTradeDate string) {
	stale, warning := marketdata.DataStale(lastTradeDate, time.Now())
	inst.DataStale = stale
	inst.StaleWarning = warning
}

// CheckInstrumentImportFields allows user-selected asset_class and region on
// import; all other instrument metadata and metrics are read-only.
func CheckInstrumentImportFields(body []byte) error {
	return checkInstrumentReadOnlyFields(body, map[string]struct{}{"asset_class": {}, "region": {}})
}

func checkInstrumentReadOnlyFields(body []byte, allowed map[string]struct{}) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("decode instrument payload: %w", err)
	}
	readOnly := []string{
		"name", "asset_class", "region", "currency", "expense_ratio",
		"historical_cagr", "annual_volatility", "max_drawdown", "correlation",
	}
	for _, f := range readOnly {
		if _, ok := raw[f]; !ok {
			continue
		}
		if allowed != nil {
			if _, ok := allowed[f]; ok {
				continue
			}
		}
		return newErr("instrument_fields_read_only", "instrument metadata and metrics are read-only",
			map[string]any{"field": f})
	}
	return nil
}

// MapSnapshotError converts snapshot errors to AppError.
func MapSnapshotError(err error) error {
	var se *marketdata.SnapshotError
	if errors.As(err, &se) {
		return newErr(se.Code, se.Message, se.Details)
	}
	return err
}
