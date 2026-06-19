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

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// InstrumentImportRequest is the only client-writable import payload.
type InstrumentImportRequest struct {
	Market         string `json:"market"`
	InstrumentType string `json:"instrument_type"`
	Code           string `json:"code"`
}

// InstrumentService manages the asset library.
type InstrumentService struct {
	sql        *sql.DB
	instRepo   *repository.InstrumentRepo
	marketRepo *repository.MarketDataRepo
	annualRepo *repository.AnnualReturnsRepo
	jobs       *repository.JobRepo
	tickets    *repository.ResolutionTicketRepo
	provider   *marketdata.ProviderClient
}

func NewInstrumentService(
	sqlDB *sql.DB,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
	annualRepo *repository.AnnualReturnsRepo,
	jobs *repository.JobRepo,
	tickets *repository.ResolutionTicketRepo,
	provider *marketdata.ProviderClient,
) *InstrumentService {
	return &InstrumentService{
		sql: sqlDB, instRepo: instRepo, marketRepo: marketRepo,
		annualRepo: annualRepo, jobs: jobs, tickets: tickets, provider: provider,
	}
}

func (s *InstrumentService) List(ctx context.Context, valuationDate string) ([]repository.InstrumentRecord, error) {
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
		if valuationDate != "" {
			metrics, quality := libraryMetricsAtDate(ctx, s.marketRepo, items[i].ID, valuationDate)
			items[i].QualityStatus = quality
			applySimulationMeta(&items[i], metrics)
		} else {
			s.enrichMarketMeta(ctx, &items[i])
		}
	}
	return items, nil
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
		if res.Instruments[i].ID == repository.SystemCashInstrumentID {
			res.Instruments[i].QualityStatus = "available"
			res.Instruments[i].SimulationEligible = true
			continue
		}
		if res.Instruments[i].IsSystem {
			res.Instruments[i].QualityStatus = "unavailable"
			continue
		}
		s.enrichMarketMeta(ctx, &res.Instruments[i])
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

func normalizeInstrumentImport(req *InstrumentImportRequest) {
	req.Code = strings.TrimSpace(req.Code)
	if strings.EqualFold(req.Market, "HK") {
		req.Code = marketdata.NormalizeHKCode(req.Code)
	}
}

func (s *InstrumentService) Preview(ctx context.Context, req InstrumentImportRequest) (map[string]any, error) {
	normalizeInstrumentImport(&req)
	if err := validateImportRequest(req); err != nil {
		return nil, err
	}
	resolved, err := s.Resolve(ctx, InstrumentResolveRequest(req))
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"deprecated": true,
		"preview":    true,
		"message":    "preview no longer fetches full history; use resolve + import-async",
		"resolve":    resolved,
	}
	if amb, ok := resolved["ambiguous"].(bool); ok && !amb {
		if r, ok := resolved["resolved"].(map[string]any); ok {
			out["instrument"] = map[string]any{
				"code": r["code"], "name": r["name"], "market": req.Market,
				"instrument_type": req.InstrumentType,
				"currency":        defaultCurrency(req.Market),
			}
			out["provider_symbol"] = r["provider_symbol"]
		}
	}
	return out, nil
}

func (s *InstrumentService) Import(ctx context.Context, req InstrumentImportRequest) (repository.InstrumentRecord,
	error,
) {
	normalizeInstrumentImport(&req)
	if err := validateImportRequest(req); err != nil {
		return repository.InstrumentRecord{}, wrapRepo("load instrument", err)
	}
	resolved, err := s.Resolve(ctx, InstrumentResolveRequest(req))
	if err != nil {
		return repository.InstrumentRecord{}, wrapRepo("load instrument", err)
	}
	if amb, _ := resolved["ambiguous"].(bool); amb {
		return repository.InstrumentRecord{}, newErr("instrument_ambiguous",
			"code is ambiguous; use import-async after resolve", nil)
	}
	r, ok := resolved["resolved"].(map[string]any)
	if !ok {
		return repository.InstrumentRecord{}, newErr("instrument_not_found", "instrument not found", nil)
	}
	ticketID, _ := r["ticket_id"].(string)
	if ticketID == "" {
		return repository.InstrumentRecord{}, newErr("invalid_request", "resolve did not return ticket_id", nil)
	}
	result, err := s.ImportAsync(ctx, InstrumentImportAsyncRequest{
		TicketID:   ticketID,
		AssetClass: defaultImportAssetClass(req.InstrumentType),
		Region:     defaultImportRegion(req.Market),
	})
	if err != nil {
		return repository.InstrumentRecord{}, wrapRepo("load instrument", err)
	}
	inst, err := s.instRepo.GetByID(ctx, result.InstrumentID)
	if err != nil {
		return repository.InstrumentRecord{}, wrapRepo("load instrument", err)
	}
	inst.QualityStatus = "pending_sync"
	return inst, nil
}

// InstrumentRefreshOptions controls instrument refresh behavior.
type InstrumentRefreshOptions struct {
	Force bool `json:"force"`
}

func (s *InstrumentService) Refresh(ctx context.Context, instrumentID string,
	opts InstrumentRefreshOptions,
) (repository.InstrumentRecord, error) {
	inst, err := s.loadRefreshableInstrument(ctx, instrumentID, opts)
	if err != nil {
		return inst, err
	}
	if opts.Force {
		slog.InfoContext(
			ctx, "instrument force refresh",
			"instrument_id", instrumentID,
			"code", inst.Code,
		)
	}

	// Heal identity before fetching so refresh requests carry instrument_kind and
	// the sidecar selects an identity-consistent history source (td/038 P1-1).
	inst, err = s.ensureInstrumentKind(ctx, inst)
	if err != nil {
		return inst, err
	}

	existingRows, err := s.marketRepo.ListByInstrument(ctx, instrumentID)
	if err != nil {
		return repository.InstrumentRecord{}, wrapRepo("load instrument", err)
	}
	existing := repoToDataPoints(existingRows)
	fullReplace := marketdata.ShouldFullReplaceOnRefresh(opts.Force, existing, "")
	lastDate, _ := s.marketRepo.LastTradeDate(ctx, instrumentID)
	start := refreshOverlapStart(fullReplace, lastDate)
	end := time.Now().Format("2006-01-02")
	data, processed, err := s.fetchAndProcessForInstrument(ctx, inst, start)
	if err != nil {
		// upstream failure: keep existing data
		return inst, err
	}
	if processed.HasAnomaly {
		return inst, newErr("provider_data_anomaly", "refresh rejected due to abnormal daily returns", nil)
	}

	fullReplace = marketdata.ShouldFullReplaceOnRefresh(opts.Force, existing, processed.SourceName)
	reprocessed := mergeRefreshProcessedData(ctx, instrumentID, existing, processed, opts.Force, end)

	newName := strings.TrimSpace(data.Name)
	shouldUpdateName := shouldUpgradeInstrumentName(inst.Name, newName, inst.Code)

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		return persistRefreshMarketDataTx(ctx, s, tx, instrumentID, fullReplace, reprocessed, shouldUpdateName, newName)
	})
	if err != nil {
		return repository.InstrumentRecord{}, wrapRepo("refresh instrument", err)
	}
	return s.Get(ctx, instrumentID)
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

func (s *InstrumentService) fetchAndProcessForInstrument(
	ctx context.Context,
	inst repository.InstrumentRecord,
	start *string,
) (*marketdata.FetchData, marketdata.ProcessFetchResult, error) {
	end := time.Now().Format("2006-01-02")
	fetchReq := marketdata.FetchRequest{
		Market: inst.Market, InstrumentType: inst.InstrumentType,
		SourceCode: inst.Code, StartDate: start, EndDate: end,
		AdjustPolicy:   marketdata.DefaultAdjustPolicy(inst.InstrumentType),
		ResolvedName:   inst.Name,
		InstrumentKind: inst.InstrumentKind,
	}
	data, err := s.provider.Fetch(ctx, fetchReq)
	if err != nil {
		slog.WarnContext(
			ctx, "instrument fetch failed",
			"instrument_id", inst.ID,
			"market", inst.Market,
			"instrument_type", inst.InstrumentType,
			"code", inst.Code,
			"error", err,
		)
		return nil, marketdata.ProcessFetchResult{}, mapMarketProviderError(err)
	}
	if err := marketdata.ValidateFetchSourceCompatibility(inst.InstrumentType, inst.AssetClass, data); err != nil {
		return nil, marketdata.ProcessFetchResult{}, mapSourceConflictError(err)
	}
	if _, err := marketdata.ResolveClassification(inst.Market, inst.InstrumentType, data); err != nil {
		return nil, marketdata.ProcessFetchResult{}, mapClassifyError(err)
	}
	processed := marketdata.ProcessProviderData(data, end)
	return data, processed, nil
}

// identitySensitiveInstrumentType reports whether an instrument type relies on a
// resolved instrument_kind to pick identity-consistent history sources. Only
// cn_exchange_fund currently shares bare codes across ETF/LOF/stock variants in
// the sidecar fallback chain (td/037 现象-4).
func identitySensitiveInstrumentType(instrumentType string) bool {
	return instrumentType == "cn_exchange_fund"
}

// ensureInstrumentKind backfills a missing resolved instrument_kind before a
// refresh fetch. Legacy assets imported before instrument_kind was persisted
// would otherwise let the sidecar fall back to the legacy ETF->LOF->stock chain
// and mix data across instruments sharing a bare code (td/038 P1-1). Identity is
// healed here via one controlled resolve, then persisted so future refreshes are
// identity-safe.
func (s *InstrumentService) ensureInstrumentKind(
	ctx context.Context, inst repository.InstrumentRecord,
) (repository.InstrumentRecord, error) {
	if inst.InstrumentKind != "" || !identitySensitiveInstrumentType(inst.InstrumentType) {
		return inst, nil
	}
	data, err := s.provider.Resolve(ctx, marketdata.ResolveRequest{
		Market: inst.Market, InstrumentType: inst.InstrumentType, Code: inst.Code,
	})
	if err != nil {
		if ae := mapMarketProviderError(err); ae != nil {
			return inst, ae
		}
		return inst, newErr("market_provider_unavailable", err.Error(), nil)
	}
	kind := matchResolvedInstrumentKind(data, inst.Code, inst.ProviderSymbol)
	if kind == "" {
		return inst, newErr("instrument_identity_unresolved",
			"cannot establish instrument identity for refresh; re-import the asset", nil)
	}
	if err := s.instRepo.UpdateInstrumentKindTx(ctx, nil, inst.ID, kind); err != nil {
		return inst, wrapRepo("backfill instrument kind", err)
	}
	inst.InstrumentKind = kind
	slog.InfoContext(ctx, "instrument kind backfilled for refresh",
		"instrument_id", inst.ID, "code", inst.Code, "instrument_kind", kind)
	return inst, nil
}

// matchResolvedInstrumentKind picks the kind of the resolved candidate whose
// identity (code/provider_symbol) matches the stored instrument, tolerating
// prefixed (sh510300) and bare (510300) forms.
func matchResolvedInstrumentKind(data *marketdata.ResolveData, code, providerSymbol string) string {
	if data == nil {
		return ""
	}
	match := func(c marketdata.ResolveCandidate) bool {
		return identityCodeMatch(c.ProviderSymbol, providerSymbol) ||
			identityCodeMatch(c.Code, code) ||
			identityCodeMatch(c.Code, providerSymbol) ||
			identityCodeMatch(c.ProviderSymbol, code)
	}
	if data.Resolved != nil && match(*data.Resolved) {
		return data.Resolved.InstrumentKind
	}
	for _, c := range data.Candidates {
		if match(c) {
			return c.InstrumentKind
		}
	}
	return ""
}

func identityCodeMatch(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	return a == b || bareInstrumentCode(a) == bareInstrumentCode(b)
}

func bareInstrumentCode(s string) string {
	for _, p := range []string{"sh", "sz", "bj"} {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
			break
		}
	}
	return strings.TrimLeft(s, "0")
}

// isPlaceholderInstrumentName reports whether a stored name is just the code
// (a placeholder left behind when name resolution failed). It tolerates the
// prefixed (sh510300), bare (510300) and zero-stripped forms.
func isPlaceholderInstrumentName(name, code string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return true
	}
	c := strings.TrimSpace(code)
	candidates := map[string]struct{}{
		strings.ToLower(c): {},
	}
	bare := strings.ToLower(c)
	for _, p := range []string{"sh", "sz", "bj"} {
		if strings.HasPrefix(bare, p) {
			bare = bare[len(p):]
			break
		}
	}
	candidates[bare] = struct{}{}
	candidates[strings.TrimLeft(bare, "0")] = struct{}{}
	_, ok := candidates[strings.ToLower(n)]
	return ok
}

// shouldUpgradeInstrumentName decides whether a freshly fetched name should
// overwrite the stored one. A real name always replaces a placeholder (code)
// name, enabling self-heal of instruments that were imported before the name
// was resolvable; a placeholder fetch result never overwrites a real name.
func shouldUpgradeInstrumentName(current, fetched, code string) bool {
	fetched = strings.TrimSpace(fetched)
	if fetched == "" || isPlaceholderInstrumentName(fetched, code) {
		return false
	}
	if isPlaceholderInstrumentName(current, code) {
		return true
	}
	return fetched != strings.TrimSpace(current)
}

func validateImportRequest(req InstrumentImportRequest) error {
	if req.Market == "" || req.InstrumentType == "" || req.Code == "" {
		return newErr("invalid_request", "market, instrument_type and code are required", nil)
	}
	if len(req.Code) > 64 {
		return newErr("invalid_request", "code too long", nil)
	}
	return nil
}

func mapClassifyError(err error) *AppError {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "instrument_classification_unsupported"):
		return newErr("instrument_classification_unsupported", "instrument classification is not supported", nil)
	case strings.Contains(msg, "instrument_metadata_conflict"):
		return newErr("instrument_metadata_conflict", "instrument metadata conflict", nil)
	default:
		return newErr("invalid_request", msg, nil)
	}
}

func mapSourceConflictError(err error) *AppError {
	if errors.Is(err, marketdata.ErrSourceTypeConflict) {
		return newErr(
			"market_data_source_type_conflict",
			"fetch source does not match instrument asset class; existing data kept",
			nil,
		)
	}
	return newErr("invalid_request", err.Error(), nil)
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

func toHistorical(points []marketdata.DataPoint) []marketdata.HistoricalPoint {
	out := make([]marketdata.HistoricalPoint, len(points))
	for i, p := range points {
		out[i] = marketdata.HistoricalPoint{Date: p.TradeDate, Value: p.Value}
	}
	return out
}

func applyDataStale(inst *repository.InstrumentRecord, lastTradeDate string) {
	stale, warning := marketdata.DataStale(lastTradeDate, time.Now())
	inst.DataStale = stale
	inst.StaleWarning = warning
}

// CheckInstrumentReadOnlyFields rejects client metadata in import payloads.
func CheckInstrumentReadOnlyFields(body []byte) error {
	return checkInstrumentReadOnlyFields(body, nil)
}

// CheckInstrumentImportAsyncFields allows user-selected asset_class and region on import-async.
func CheckInstrumentImportAsyncFields(body []byte) error {
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
