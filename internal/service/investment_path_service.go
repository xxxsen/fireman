package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/investmentpath"
	"github.com/fireman/fireman/internal/repository"
)

type InvestmentPathAssetInput struct {
	AssetKey     string `json:"asset_key"`
	AdjustPolicy string `json:"adjust_policy"`
	PointType    string `json:"point_type"`
}

type InvestmentPathRequest struct {
	Mode                string                                `json:"mode"`
	Asset               InvestmentPathAssetInput              `json:"asset"`
	BaseCurrency        string                                `json:"base_currency"`
	EvaluationStart     string                                `json:"evaluation_start"`
	EvaluationEnd       string                                `json:"evaluation_end"`
	HorizonMonths       int                                   `json:"horizon_months"`
	PrimaryStart        string                                `json:"primary_start,omitempty"`
	MonthlyDay          int                                   `json:"monthly_day"`
	TransactionCostRate float64                               `json:"transaction_cost_rate"`
	IncomeDCA           *investmentpath.IncomeDCAConfig       `json:"income_dca,omitempty"`
	ExistingCapital     *investmentpath.ExistingCapitalConfig `json:"existing_capital,omitempty"`
	IdempotencyKey      string                                `json:"idempotency_key,omitempty"`
}

type InvestmentPathIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type InvestmentPathReadiness struct {
	Ready    bool                     `json:"ready"`
	Issues   []InvestmentPathIssue    `json:"issues"`
	Warnings []InvestmentPathIssue    `json:"warnings"`
	Resolved *investmentpath.Resolved `json:"resolved,omitempty"`
}

type InvestmentPathRunView struct {
	repository.InvestmentPathRun
	Task        repository.WorkerTask `json:"task"`
	Summary     any                   `json:"summary"`
	DataQuality any                   `json:"data_quality"`
	Strategies  []string              `json:"strategies"`
}

type InvestmentPathCreateResult struct {
	Run    InvestmentPathRunView `json:"run"`
	Reused bool                  `json:"reused"`
}

type investmentPathSourceSnapshot struct {
	Asset       investmentPathAssetSourceIdentity       `json:"asset"`
	History     investmentPathHistorySourceIdentity     `json:"history"`
	AssetPoints []repository.MarketAssetPoint           `json:"asset_points"`
	FX          map[string][]repository.MarketDataPoint `json:"fx"`
}

type investmentPathAssetSourceIdentity struct {
	AssetKey       string `json:"asset_key"`
	InstrumentType string `json:"instrument_type"`
	Currency       string `json:"currency"`
	SourceName     string `json:"source_name"`
}

type investmentPathHistorySourceIdentity struct {
	AdjustPolicy string `json:"adjust_policy"`
	PointType    string `json:"point_type"`
	DataAsOf     string `json:"data_as_of"`
	PointCount   int    `json:"point_count"`
	SourceName   string `json:"source_name"`
}

type investmentPathInputSnapshot struct {
	EngineVersion string                  `json:"engine_version"`
	Request       InvestmentPathRequest   `json:"request"`
	Resolved      investmentpath.Resolved `json:"resolved"`
	AssetName     string                  `json:"asset_name"`
	AssetCurrency string                  `json:"asset_currency"`
	SourceHash    string                  `json:"source_hash"`
	SourceSummary map[string]any          `json:"source_summary"`
	Algorithms    map[string]string       `json:"algorithms"`
}

type preparedInvestmentPath struct {
	request    InvestmentPathRequest
	input      investmentpath.Input
	resolved   investmentpath.Resolved
	asset      repository.MarketAsset
	sourceHash string
	snapshot   investmentPathInputSnapshot
	warnings   []InvestmentPathIssue
}

func (s *ResearchService) GetInvestmentPathReadiness(
	ctx context.Context, request InvestmentPathRequest,
) (InvestmentPathReadiness, error) {
	prepared, err := s.prepareInvestmentPath(ctx, request)
	if err == nil {
		return InvestmentPathReadiness{
			Ready: true, Issues: []InvestmentPathIssue{}, Warnings: prepared.warnings, Resolved: &prepared.resolved,
		}, nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return InvestmentPathReadiness{
			Ready: false, Issues: []InvestmentPathIssue{{Code: appErr.Code, Message: appErr.Message}},
			Warnings: []InvestmentPathIssue{},
		}, nil
	}
	return InvestmentPathReadiness{}, err
}

//nolint:funlen,gocognit,gocyclo,lll // Readiness must derive asset, FX, canonical windows and hashes as one frozen identity.
func (s *ResearchService) prepareInvestmentPath(
	ctx context.Context, request InvestmentPathRequest,
) (*preparedInvestmentPath, error) {
	if !researchBaseCurrencies[request.BaseCurrency] {
		return nil, newErr("investment_path_invalid_request", "不支持该基准币种", nil)
	}
	if request.IdempotencyKey != "" {
		if _, err := uuid.Parse(request.IdempotencyKey); err != nil {
			return nil, newErr("investment_path_invalid_request", "idempotency_key 必须是 UUID", nil)
		}
	}
	asset, err := s.assets.GetByKey(ctx, request.Asset.AssetKey)
	if err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return nil, newErr("investment_path_asset_not_found", "资产不存在", nil)
		}
		return nil, wrapRepo("load investment path asset", err)
	}
	if !asset.Active || isSystemCashAsset(asset) {
		return nil, newErr("investment_path_invalid_request", "只能选择 active 的非现金资产", nil)
	}
	state, ok, err := s.assets.GetHistoryState(ctx, asset.AssetKey, request.Asset.AdjustPolicy, request.Asset.PointType)
	if err != nil {
		return nil, wrapRepo("load investment path history state", err)
	}
	if !ok {
		return nil, newErr("investment_path_history_missing", "指定的复权与点位历史不存在", nil)
	}
	assetPoints, err := s.assets.ListPoints(ctx, asset.AssetKey, request.Asset.AdjustPolicy, request.Asset.PointType)
	if err != nil {
		return nil, wrapRepo("list investment path history", err)
	}
	if len(assetPoints) == 0 {
		return nil, newErr("investment_path_history_missing", "资产没有可用历史", nil)
	}
	fxRaw := map[string][]repository.MarketDataPoint{}
	fxSeries := map[string]preparedSeries{}
	for _, pair := range ResearchFXPairsFor(asset.Currency, request.BaseCurrency) {
		fx, loadErr := s.loadFXData(ctx, pair)
		if loadErr != nil {
			return nil, loadErr
		}
		if !fx.Found || fx.NonPositiveCount > 0 {
			return nil, newErr("investment_path_fx_missing", "缺少可用汇率历史: "+pair, nil)
		}
		series, prepareErr := prepareSeries(fxPointsToSeries(fx.Points))
		if prepareErr != nil {
			return nil, newErr("investment_path_fx_missing", "汇率历史不可用: "+pair, nil)
		}
		fxRaw[pair], fxSeries[pair] = fx.Points, series
	}
	converter, _, err := prepareFXConverter(asset.AssetKey, asset.Currency, request.BaseCurrency, fxSeries)
	if err != nil {
		return nil, newErr("investment_path_fx_missing", err.Error(), nil)
	}
	assetSeries, err := prepareSeries(assetPointsToSeries(assetPoints))
	if err != nil {
		return nil, newErr("investment_path_invalid_request", "资产历史包含非法点位", nil)
	}
	valuationDays := map[int]bool{}
	for _, point := range assetPoints {
		day, parseErr := parseResearchDate(point.TradeDate)
		if parseErr != nil || point.Value <= 0 || math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
			return nil, newErr("investment_path_invalid_request", "资产历史包含非法点位", nil)
		}
		valuationDays[day] = true
	}
	for _, points := range fxRaw {
		for _, point := range points {
			day, parseErr := parseResearchDate(point.TradeDate)
			if parseErr == nil {
				if _, exists := valuationDays[day]; !exists {
					valuationDays[day] = false
				}
			}
		}
	}
	days := make([]int, 0, len(valuationDays))
	for day := range valuationDays {
		days = append(days, day)
	}
	sort.Ints(days)
	prices := make([]investmentpath.PricePoint, 0, len(days))
	for _, day := range days {
		assetValue, assetFound := assetSeries.valueAt(day)
		rate, found := converter.rateAt(day)
		if !assetFound || !found {
			continue
		}
		if investmentPathFXGapExceeded(converter, day, researchFXFillGapDays) {
			return nil, newErr("investment_path_fx_missing", "汇率前值填充缺口超过容忍范围", nil)
		}
		prices = append(prices, investmentpath.PricePoint{Date: researchDayToDate(day), Value: assetValue * rate, Tradable: assetSeries.hasObservation(day)})
	}
	engineInput := investmentpath.Input{
		Mode: request.Mode, EvaluationStart: request.EvaluationStart, EvaluationEnd: request.EvaluationEnd,
		HorizonMonths: request.HorizonMonths, PrimaryStart: request.PrimaryStart, MonthlyDay: request.MonthlyDay,
		TransactionCostRate: request.TransactionCostRate, IncomeDCA: request.IncomeDCA,
		ExistingCapital: request.ExistingCapital, Prices: prices,
		MaxTailGapDays: ResearchFillGapToleranceDays(asset.InstrumentType),
	}
	canonical, resolved, err := investmentpath.ValidateAndResolve(engineInput)
	if err != nil {
		return nil, mapInvestmentPathEngineError(err)
	}
	request.PrimaryStart = canonical.PrimaryStart
	if request.ExistingCapital != nil {
		request.ExistingCapital.PhaseInMonths = append([]int(nil), canonical.ExistingCapital.PhaseInMonths...)
	}
	source := investmentPathSourceSnapshot{
		Asset: investmentPathAssetSourceIdentity{
			AssetKey: asset.AssetKey, InstrumentType: asset.InstrumentType,
			Currency: asset.Currency, SourceName: asset.SourceName,
		},
		History: investmentPathHistorySourceIdentity{
			AdjustPolicy: state.AdjustPolicy, PointType: state.PointType, DataAsOf: state.DataAsOf,
			PointCount: state.PointCount, SourceName: state.SourceName,
		},
		AssetPoints: assetPoints,
		FX:          fxRaw,
	}
	sourceHash, err := stableJSONHash(source)
	if err != nil {
		return nil, fmt.Errorf("hash investment path source: %w", err)
	}
	summary := map[string]any{
		"asset_points": len(assetPoints), "source_start": prices[0].Date,
		"source_end": prices[len(prices)-1].Date, "fx_pairs": ResearchFXPairsFor(asset.Currency, request.BaseCurrency),
	}
	snapshot := investmentPathInputSnapshot{
		EngineVersion: investmentpath.EngineVersion, Request: request, Resolved: resolved, AssetName: asset.Name,
		AssetCurrency: asset.Currency, SourceHash: sourceHash, SourceSummary: summary,
		Algorithms: map[string]string{"quantile": investmentpath.QuantileVersion, "xirr": "bounded_bisection_v1", "unitization": "pre_flow_nav_v1"},
	}
	warnings := []InvestmentPathIssue{}
	if request.TransactionCostRate == 0 {
		warnings = append(warnings, InvestmentPathIssue{Code: "transaction_cost_is_zero", Message: "交易成本为 0"})
	}
	if request.Asset.AdjustPolicy == "none" || strings.Contains(request.Asset.PointType, "close") && !strings.Contains(request.Asset.PointType, "adjusted") {
		warnings = append(warnings, InvestmentPathIssue{Code: "unadjusted_history", Message: "未复权历史可能不包含分红与拆并股影响"})
	}
	if len(resolved.WindowStarts) < 24 {
		warnings = append(warnings, InvestmentPathIssue{Code: "few_rolling_windows", Message: "完整滚动窗口少于 24 个"})
	}
	return &preparedInvestmentPath{
		request: request, input: canonical, resolved: resolved, asset: asset,
		sourceHash: sourceHash, snapshot: snapshot, warnings: warnings,
	}, nil
}

func investmentPathFXGapExceeded(converter fxConverter, day, tolerance int) bool {
	for _, series := range []preparedSeries{converter.numer, converter.denom} {
		if series.empty() {
			continue
		}
		idx := sort.SearchInts(series.days, day)
		if idx < len(series.days) && series.days[idx] == day {
			continue
		}
		if idx == 0 || day-series.days[idx-1] > tolerance {
			return true
		}
	}
	return false
}

func mapInvestmentPathEngineError(err error) error {
	switch {
	case errors.Is(err, investmentpath.ErrNoCompleteWindow):
		return newErr("investment_path_no_complete_window", "没有完整的滚动窗口", nil)
	case errors.Is(err, investmentpath.ErrTradeBudgetTooSmall):
		return newErr("investment_path_trade_budget_too_small", "买入预算扣费后不为正", nil)
	case errors.Is(err, investmentpath.ErrInvalidInput):
		code := "investment_path_invalid_request"
		if strings.Contains(err.Error(), "budget") || strings.Contains(err.Error(), "exceeds") {
			code = "investment_path_budget_exceeded"
		}
		return newErr(code, err.Error(), nil)
	default:
		return err
	}
}

func stableJSONHash(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func investmentPathInputHash(snapshot investmentPathInputSnapshot) (string, error) {
	canonical := snapshot
	canonical.Request.IdempotencyKey = ""
	return stableJSONHash(canonical)
}

func (s *ResearchService) CreateInvestmentPathRun(
	ctx context.Context, request InvestmentPathRequest,
) (InvestmentPathCreateResult, error) {
	prepared, err := s.prepareInvestmentPath(ctx, request)
	if err != nil {
		return InvestmentPathCreateResult{}, fmt.Errorf("create investment path transaction: %w", err)
	}
	inputHash, err := investmentPathInputHash(prepared.snapshot)
	if err != nil {
		return InvestmentPathCreateResult{}, fmt.Errorf("hash investment path input: %w", err)
	}
	if existing, findErr := s.investmentPaths.FindReusable(ctx, prepared.asset.AssetKey, inputHash); findErr == nil {
		view, viewErr := s.buildInvestmentPathRunView(ctx, existing)
		return InvestmentPathCreateResult{Run: view, Reused: true}, viewErr
	} else if !errors.Is(findErr, repository.ErrInvestmentPathRunNotFound) {
		return InvestmentPathCreateResult{}, wrapRepo("find reusable investment path run", findErr)
	}
	snapshotJSON, err := json.Marshal(prepared.snapshot)
	if err != nil {
		return InvestmentPathCreateResult{}, fmt.Errorf("marshal investment path snapshot: %w", err)
	}
	now := s.now().UnixMilli()
	runID, taskID := "ipr_"+uuid.NewString(), "task_"+uuid.NewString()
	run := repository.InvestmentPathRun{
		ID: runID, TaskID: taskID, AssetKey: prepared.asset.AssetKey, Mode: request.Mode,
		InputHash: inputHash, SourceHash: prepared.sourceHash, InputSnapshotJSON: string(snapshotJSON),
		EngineVersion: investmentpath.EngineVersion, BaseCurrency: request.BaseCurrency,
		EvaluationStart: request.EvaluationStart, EvaluationEnd: request.EvaluationEnd,
		PrimaryStart: prepared.resolved.PrimaryStart, PrimaryEnd: prepared.resolved.PrimaryEnd,
		HorizonMonths: request.HorizonMonths, SummaryJSON: "{}", DataQualityJSON: "{}", CreatedAt: now,
	}
	payload, _ := json.Marshal(map[string]string{"run_id": runID, "asset_key": run.AssetKey})
	task := repository.WorkerTask{
		ID: taskID, WorkerType: repository.WorkerTypeGo, Type: repository.WorkerTaskTypeInvestmentPath,
		Status: repository.WorkerTaskStatusPending, ScopeType: "research_investment_path", ScopeID: run.AssetKey,
		DedupeKey: repository.WorkerTaskTypeInvestmentPath + "|asset:" + run.AssetKey, InputHash: inputHash,
		PayloadJSON:   string(payload),
		ProgressTotal: len(prepared.resolved.WindowStarts)*len(prepared.resolved.StrategyKeys) + 3,
		CreatedAt:     now,
	}
	var bound repository.WorkerTask
	var reused bool
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		var createErr error
		bound, reused, createErr = createOrReuseActiveTaskTx(ctx, tx, s.tasks, s.coordinator, task, func() error {
			return s.investmentPaths.CreateRunTx(ctx, tx, run)
		})
		return createErr
	})
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) && appErr.Code == "task_already_active" {
			return InvestmentPathCreateResult{}, newErr("investment_path_task_active", appErr.Message, appErr.Details)
		}
		return InvestmentPathCreateResult{}, fmt.Errorf("create investment path run: %w", err)
	}
	if reused {
		existing, getErr := s.investmentPaths.GetRunByTaskID(ctx, bound.ID)
		if getErr != nil {
			return InvestmentPathCreateResult{}, wrapRepo("load reused investment path run", getErr)
		}
		view, viewErr := s.buildInvestmentPathRunView(ctx, existing)
		return InvestmentPathCreateResult{Run: view, Reused: true}, viewErr
	}
	view, err := s.buildInvestmentPathRunView(ctx, run)
	return InvestmentPathCreateResult{Run: view}, err
}

func (s *ResearchService) GetInvestmentPathRun(ctx context.Context, runID string) (InvestmentPathRunView, error) {
	run, err := s.investmentPaths.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrInvestmentPathRunNotFound) {
			return InvestmentPathRunView{}, newErr("investment_path_run_not_found", "投入路径实验不存在", nil)
		}
		return InvestmentPathRunView{}, wrapRepo("load investment path run", err)
	}
	return s.buildInvestmentPathRunView(ctx, run)
}

//nolint:lll // Public list signature remains explicit.
func (s *ResearchService) ListInvestmentPathRuns(ctx context.Context, limit, offset int) ([]InvestmentPathRunView, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	runs, err := s.investmentPaths.ListRuns(ctx, limit, offset)
	if err != nil {
		return nil, wrapRepo("list investment path runs", err)
	}
	out := make([]InvestmentPathRunView, 0, len(runs))
	for _, run := range runs {
		view, viewErr := s.buildInvestmentPathRunView(ctx, run)
		if viewErr != nil {
			return nil, viewErr
		}
		out = append(out, view)
	}
	return out, nil
}

//nolint:lll // View construction joins the immutable run and its single lifecycle task.
func (s *ResearchService) buildInvestmentPathRunView(ctx context.Context, run repository.InvestmentPathRun) (InvestmentPathRunView, error) {
	task, err := s.tasks.GetByID(ctx, run.TaskID)
	if err != nil {
		return InvestmentPathRunView{}, wrapRepo("load investment path task", err)
	}
	view := InvestmentPathRunView{InvestmentPathRun: run, Task: task, Summary: map[string]any{}, DataQuality: map[string]any{}}
	_ = json.Unmarshal([]byte(run.SummaryJSON), &view.Summary)
	_ = json.Unmarshal([]byte(run.DataQualityJSON), &view.DataQuality)
	var snapshot investmentPathInputSnapshot
	if json.Unmarshal([]byte(run.InputSnapshotJSON), &snapshot) == nil {
		view.Strategies = snapshot.Resolved.StrategyKeys
	}
	return view, nil
}

func (s *ResearchService) validateInvestmentPathStrategy(ctx context.Context, runID, strategy string) error {
	view, err := s.GetInvestmentPathRun(ctx, runID)
	if err != nil {
		return err
	}
	for _, candidate := range view.Strategies {
		if candidate == strategy {
			return nil
		}
	}
	return newErr("investment_path_invalid_request", "strategy_key 不属于该 run", nil)
}

//nolint:lll // Public result signature remains explicit.
func (s *ResearchService) GetInvestmentPathPoints(ctx context.Context, runID, strategy string) ([]repository.InvestmentPathPoint, error) {
	if err := s.validateInvestmentPathStrategy(ctx, runID, strategy); err != nil {
		return nil, err
	}
	points, err := s.investmentPaths.ListPoints(ctx, runID, strategy)
	if err != nil {
		return nil, wrapRepo("list investment path points", err)
	}
	return points, nil
}

//nolint:lll // Public result signature remains explicit.
func (s *ResearchService) GetInvestmentPathTrades(ctx context.Context, runID, strategy string) ([]repository.InvestmentPathTrade, error) {
	if err := s.validateInvestmentPathStrategy(ctx, runID, strategy); err != nil {
		return nil, err
	}
	trades, err := s.investmentPaths.ListTrades(ctx, runID, strategy)
	if err != nil {
		return nil, wrapRepo("list investment path trades", err)
	}
	return trades, nil
}

func (s *ResearchService) GetInvestmentPathWindows(
	ctx context.Context, runID, strategy string, limit, offset int,
) ([]repository.InvestmentPathWindow, error) {
	if err := s.validateInvestmentPathStrategy(ctx, runID, strategy); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	windows, err := s.investmentPaths.ListWindows(ctx, runID, strategy, limit, offset)
	if err != nil {
		return nil, wrapRepo("list investment path windows", err)
	}
	return windows, nil
}

func (s *ResearchService) ExportInvestmentPathCSV(ctx context.Context, runID string) (string, string, error) {
	view, err := s.GetInvestmentPathRun(ctx, runID)
	if err != nil {
		return "", "", err
	}
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{
		"record_type", "strategy_key", "date", "end_date", "terminal_value_minor",
		"profit_minor", "xirr", "twr_annualized", "max_drawdown", "trade_side", "trade_reason",
		"gross_trade_minor", "fee_minor",
	})
	for _, strategy := range view.Strategies {
		windows, listErr := s.investmentPaths.ListWindows(ctx, runID, strategy, 1000, 0)
		if listErr != nil {
			return "", "", wrapRepo("export investment path windows", listErr)
		}
		for _, window := range windows {
			xirr := ""
			if window.XIRR != nil {
				xirr = fmt.Sprintf("%.10g", *window.XIRR)
			}
			_ = writer.Write([]string{
				"window", strategy, window.WindowStart, window.WindowEnd,
				fmt.Sprint(window.TerminalValueMinor), fmt.Sprint(window.ProfitMinor), xirr,
				fmt.Sprintf("%.10g", window.TWRAnnualized), fmt.Sprintf("%.10g", window.MaxDrawdown), "", "", "", "",
			})
		}
		trades, listErr := s.investmentPaths.ListTrades(ctx, runID, strategy)
		if listErr != nil {
			return "", "", wrapRepo("export investment path trades", listErr)
		}
		for _, trade := range trades {
			_ = writer.Write([]string{
				"trade", strategy, trade.TradeDate, "", "", "", "", "", "",
				trade.Side, trade.Reason, fmt.Sprint(trade.GrossTradeMinor), fmt.Sprint(trade.FeeMinor),
			})
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", "", err
	}
	return "investment-path-" + runID + ".csv", buffer.String(), nil
}

//nolint:funlen,gocyclo,lll // Worker execution keeps source verification and one-transaction publication together.
func (s *ResearchService) ExecuteInvestmentPathTaskOwned(
	ctx context.Context, taskID string, canceled func() bool, progress func(int, int, string), complete func(*sql.Tx) error,
) error {
	run, err := s.investmentPaths.GetRunByTaskID(ctx, taskID)
	if err != nil {
		return wrapRepo("load investment path task run", err)
	}
	var frozen investmentPathInputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &frozen); err != nil {
		return fmt.Errorf("decode investment path snapshot: %w", err)
	}
	total := len(frozen.Resolved.WindowStarts)*len(frozen.Resolved.StrategyKeys) + 3
	progress(0, total, "loading")
	prepared, err := s.prepareInvestmentPath(ctx, frozen.Request)
	if err != nil {
		return err
	}
	if canceled() {
		return context.Canceled
	}
	progress(1, total, "verifying")
	inputHash, err := investmentPathInputHash(prepared.snapshot)
	if err != nil {
		return err
	}
	if prepared.sourceHash != run.SourceHash || inputHash != run.InputHash {
		return newErr("investment_path_source_changed", "排队后市场数据已变化，请重新创建实验", nil)
	}
	result, err := investmentpath.Run(ctx, prepared.input, func(done, _ int) error {
		if canceled() {
			return context.Canceled
		}
		progress(done+2, total, "rolling_windows")
		return nil
	})
	if err != nil {
		return mapInvestmentPathEngineError(err)
	}
	if canceled() {
		return context.Canceled
	}
	progress(total-1, total, "persisting")
	summaryJSON, err := json.Marshal(map[string]any{"primary": result.Primary, "aggregates": result.Aggregates, "warnings": result.Warnings})
	if err != nil {
		return err
	}
	qualityJSON, _ := json.Marshal(frozen.SourceSummary)
	points := make([]repository.InvestmentPathPoint, len(result.Points))
	for i, p := range result.Points {
		points[i] = repository.InvestmentPathPoint{
			RunID: run.ID, StrategyKey: p.StrategyKey, ValuationDate: p.ValuationDate,
			AccountValueMinor: p.AccountValueMinor, AssetValueMinor: p.AssetValueMinor, CashValueMinor: p.CashValueMinor,
			CumulativeExternalContributionMinor: p.CumulativeContributionMinor, UnitNAV: p.UnitNAV, Drawdown: p.Drawdown,
		}
	}
	trades := make([]repository.InvestmentPathTrade, len(result.Trades))
	for i, trade := range result.Trades {
		trades[i] = repository.InvestmentPathTrade{
			RunID: run.ID, StrategyKey: trade.StrategyKey, SequenceNo: trade.SequenceNo,
			TradeDate: trade.TradeDate, Side: trade.Side, Reason: trade.Reason, GrossTradeMinor: trade.GrossTradeMinor,
			FeeMinor: trade.FeeMinor, AssetValueDeltaMinor: trade.AssetValueDeltaMinor, CashDeltaMinor: trade.CashDeltaMinor,
		}
	}
	windows := make([]repository.InvestmentPathWindow, len(result.Windows))
	for i, w := range result.Windows {
		windows[i] = repository.InvestmentPathWindow{
			RunID: run.ID, StrategyKey: w.StrategyKey, WindowStart: w.WindowStart,
			WindowEnd: w.WindowEnd, TotalContributionMinor: w.TotalContributionMinor, TerminalValueMinor: w.TerminalValueMinor,
			ProfitMinor: w.ProfitMinor, XIRR: w.XIRR, XIRRReason: w.XIRRReason, TWRTotal: w.TWRTotal,
			TWRAnnualized: w.TWRAnnualized, MaxDrawdown: w.MaxDrawdown, MaxDrawdownStart: w.MaxDrawdownStart,
			MaxDrawdownEnd: w.MaxDrawdownEnd, LongestUnderwaterDays: w.LongestUnderwaterDays,
			MaxPrincipalDeficitMinor: w.MaxPrincipalDeficitMinor, MaxPrincipalDeficitRatio: w.MaxPrincipalDeficitRatio,
			LongestBelowPrincipalDays: w.LongestBelowPrincipalDays, FirstRecoveryAbovePrincipalDate: w.FirstRecoveryAbovePrincipalDate,
			AverageCashWeight: w.AverageCashWeight, TotalTransactionCostMinor: w.TotalTransactionCostMinor,
			TradeCount: w.TradeCount, Turnover: w.Turnover, DeploymentCompleteDate: w.DeploymentCompleteDate,
		}
	}
	completedAt := time.Now().UnixMilli()
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.investmentPaths.CompleteTx(ctx, tx, run.ID, string(summaryJSON), string(qualityJSON), completedAt, points, trades, windows); err != nil {
			return fmt.Errorf("persist investment path result: %w", err)
		}
		return complete(tx)
	})
	if err != nil {
		return fmt.Errorf("complete investment path transaction: %w", err)
	}
	return nil
}
