package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

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
	provider   *marketdata.ProviderClient
}

func NewInstrumentService(
	sqlDB *sql.DB,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
	annualRepo *repository.AnnualReturnsRepo,
	provider *marketdata.ProviderClient,
) *InstrumentService {
	return &InstrumentService{
		sql: sqlDB, instRepo: instRepo, marketRepo: marketRepo,
		annualRepo: annualRepo, provider: provider,
	}
}

func (s *InstrumentService) List(ctx context.Context) ([]repository.InstrumentRecord, error) {
	items, err := s.instRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].IsSystem {
			continue
		}
		s.enrichMarketMeta(ctx, &items[i])
	}
	return items, nil
}

func (s *InstrumentService) enrichMarketMeta(ctx context.Context, inst *repository.InstrumentRecord) {
	last, _ := s.marketRepo.LastTradeDate(ctx, inst.ID)
	inst.DataAsOf = last
	src, pt, _ := s.marketRepo.LatestPointMeta(ctx, inst.ID)
	inst.DataSourceName = src
	inst.PointType = pt
	inst.QualityStatus = s.libraryQuality(ctx, inst.ID)
	applyDataStale(inst, last)
}

func (s *InstrumentService) Get(ctx context.Context, id string) (repository.InstrumentRecord, error) {
	inst, err := s.instRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return repository.InstrumentRecord{}, newErr("instrument_not_found", "instrument not found", nil)
		}
		return repository.InstrumentRecord{}, err
	}
	if !inst.IsSystem {
		s.enrichMarketMeta(ctx, &inst)
	} else {
		inst.QualityStatus = "available"
	}
	return inst, nil
}

func (s *InstrumentService) Preview(ctx context.Context, req InstrumentImportRequest) (map[string]any, error) {
	if err := validateImportRequest(req); err != nil {
		return nil, err
	}
	data, processed, err := s.fetchAndProcess(ctx, req, nil)
	if err != nil {
		return nil, err
	}
	inclusionDate := time.Now().Format("2006-01-02")
	simMetrics := marketdata.BuildSnapshotMetrics(processed.Points, inclusionDate, data.PointType, data.SourceName)
	simYears := marketdata.SelectSimulationYears(processed.Points, processed.Annual, inclusionDate)
	excludedYears := excludedYearLabels(processed.Annual, simYears)
	return map[string]any{
		"preview": true,
		"instrument": map[string]any{
			"code": req.Code, "name": data.Name, "market": req.Market,
			"instrument_type":      req.InstrumentType,
			"asset_class":          classifyPreview(data),
			"currency":             data.Currency,
			"point_type":           data.PointType,
			"expense_ratio_status": data.ExpenseRatioStatus,
			"fee_treatment":        marketdata.FeeTreatmentForType(req.InstrumentType),
		},
		"quality_status":    processed.Quality,
		"point_count":       len(processed.Points),
		"annual_returns":    toAnnualDTO(processed.Annual),
		"data_as_of":        lastDate(processed.Points),
		"provider_symbol":   data.ProviderSymbol,
		"source_name":       data.SourceName,
		"has_daily_anomaly": processed.HasAnomaly,
		"simulation_window": map[string]any{
			"inclusion_date":        inclusionDate,
			"complete_year_count":   simMetrics.CompleteYearCount,
			"complete_year_start":   simMetrics.CompleteYearStart,
			"complete_year_end":     simMetrics.CompleteYearEnd,
			"excluded_years":        excludedYears,
			"modeled_annual_return": simMetrics.ModeledAnnualReturn,
			"annual_volatility":     simMetrics.AnnualVolatility,
			"max_drawdown":          simMetrics.MaxDrawdown,
			"quality_status":        simMetrics.QualityStatus,
		},
	}, nil
}

func (s *InstrumentService) Import(ctx context.Context, req InstrumentImportRequest) (repository.InstrumentRecord, error) {
	if err := validateImportRequest(req); err != nil {
		return repository.InstrumentRecord{}, err
	}
	adjust := marketdata.DefaultAdjustPolicy(req.InstrumentType)
	if existing, err := s.instRepo.FindByKey(ctx, req.Market, req.InstrumentType, req.Code, adjust); err == nil {
		return existing, newErr("instrument_already_exists", "instrument already imported", map[string]any{"instrument_id": existing.ID})
	} else if !errors.Is(err, repository.ErrInstrumentNotFound) {
		return repository.InstrumentRecord{}, err
	}

	data, processed, err := s.fetchAndProcess(ctx, req, nil)
	if err != nil {
		return repository.InstrumentRecord{}, err
	}
	class, err := marketdata.ResolveClassification(req.Market, req.InstrumentType, data)
	if err != nil {
		return repository.InstrumentRecord{}, mapClassifyError(err)
	}
	if processed.HasAnomaly {
		return repository.InstrumentRecord{}, newErr("provider_data_anomaly", "provider data contains abnormal daily returns", nil)
	}
	if processed.Quality == "insufficient_history" {
		// Partial annual rows remain visible in history but are excluded from simulation.
	}

	id := "ins_" + uuid.New().String()
	inst := repository.InstrumentRecord{
		ID: id, Code: strings.TrimSpace(req.Code), Name: data.Name,
		Market: req.Market, InstrumentType: req.InstrumentType,
		AssetClass: class.AssetClass, Region: class.Region, Currency: class.Currency,
		Provider: "akshare", ProviderSymbol: data.ProviderSymbol, AdjustPolicy: adjust,
		ExpenseRatio:       marketdata.ExpenseRatioFromComponents(data.ExpenseRatioComponents),
		ExpenseRatioStatus: data.ExpenseRatioStatus,
		FeeTreatment:       marketdata.FeeTreatmentForType(req.InstrumentType),
		Status:             "active",
	}
	points := toRepoPoints(id, processed.Points)
	annual := toRepoAnnual(id, processed.Annual)

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.instRepo.Create(ctx, tx, inst); err != nil {
			return err
		}
		if err := s.marketRepo.UpsertBatch(ctx, tx, id, points); err != nil {
			return err
		}
		return s.annualRepo.ReplaceAll(ctx, tx, id, annual)
	})
	if err != nil {
		return repository.InstrumentRecord{}, err
	}
	inst.QualityStatus = processed.Quality
	inst.DataAsOf = lastDate(processed.Points)
	inst.DataSourceName = data.SourceName
	inst.PointType = data.PointType
	applyDataStale(&inst, inst.DataAsOf)
	return inst, nil
}

// InstrumentRefreshOptions controls instrument refresh behavior.
type InstrumentRefreshOptions struct {
	Force bool `json:"force"`
}

func (s *InstrumentService) Refresh(ctx context.Context, instrumentID string, opts InstrumentRefreshOptions) (repository.InstrumentRecord, error) {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return repository.InstrumentRecord{}, newErr("instrument_not_found", "instrument not found", nil)
		}
		return repository.InstrumentRecord{}, err
	}
	if inst.IsSystem || inst.Provider != "akshare" {
		return repository.InstrumentRecord{}, newErr("instrument_not_refreshable", "only AKShare instruments can be refreshed", nil)
	}
	lastFetched, _ := s.marketRepo.LastFetchedAt(ctx, instrumentID)
	namePlaceholder := inst.Name == "" || inst.Name == inst.Code
	if !opts.Force && lastFetched > 0 && time.Now().UnixMilli()-lastFetched < 24*time.Hour.Milliseconds() && !namePlaceholder {
		return inst, newErr("instrument_refresh_throttled", "instrument refreshed within last 24 hours", nil)
	}
	if opts.Force {
		slog.InfoContext(ctx, "instrument force refresh",
			"instrument_id", instrumentID,
			"code", inst.Code,
		)
	}

	existingRows, err := s.marketRepo.ListByInstrument(ctx, instrumentID)
	if err != nil {
		return repository.InstrumentRecord{}, err
	}
	existing := repoToDataPoints(existingRows)
	fullReplace := marketdata.ShouldFullReplaceOnRefresh(opts.Force, existing, "")

	lastDate, _ := s.marketRepo.LastTradeDate(ctx, instrumentID)
	end := time.Now().Format("2006-01-02")
	var start *string
	if !fullReplace && lastDate != "" {
		overlap, err := marketdata.RefreshStartDate(lastDate)
		if err == nil {
			start = &overlap
		}
	}
	req := InstrumentImportRequest{Market: inst.Market, InstrumentType: inst.InstrumentType, Code: inst.Code}
	data, processed, err := s.fetchAndProcess(ctx, req, start)
	if err != nil {
		// upstream failure: keep existing data
		return inst, newErr("market_provider_unavailable", err.Error(), nil)
	}
	if processed.HasAnomaly {
		return inst, newErr("provider_data_anomaly", "refresh rejected due to abnormal daily returns", nil)
	}

	fullReplace = marketdata.ShouldFullReplaceOnRefresh(opts.Force, existing, processed.SourceName)
	var reprocessed marketdata.ProcessFetchResult
	if fullReplace {
		reprocessed = processed
		if opts.Force {
			slog.InfoContext(ctx, "instrument full history replace on force refresh",
				"instrument_id", instrumentID,
				"source", processed.SourceName,
				"points", len(processed.Points),
			)
		} else {
			slog.InfoContext(ctx, "instrument full history replace on source change",
				"instrument_id", instrumentID,
				"from", marketdata.DominantSourceName(existing),
				"to", processed.SourceName,
			)
		}
	} else {
		merged := marketdata.MergeRefreshedPoints(existing, processed.Points)
		reprocessed = marketdata.ProcessProviderData(&marketdata.FetchData{
			Points: toHistorical(merged), PointType: processed.PointType, SourceName: processed.SourceName,
		}, end)
	}

	newName := strings.TrimSpace(data.Name)
	shouldUpdateName := newName != "" && newName != inst.Code && newName != inst.Name

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if fullReplace {
			if err := s.marketRepo.DeleteAllTx(ctx, tx, instrumentID); err != nil {
				return err
			}
		}
		if err := s.marketRepo.UpsertBatch(ctx, tx, instrumentID, toRepoPoints(instrumentID, reprocessed.Points)); err != nil {
			return err
		}
		if err := s.annualRepo.ReplaceAll(ctx, tx, instrumentID, toRepoAnnual(instrumentID, reprocessed.Annual)); err != nil {
			return err
		}
		if shouldUpdateName {
			if err := s.instRepo.UpdateNameTx(ctx, tx, instrumentID, newName); err != nil {
				return err
			}
		}
		return s.instRepo.TouchUpdated(ctx, tx, instrumentID)
	})
	if err != nil {
		return repository.InstrumentRecord{}, err
	}
	return s.Get(ctx, instrumentID)
}

func (s *InstrumentService) Delete(ctx context.Context, instrumentID string) error {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return newErr("instrument_not_found", "instrument not found", nil)
		}
		return err
	}
	if inst.IsSystem || inst.Provider != "akshare" {
		return newErr("instrument_not_deletable", "system instruments cannot be deleted", nil)
	}
	ref, err := s.instRepo.IsReferencedByPlan(ctx, instrumentID)
	if err != nil {
		return err
	}
	if ref {
		return newErr("instrument_in_use", "instrument is referenced by a plan holding", nil)
	}
	if err := s.instRepo.Delete(ctx, instrumentID); err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return newErr("instrument_not_found", "instrument not found", nil)
		}
		return err
	}
	return nil
}

func (s *InstrumentService) AnnualReturns(ctx context.Context, instrumentID string, inclusionDate string) ([]repository.AnnualReturnRecord, error) {
	if _, err := s.instRepo.GetByID(ctx, instrumentID); err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return nil, newErr("instrument_not_found", "instrument not found", nil)
		}
		return nil, err
	}
	rows, err := s.annualRepo.ListByInstrument(ctx, instrumentID)
	if err != nil {
		return nil, err
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

// InstrumentDetailView aggregates asset library detail for the UI.
type InstrumentDetailView struct {
	Instrument          repository.InstrumentRecord          `json:"instrument"`
	AnnualReturns       []repository.AnnualReturnRecord      `json:"annual_returns"`
	SimulationWindow    map[string]any                       `json:"simulation_window"`
	HistoricalSnapshots []repository.SimulationSnapshot      `json:"historical_snapshots"`
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
	annualRows := toMarketAnnualFromRepo(returns)
	simYears := marketdata.SelectSimulationYears(repoToDataPoints(points), annualRows, inclusionDate)
	simMetrics := marketdata.BuildSnapshotMetrics(repoToDataPoints(points), inclusionDate, "adjusted_close", "library")
	excluded := excludedYearLabels(marketdata.ComputeAnnualReturns(repoToDataPoints(points)), simYears)
	selectedYears := make([]int, len(simYears))
	for i, y := range simYears {
		selectedYears[i] = y.Year
	}
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
		excluded = []int{}
	}
	return InstrumentDetailView{
		Instrument: inst, AnnualReturns: returns,
		SimulationWindow: map[string]any{
			"inclusion_date": inclusionDate, "selected_years": selectedYears,
			"excluded_years": excluded, "complete_year_count": simMetrics.CompleteYearCount,
			"historical_cagr": simMetrics.HistoricalCAGR, "annual_volatility": simMetrics.AnnualVolatility,
			"max_drawdown": simMetrics.MaxDrawdown, "observation_count": simMetrics.ObservationCount,
			"fee_treatment": inst.FeeTreatment, "expense_ratio_status": inst.ExpenseRatioStatus,
			"quality_status": simMetrics.QualityStatus,
		},
		HistoricalSnapshots: snaps, ReferencingPlans: refs,
	}, nil
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

func (s *InstrumentService) fetchAndProcess(ctx context.Context, req InstrumentImportRequest, start *string) (*marketdata.FetchData, marketdata.ProcessFetchResult, error) {
	end := time.Now().Format("2006-01-02")
	fetchReq := marketdata.FetchRequest{
		Market: req.Market, InstrumentType: req.InstrumentType,
		SourceCode: strings.TrimSpace(req.Code), StartDate: start, EndDate: end,
		AdjustPolicy: marketdata.DefaultAdjustPolicy(req.InstrumentType),
	}
	data, err := s.provider.Fetch(ctx, fetchReq)
	if err != nil {
		slog.WarnContext(ctx, "instrument fetch failed",
			"market", req.Market,
			"instrument_type", req.InstrumentType,
			"code", req.Code,
			"error", err,
		)
		return nil, marketdata.ProcessFetchResult{}, newErr("market_provider_unavailable", err.Error(), nil)
	}
	if _, err := marketdata.ResolveClassification(req.Market, req.InstrumentType, data); err != nil {
		return nil, marketdata.ProcessFetchResult{}, mapClassifyError(err)
	}
	processed := marketdata.ProcessProviderData(data, end)
	return data, processed, nil
}

func validateImportRequest(req InstrumentImportRequest) error {
	req.Code = strings.TrimSpace(req.Code)
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

func (s *InstrumentService) libraryQuality(ctx context.Context, instrumentID string) string {
	points, err := s.marketRepo.ListByInstrument(ctx, instrumentID)
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

func classifyPreview(data *marketdata.FetchData) string { return data.AssetClass }

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

func toAnnualDTO(rows []marketdata.AnnualReturnRow) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{
			"year": r.Year, "annual_return": r.AnnualReturn,
			"start_date": r.StartDate, "end_date": r.EndDate,
			"observations": r.Observations, "is_partial": r.IsPartial,
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

func lastDate(points []marketdata.DataPoint) string {
	if len(points) == 0 {
		return ""
	}
	return points[len(points)-1].TradeDate
}

func excludedYearLabels(all []marketdata.AnnualReturnRow, selected []marketdata.SimulationYear) []int {
	selectedSet := map[int]struct{}{}
	for _, y := range selected {
		selectedSet[y.Year] = struct{}{}
	}
	var out []int
	for _, row := range all {
		if row.IsPartial {
			out = append(out, row.Year)
			continue
		}
		if _, ok := selectedSet[row.Year]; !ok {
			out = append(out, row.Year)
		}
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
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	readOnly := []string{
		"name", "asset_class", "region", "currency", "expense_ratio",
		"historical_cagr", "annual_volatility", "max_drawdown", "correlation",
	}
	for _, f := range readOnly {
		if _, ok := raw[f]; ok {
			return newErr("instrument_fields_read_only", "instrument metadata and metrics are read-only", map[string]any{"field": f})
		}
	}
	return nil
}

// MapSnapshotError converts snapshot errors to AppError.
func MapSnapshotError(err error) error {
	var se *marketdata.SnapshotError
	if errors.As(err, &se) {
		return newErr(se.Code, se.Message, nil)
	}
	return err
}
