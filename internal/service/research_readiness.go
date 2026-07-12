package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

// research_readiness.go implements the backtest admission check (td/099 §7):
// the only gate for creating a research backtest run. The dataset loader
// pulls everything from the local DB; evaluateResearchReadiness is pure so
// unit tests can construct datasets directly.

// Blocking reason codes.
const (
	ResearchReasonNoEnabledAssets     = "no_enabled_assets"
	ResearchReasonWeightSumInvalid    = "weight_sum_invalid"
	ResearchReasonNegativeWeight      = "negative_weight"
	ResearchReasonWeightExceeds100    = "weight_exceeds_100"
	ResearchReasonHistoryMissing      = "history_missing"
	ResearchReasonHistorySyncing      = "history_syncing"
	ResearchReasonHistorySyncFailed   = "history_sync_failed"
	ResearchReasonWindowEmpty         = "common_window_empty"
	ResearchReasonWindowTooShort      = "common_window_too_short"
	ResearchReasonFXMissing           = "fx_missing"
	ResearchReasonFXSyncing           = "fx_syncing"
	ResearchReasonFXGapExceeded       = "fx_gap_exceeded"
	ResearchReasonNonPositivePoints   = "non_positive_points"
	ResearchReasonMixedSources        = "mixed_sources"
	ResearchReasonBenchmarkNoHistory  = "benchmark_history_missing"
	ResearchReasonBenchmarkWindow     = "benchmark_window_not_covered"
	ResearchReasonBenchmarkGap        = "benchmark_gap_exceeded"
	ResearchReasonTooFewEffectiveDays = "too_few_effective_days"
	ResearchReasonCVARSample          = "cvar_sample_insufficient"
	ResearchReasonUnadjustedSeries    = "unadjusted_price_series"
	ResearchReasonUnsupportedSeries   = "unsupported_return_series"
	ResearchReasonDuplicateFund       = "duplicate_canonical_fund"
)

// Warning reason codes.
const (
	ResearchWarnShortWindow           = "short_common_window"
	ResearchWarnStaleData             = "stale_data"
	ResearchWarnDataLagging           = "data_lagging"
	ResearchWarnAssetInactive         = "asset_inactive"
	ResearchWarnExcessiveFill         = "excessive_forward_fill"
	ResearchWarnSyncFailedStale       = "history_sync_failed_stale"
	ResearchWarnWeightConcentration   = "weight_concentration"
	ResearchWarnMarketConcentration   = "market_concentration"
	ResearchWarnCurrencyConcentration = "currency_concentration"
	ResearchWarnHighCorrelation       = "high_correlation"
)

// Concentration thresholds (warnings only).
const (
	researchWeightConcentrationLimit   = 0.7
	researchGroupConcentrationLimit    = 0.9
	researchHighCorrelationLimit       = 0.95
	researchCorrelationMinSamples      = 30
	researchCorrelationMaxAssets       = 8
	researchSystemCashInstrumentType   = "cash"
	researchDataLaggingSlackDays       = 30
	researchReadinessMinEffectiveObs   = 3
	researchReadinessMinWindowDays     = researchMinWindowDays
	researchReadinessShortWindowDays   = researchShortWindowDays
	researchReadinessFXFillGapDays     = researchFXFillGapDays
	researchReadinessCorrelationSample = researchCorrelationMinSamples
)

// ResearchReadinessIssue is one blocking reason or warning.
type ResearchReadinessIssue struct {
	AssetKey string `json:"asset_key,omitempty"`
	Pair     string `json:"pair,omitempty"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
}

// ResearchDataDependencies summarizes what the backtest depends on.
type ResearchDataDependencies struct {
	AssetCount          int      `json:"asset_count"`
	FXPairs             []string `json:"fx_pairs"`
	StaleAssetCount     int      `json:"stale_asset_count"`
	MissingHistoryCount int      `json:"missing_history_count"`
}

// ResearchReadinessAssetView is the per-asset data status block for the
// collection page's data workspace.
type ResearchReadinessAssetView struct {
	ItemID            string   `json:"item_id"`
	AssetKey          string   `json:"asset_key"`
	Name              string   `json:"name"`
	Currency          string   `json:"currency"`
	IsCash            bool     `json:"is_cash"`
	Enabled           bool     `json:"enabled"`
	Weight            float64  `json:"weight"`
	AdjustPolicy      string   `json:"adjust_policy"`
	PointType         string   `json:"point_type"`
	ListingStatus     string   `json:"listing_status"`
	HasHistory        bool     `json:"has_history"`
	HistoryStart      string   `json:"history_start,omitempty"`
	HistoryEnd        string   `json:"history_end,omitempty"`
	PointCount        int      `json:"point_count"`
	DataAsOf          string   `json:"data_as_of,omitempty"`
	Stale             bool     `json:"stale"`
	SyncStatus        string   `json:"sync_status,omitempty"`
	SyncError         string   `json:"sync_error,omitempty"`
	FXPairs           []string `json:"fx_pairs,omitempty"`
	LimitsCommonStart bool     `json:"limits_common_start,omitempty"`
	LimitsCommonEnd   bool     `json:"limits_common_end,omitempty"`
}

// ResearchReadiness is the GET /collections/{id}/readiness response
// (td/099 §5.4).
type ResearchReadiness struct {
	Ready            bool                         `json:"ready"`
	WeightSum        float64                      `json:"weight_sum"`
	CommonStart      string                       `json:"common_start,omitempty"`
	CommonEnd        string                       `json:"common_end,omitempty"`
	WindowStart      string                       `json:"window_start,omitempty"`
	WindowEnd        string                       `json:"window_end,omitempty"`
	BlockingReasons  []ResearchReadinessIssue     `json:"blocking_reasons"`
	Warnings         []ResearchReadinessIssue     `json:"warnings"`
	DataDependencies ResearchDataDependencies     `json:"data_dependencies"`
	Assets           []ResearchReadinessAssetView `json:"assets"`
	TailRisk         *ResearchTailRiskReadiness   `json:"tail_risk,omitempty"`
}

type ResearchTailRiskReadiness struct {
	Confidence           float64 `json:"confidence"`
	HorizonDays          int     `json:"horizon_days"`
	EffectiveReturnCount int     `json:"effective_return_count"`
	ScenarioCount        int     `json:"scenario_count"`
	MinimumScenarioCount int     `json:"minimum_scenario_count"`
}

// --- dataset ---

// researchAssetData is one enabled collection item with everything readiness
// and backtest creation need.
type researchAssetData struct {
	Item     repository.ResearchCollectionItem
	Asset    repository.MarketAsset
	IsCash   bool
	HasState bool
	State    repository.MarketAssetHistoryState
	Task     *repository.WorkerTask
	Points   []repository.MarketAssetPoint
	// Derived from Points.
	SourceNames      []string
	NonPositiveCount int
	FXPairs          []string
}

// researchFXData is one required FX pair's stored history.
type researchFXData struct {
	Pair             string
	Found            bool
	SourceName       string
	Points           []repository.MarketDataPoint
	NonPositiveCount int
}

// researchDataset is the full local snapshot backing one readiness check or
// backtest creation.
type researchDataset struct {
	Collection   repository.ResearchCollection
	Items        []repository.ResearchCollectionItem
	Enabled      []researchAssetData
	FX           map[string]*researchFXData
	FXPairs      []string
	FXSyncActive bool
	Benchmark    *researchAssetData
}

func isSystemCashAsset(a repository.MarketAsset) bool {
	return a.InstrumentType == researchSystemCashInstrumentType
}

// loadResearchDataset assembles the dataset for one collection from local
// tables only.
func (s *ResearchService) loadResearchDataset(
	ctx context.Context, collection repository.ResearchCollection,
) (*researchDataset, error) {
	items, err := s.research.ListItems(ctx, collection.ID)
	if err != nil {
		return nil, wrapRepo("list research items", err)
	}
	ds := &researchDataset{
		Collection: collection,
		Items:      items,
		FX:         map[string]*researchFXData{},
	}

	fxNeeded := map[string]bool{}
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		data, err := s.loadResearchAssetData(ctx, collection, item)
		if err != nil {
			return nil, err
		}
		for _, pair := range data.FXPairs {
			fxNeeded[pair] = true
		}
		ds.Enabled = append(ds.Enabled, data)
	}

	if collection.BenchmarkAssetKey != "" {
		bench, err := s.loadBenchmarkData(ctx, collection)
		if err != nil {
			return nil, err
		}
		for _, pair := range bench.FXPairs {
			fxNeeded[pair] = true
		}
		ds.Benchmark = &bench
	}

	for pair := range fxNeeded {
		fx, err := s.loadFXData(ctx, pair)
		if err != nil {
			return nil, err
		}
		ds.FX[pair] = fx
		ds.FXPairs = append(ds.FXPairs, pair)
	}
	sort.Strings(ds.FXPairs)

	if len(ds.FXPairs) > 0 {
		active, err := s.fxSyncActive(ctx)
		if err != nil {
			return nil, err
		}
		ds.FXSyncActive = active
	}
	return ds, nil
}

func (s *ResearchService) loadResearchAssetData(
	ctx context.Context,
	collection repository.ResearchCollection,
	item repository.ResearchCollectionItem,
) (researchAssetData, error) {
	data := researchAssetData{Item: item}
	asset, err := s.assets.GetByKey(ctx, item.AssetKey)
	if err != nil {
		return data, wrapRepo("load market asset "+item.AssetKey, err)
	}
	data.Asset = asset
	data.IsCash = isSystemCashAsset(asset)
	data.FXPairs = ResearchFXPairsFor(asset.Currency, collection.BaseCurrency)

	if data.IsCash {
		return data, nil
	}

	state, ok, err := s.assets.GetHistoryState(ctx, item.AssetKey, item.AdjustPolicy, item.PointType)
	if err != nil {
		return data, wrapRepo("load history state", err)
	}
	data.HasState = ok
	data.State = state
	if ok && state.LastTaskID != "" {
		task, err := s.tasks.GetByID(ctx, state.LastTaskID)
		if err == nil {
			data.Task = &task
		} else if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
			return data, wrapRepo("load history task", err)
		}
	}

	points, err := s.assets.ListPoints(ctx, item.AssetKey, item.AdjustPolicy, item.PointType)
	if err != nil {
		return data, wrapRepo("list asset points", err)
	}
	data.Points = points
	sources := map[string]bool{}
	for _, p := range points {
		if p.Value <= 0 || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			data.NonPositiveCount++
		}
		if p.SourceName != "" {
			sources[p.SourceName] = true
		}
	}
	for name := range sources {
		data.SourceNames = append(data.SourceNames, name)
	}
	sort.Strings(data.SourceNames)
	return data, nil
}

// loadBenchmarkData loads the benchmark asset with its best history
// dimension (explicit dimensions are an item concept; the benchmark uses the
// asset's existing history, falling back to type defaults).
func (s *ResearchService) loadBenchmarkData(
	ctx context.Context, collection repository.ResearchCollection,
) (researchAssetData, error) {
	assetKey := collection.BenchmarkAssetKey
	asset, err := s.assets.GetByKey(ctx, assetKey)
	if err != nil {
		return researchAssetData{}, wrapRepo("load benchmark asset "+assetKey, err)
	}
	adjustPolicy := DefaultAdjustPolicy(asset.InstrumentType)
	pointType := DefaultPointType(asset.InstrumentType, asset.InstrumentKind)
	states, err := s.assets.ListHistoryStatesByAsset(ctx, assetKey)
	if err != nil {
		return researchAssetData{}, wrapRepo("list benchmark history states", err)
	}
	bestPoints := -1
	for _, st := range states {
		if isExchangeTradedResearchAsset(asset) {
			if st.AdjustPolicy != adjustPolicy || st.PointType != pointType {
				continue
			}
		}
		if st.PointCount > bestPoints {
			bestPoints = st.PointCount
			adjustPolicy, pointType = st.AdjustPolicy, st.PointType
		}
	}
	item := repository.ResearchCollectionItem{
		AssetKey:     assetKey,
		AdjustPolicy: adjustPolicy,
		PointType:    pointType,
		Enabled:      true,
	}
	return s.loadResearchAssetData(ctx, collection, item)
}

func (s *ResearchService) loadFXData(ctx context.Context, pair string) (*researchFXData, error) {
	fx := &researchFXData{Pair: pair}
	inst, err := s.instruments.FindByKey(ctx, "SYSTEM", "fx_rate", pair, "none")
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return fx, nil
		}
		return nil, wrapRepo("find fx instrument "+pair, err)
	}
	points, err := s.marketData.ListByInstrument(ctx, inst.ID)
	if err != nil {
		return nil, wrapRepo("list fx points "+pair, err)
	}
	fx.Found = len(points) > 0
	fx.Points = points
	for _, p := range points {
		if p.Value <= 0 || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			fx.NonPositiveCount++
		}
	}
	if len(points) > 0 {
		fx.SourceName = points[0].SourceName
	}
	return fx, nil
}

// fxSyncActive reports whether an fx_rate_sync task is currently active.
func (s *ResearchService) fxSyncActive(ctx context.Context) (bool, error) {
	st, ok, err := s.assets.GetSyncState(ctx, ScopeFXRates)
	if err != nil {
		return false, wrapRepo("load fx sync state", err)
	}
	if !ok || st.LastTaskID == "" {
		return false, nil
	}
	task, err := s.tasks.GetByID(ctx, st.LastTaskID)
	if err != nil {
		if errors.Is(err, repository.ErrWorkerTaskNotFound) {
			return false, nil
		}
		return false, wrapRepo("load fx sync task", err)
	}
	return repository.IsActiveWorkerTaskStatus(task.Status), nil
}

// --- evaluation ---

type researchBoundedRange struct {
	lo, hi   int
	assetKey string
}

// evaluateResearchReadiness derives the full readiness verdict from one
// dataset. Pure: no I/O, injectable clock.
func evaluateResearchReadiness(ds *researchDataset, now time.Time) ResearchReadiness {
	out := ResearchReadiness{
		BlockingReasons: []ResearchReadinessIssue{},
		Warnings:        []ResearchReadinessIssue{},
		Assets:          []ResearchReadinessAssetView{},
		DataDependencies: ResearchDataDependencies{
			AssetCount: len(ds.Enabled),
			FXPairs:    append([]string{}, ds.FXPairs...),
		},
	}
	block := func(issue ResearchReadinessIssue) {
		out.BlockingReasons = append(out.BlockingReasons, issue)
	}
	warn := func(issue ResearchReadinessIssue) {
		out.Warnings = append(out.Warnings, issue)
	}

	evaluateResearchWeights(ds, &out, block)
	evaluateCanonicalFundDuplicates(ds, block)

	// 2. Per-asset history checks + usable window assembly.
	fxBounds := func(pairs []string) (int, int, bool) {
		return researchFXBounds(ds, pairs)
	}

	ranges, prepared, staleCount, missingCount := evaluateResearchAssets(
		ds, now, &out, fxBounds, block, warn,
	)
	out.DataDependencies.StaleAssetCount = staleCount
	out.DataDependencies.MissingHistoryCount = missingCount

	evaluateResearchFX(ds, block)

	evaluateResearchBenchmark(ds, block)

	commonLo, commonHi, haveWindow := deriveResearchCommonWindow(ds, ranges, &out, block)

	if haveWindow {
		evaluateResearchSelectedWindow(
			ds, &out, prepared, commonLo, commonHi, fxBounds, block, warn,
		)
	}
	if out.WindowStart != "" && out.WindowEnd != "" && len(out.BlockingReasons) == 0 {
		evaluateResearchTailRisk(ds, &out, block)
	}

	// 6. Concentration warnings.
	addConcentrationWarnings(ds, warn)

	out.Ready = len(out.BlockingReasons) == 0
	return out
}

func evaluateCanonicalFundDuplicates(
	ds *researchDataset,
	block func(ResearchReadinessIssue),
) {
	seen := map[string]string{}
	for _, data := range ds.Enabled {
		identity := canonicalFundIdentity(data.Asset)
		if identity == "" {
			continue
		}
		if existingAssetKey, exists := seen[identity]; exists {
			canonical := data.Asset.CanonicalSymbol
			if canonical == "" {
				canonical = data.Asset.Symbol
			}
			block(ResearchReadinessIssue{
				AssetKey: data.Asset.AssetKey,
				Reason:   ResearchReasonDuplicateFund,
				Message: fmt.Sprintf(
					"该资产与 %s 对应同一主基金 %s，请仅保留一个交易代码",
					existingAssetKey, canonical,
				),
			})
			continue
		}
		seen[identity] = data.Asset.AssetKey
	}
}

func evaluateResearchSelectedWindow(
	ds *researchDataset,
	out *ResearchReadiness,
	prepared map[string]preparedSeries,
	commonLo, commonHi int,
	fxBounds func([]string) (int, int, bool),
	block, warn func(ResearchReadinessIssue),
) {
	winLo, winHi := clampResearchReadinessWindow(ds.Collection, commonLo, commonHi)
	switch {
	case winHi <= winLo:
		block(ResearchReadinessIssue{
			Reason: ResearchReasonWindowEmpty, Message: "指定的回测区间与共同可用区间没有重叠",
		})
	case winHi-winLo < researchReadinessMinWindowDays:
		block(ResearchReadinessIssue{
			Reason: ResearchReasonWindowTooShort,
			Message: fmt.Sprintf("回测区间 %s ~ %s 短于最小长度 1 年",
				researchDayToDate(winLo), researchDayToDate(winHi)),
		})
	default:
		evaluateValidResearchWindow(
			ds, out, prepared, commonHi, winLo, winHi, fxBounds, block, warn,
		)
	}
}

func clampResearchReadinessWindow(
	collection repository.ResearchCollection, commonLo, commonHi int,
) (int, int) {
	if collection.StartPolicy != ResearchStartPolicyCustom {
		return commonLo, commonHi
	}
	if day, err := parseResearchDate(collection.WindowStart); err == nil {
		commonLo = maxInt(commonLo, day)
	}
	if day, err := parseResearchDate(collection.WindowEnd); err == nil {
		commonHi = minInt(commonHi, day)
	}
	return commonLo, commonHi
}

func allCash(ds *researchDataset) bool {
	for _, a := range ds.Enabled {
		if !a.IsCash {
			return false
		}
	}
	return len(ds.Enabled) > 0
}

func evaluateResearchWeights(
	ds *researchDataset,
	out *ResearchReadiness,
	block func(ResearchReadinessIssue),
) {
	for _, asset := range ds.Enabled {
		out.WeightSum += asset.Item.Weight
		if asset.Item.Weight < 0 {
			block(ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey, Reason: ResearchReasonNegativeWeight, Message: "权重为负数",
			})
		}
		if asset.Item.Weight > 1+ResearchWeightTolerance {
			block(ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey, Reason: ResearchReasonWeightExceeds100,
				Message: "单资产权重大于 100%",
			})
		}
	}
	if len(ds.Enabled) == 0 {
		block(ResearchReadinessIssue{Reason: ResearchReasonNoEnabledAssets, Message: "集合没有启用的资产"})
		return
	}
	if math.Abs(out.WeightSum-1) > ResearchWeightTolerance {
		block(ResearchReadinessIssue{
			Reason:  ResearchReasonWeightSumInvalid,
			Message: fmt.Sprintf("权重合计不是 100%%（当前 %.4f%%）", out.WeightSum*100),
		})
	}
}

func researchFXBounds(ds *researchDataset, pairs []string) (int, int, bool) {
	lo, hi, set := 0, 0, false
	for _, pair := range pairs {
		fx, ok := ds.FX[pair]
		if !ok || !fx.Found || len(fx.Points) == 0 {
			return 0, 0, false
		}
		first, firstErr := parseResearchDate(fx.Points[0].TradeDate)
		last, lastErr := parseResearchDate(fx.Points[len(fx.Points)-1].TradeDate)
		if firstErr != nil || lastErr != nil {
			return 0, 0, false
		}
		if !set {
			lo, hi, set = first, last, true
			continue
		}
		lo, hi = maxInt(lo, first), minInt(hi, last)
	}
	return lo, hi, set
}

type researchAssetReadinessResult struct {
	view      ResearchReadinessAssetView
	series    preparedSeries
	itemRange *researchBoundedRange
	stale     bool
	missing   bool
}

func evaluateResearchAssets(
	ds *researchDataset,
	now time.Time,
	out *ResearchReadiness,
	fxBounds func([]string) (int, int, bool),
	block, warn func(ResearchReadinessIssue),
) ([]researchBoundedRange, map[string]preparedSeries, int, int) {
	ranges := make([]researchBoundedRange, 0, len(ds.Enabled))
	prepared := make(map[string]preparedSeries, len(ds.Enabled))
	staleCount, missingCount := 0, 0
	for _, asset := range ds.Enabled {
		result := evaluateResearchAsset(ds, asset, now, fxBounds, block, warn)
		out.Assets = append(out.Assets, result.view)
		if result.itemRange != nil {
			ranges = append(ranges, *result.itemRange)
		}
		if !result.series.empty() {
			prepared[asset.Item.AssetKey] = result.series
		}
		if result.stale {
			staleCount++
		}
		if result.missing {
			missingCount++
		}
	}
	return ranges, prepared, staleCount, missingCount
}

func evaluateResearchAsset(
	ds *researchDataset,
	asset researchAssetData,
	now time.Time,
	fxBounds func([]string) (int, int, bool),
	block, warn func(ResearchReadinessIssue),
) researchAssetReadinessResult {
	result := researchAssetReadinessResult{view: newResearchReadinessAssetView(asset)}
	evaluateResearchAssetFX(ds, asset, block)
	if asset.IsCash {
		result.view.HasHistory = true
		if lo, hi, ok := fxBounds(asset.FXPairs); ok {
			result.view.HistoryStart, result.view.HistoryEnd = researchDayToDate(lo), researchDayToDate(hi)
			result.itemRange = &researchBoundedRange{lo: lo, hi: hi, assetKey: asset.Item.AssetKey}
		}
		return result
	}
	evaluateResearchAssetTask(asset, &result.view, block)
	if len(asset.Points) == 0 {
		result.missing = true
		evaluateMissingResearchAsset(asset, block)
		return result
	}
	populateResearchAssetHistory(asset, &result.view)
	evaluateResearchAssetDataQuality(asset, block, warn)
	result.stale = evaluateResearchAssetStaleness(asset, now, &result.view, warn)
	series, err := prepareSeries(assetPointsToSeries(asset.Points))
	if err != nil || series.empty() {
		return result
	}
	result.series = series
	lo, hi := series.firstDay(), series.lastDay()
	if fxLo, fxHi, ok := fxBounds(asset.FXPairs); ok {
		lo, hi = maxInt(lo, fxLo), minInt(hi, fxHi)
	}
	if hi > lo {
		result.itemRange = &researchBoundedRange{lo: lo, hi: hi, assetKey: asset.Item.AssetKey}
	}
	return result
}

func newResearchReadinessAssetView(asset researchAssetData) ResearchReadinessAssetView {
	return ResearchReadinessAssetView{
		ItemID: asset.Item.ID, AssetKey: asset.Item.AssetKey, Name: asset.Asset.Name,
		Currency: asset.Asset.Currency, IsCash: asset.IsCash, Enabled: asset.Item.Enabled,
		Weight: asset.Item.Weight, AdjustPolicy: asset.Item.AdjustPolicy, PointType: asset.Item.PointType,
		ListingStatus: asset.Asset.ListingStatus, FXPairs: asset.FXPairs,
	}
}

func evaluateResearchAssetFX(
	ds *researchDataset, asset researchAssetData, block func(ResearchReadinessIssue),
) {
	for _, pair := range asset.FXPairs {
		fx, ok := ds.FX[pair]
		if !ok || !fx.Found {
			block(ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey, Pair: pair, Reason: ResearchReasonFXMissing,
				Message: fmt.Sprintf("%s 资产需要 %s 历史汇率", asset.Asset.Currency, pair),
			})
		}
	}
}

func evaluateResearchAssetTask(
	asset researchAssetData,
	view *ResearchReadinessAssetView,
	block func(ResearchReadinessIssue),
) {
	if asset.Task == nil {
		return
	}
	view.SyncStatus = asset.Task.Status
	if asset.Task.Status == repository.WorkerTaskStatusFailed {
		view.SyncError = asset.Task.ErrorMessage
		if view.SyncError == "" {
			view.SyncError = asset.Task.ErrorCode
		}
	}
	if repository.IsActiveWorkerTaskStatus(asset.Task.Status) {
		block(ResearchReadinessIssue{
			AssetKey: asset.Item.AssetKey, Reason: ResearchReasonHistorySyncing,
			Message: "历史同步正在运行",
		})
	}
}

func evaluateMissingResearchAsset(asset researchAssetData, block func(ResearchReadinessIssue)) {
	if asset.Task != nil && asset.Task.Status == repository.WorkerTaskStatusFailed {
		block(ResearchReadinessIssue{
			AssetKey: asset.Item.AssetKey, Reason: ResearchReasonHistorySyncFailed,
			Message: "历史同步失败且没有可用旧数据",
		})
		return
	}
	if asset.Task == nil || !repository.IsActiveWorkerTaskStatus(asset.Task.Status) {
		block(ResearchReadinessIssue{
			AssetKey: asset.Item.AssetKey, Reason: ResearchReasonHistoryMissing, Message: "缺少历史点位",
		})
	}
}

func populateResearchAssetHistory(asset researchAssetData, view *ResearchReadinessAssetView) {
	view.HasHistory = true
	view.PointCount = len(asset.Points)
	view.HistoryStart = asset.Points[0].TradeDate
	view.HistoryEnd = asset.Points[len(asset.Points)-1].TradeDate
	view.DataAsOf = asset.State.DataAsOf
	if view.DataAsOf == "" {
		view.DataAsOf = view.HistoryEnd
	}
}

func evaluateResearchAssetDataQuality(
	asset researchAssetData, block, warn func(ResearchReadinessIssue),
) {
	if asset.Task != nil && asset.Task.Status == repository.WorkerTaskStatusFailed {
		warn(ResearchReadinessIssue{
			AssetKey: asset.Item.AssetKey, Reason: ResearchWarnSyncFailedStale,
			Message: "最近一次历史同步失败，当前使用旧数据",
		})
	}
	if asset.NonPositiveCount > 0 {
		block(ResearchReadinessIssue{
			AssetKey: asset.Item.AssetKey, Reason: ResearchReasonNonPositivePoints,
			Message: fmt.Sprintf("存在 %d 个非正数点位", asset.NonPositiveCount),
		})
	}
	if len(asset.SourceNames) > 1 {
		block(ResearchReadinessIssue{
			AssetKey: asset.Item.AssetKey, Reason: ResearchReasonMixedSources,
			Message: "历史点位来自多个数据源，无法确认口径：" + strings.Join(asset.SourceNames, ", "),
		})
	}
	if isExchangeTradedResearchAsset(asset.Asset) {
		switch {
		case asset.Item.AdjustPolicy == "none" || asset.Item.PointType == "close":
			block(ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey, Reason: ResearchReasonUnadjustedSeries,
				Message: "未复权收盘价不能用于收益回测，请同步后复权历史数据",
			})
		case asset.Item.AdjustPolicy != "hfq" || asset.Item.PointType != "adjusted_close":
			block(ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey, Reason: ResearchReasonUnsupportedSeries,
				Message: "场内资产收益回测只支持后复权收盘价",
			})
		}
	}
}

func isExchangeTradedResearchAsset(asset repository.MarketAsset) bool {
	switch asset.InstrumentType {
	case "cn_exchange_stock", "cn_exchange_fund", "hk_stock", "hk_etf", "us_stock", "us_etf":
		return true
	default:
		return false
	}
}

func evaluateResearchAssetStaleness(
	asset researchAssetData,
	now time.Time,
	view *ResearchReadinessAssetView,
	warn func(ResearchReadinessIssue),
) bool {
	if asset.Asset.ListingStatus != "" && asset.Asset.ListingStatus != "active" {
		warn(ResearchReadinessIssue{
			AssetKey: asset.Item.AssetKey, Reason: ResearchWarnAssetInactive,
			Message: "资产已停更/退市，数据不再更新",
		})
		return false
	}
	asOfDay, err := parseResearchDate(view.DataAsOf)
	if err != nil {
		return false
	}
	tolerance := ResearchStaleToleranceDays(asset.Asset.InstrumentType)
	if int(now.Unix()/86400)-asOfDay <= tolerance {
		return false
	}
	view.Stale = true
	warn(ResearchReadinessIssue{
		AssetKey: asset.Item.AssetKey, Reason: ResearchWarnStaleData,
		Message: fmt.Sprintf("数据截至 %s，超过 %d 天未更新", view.DataAsOf, tolerance),
	})
	return true
}

func evaluateResearchFX(ds *researchDataset, block func(ResearchReadinessIssue)) {
	if ds.FXSyncActive {
		for _, pair := range ds.FXPairs {
			block(ResearchReadinessIssue{
				Pair: pair, Reason: ResearchReasonFXSyncing, Message: "汇率同步正在运行",
			})
		}
	}
	for _, pair := range ds.FXPairs {
		fx, ok := ds.FX[pair]
		if ok && fx.Found && fx.NonPositiveCount > 0 {
			block(ResearchReadinessIssue{
				Pair: pair, Reason: ResearchReasonNonPositivePoints,
				Message: fmt.Sprintf("FX %s 存在 %d 个非正数点位", pair, fx.NonPositiveCount),
			})
		}
	}
}

func evaluateResearchBenchmark(ds *researchDataset, block func(ResearchReadinessIssue)) {
	benchmark := ds.Benchmark
	if benchmark == nil {
		return
	}
	if !benchmark.IsCash && len(benchmark.Points) == 0 {
		block(ResearchReadinessIssue{
			AssetKey: benchmark.Item.AssetKey, Reason: ResearchReasonBenchmarkNoHistory,
			Message: "基准资产缺少历史点位",
		})
	}
	for _, pair := range benchmark.FXPairs {
		fx, ok := ds.FX[pair]
		if !ok || !fx.Found {
			block(ResearchReadinessIssue{
				AssetKey: benchmark.Item.AssetKey, Pair: pair, Reason: ResearchReasonFXMissing,
				Message: fmt.Sprintf("基准资产需要 %s 历史汇率", pair),
			})
		}
	}
	if benchmark.NonPositiveCount > 0 {
		block(ResearchReadinessIssue{
			AssetKey: benchmark.Item.AssetKey, Reason: ResearchReasonNonPositivePoints,
			Message: fmt.Sprintf("基准资产存在 %d 个非正数点位", benchmark.NonPositiveCount),
		})
	}
	if len(benchmark.SourceNames) > 1 {
		block(ResearchReadinessIssue{
			AssetKey: benchmark.Item.AssetKey, Reason: ResearchReasonMixedSources,
			Message: "基准资产历史点位来自多个数据源：" + strings.Join(benchmark.SourceNames, ", "),
		})
	}
}

func deriveResearchCommonWindow(
	ds *researchDataset,
	ranges []researchBoundedRange,
	out *ResearchReadiness,
	block func(ResearchReadinessIssue),
) (int, int, bool) {
	if len(ds.Enabled) == 0 {
		return 0, 0, false
	}
	if len(ranges) == 0 {
		return deriveAllCashResearchWindow(ds, out, block)
	}
	commonLo, commonHi := ranges[0].lo, ranges[0].hi
	for _, itemRange := range ranges[1:] {
		commonLo = maxInt(commonLo, itemRange.lo)
		commonHi = minInt(commonHi, itemRange.hi)
	}
	if commonHi <= commonLo {
		block(ResearchReadinessIssue{
			Reason: ResearchReasonWindowEmpty, Message: "资产历史没有共同重叠区间",
		})
		return 0, 0, false
	}
	out.CommonStart, out.CommonEnd = researchDayToDate(commonLo), researchDayToDate(commonHi)
	markResearchWindowLimiters(out.Assets, ranges, commonLo, commonHi)
	return commonLo, commonHi, true
}

func deriveAllCashResearchWindow(
	ds *researchDataset,
	out *ResearchReadiness,
	block func(ResearchReadinessIssue),
) (int, int, bool) {
	if out.DataDependencies.MissingHistoryCount != 0 || !allCash(ds) {
		return 0, 0, false
	}
	lo, loErr := parseResearchDate(ds.Collection.WindowStart)
	hi, hiErr := parseResearchDate(ds.Collection.WindowEnd)
	if loErr == nil && hiErr == nil && hi > lo {
		out.CommonStart, out.CommonEnd = ds.Collection.WindowStart, ds.Collection.WindowEnd
		return lo, hi, true
	}
	block(ResearchReadinessIssue{
		Reason: ResearchReasonWindowEmpty, Message: "纯现金组合需要明确的回测开始和结束日期",
	})
	return 0, 0, false
}

func evaluateValidResearchWindow(
	ds *researchDataset,
	out *ResearchReadiness,
	prepared map[string]preparedSeries,
	commonHi, winLo, winHi int,
	fxBounds func([]string) (int, int, bool),
	block, warn func(ResearchReadinessIssue),
) {
	out.WindowStart, out.WindowEnd = researchDayToDate(winLo), researchDayToDate(winHi)
	evaluateBenchmarkWindow(ds, winLo, winHi, fxBounds, block)
	if winHi-winLo < researchReadinessShortWindowDays {
		warn(ResearchReadinessIssue{
			Reason:  ResearchWarnShortWindow,
			Message: "共同回测区间短于 3 年，结果不建议作为 FIRE 资产决策依据",
		})
	}
	if len(prepared) > 0 && countEffectiveResearchDays(ds, prepared, winLo, winHi) < researchReadinessMinEffectiveObs {
		block(ResearchReadinessIssue{
			Reason: ResearchReasonTooFewEffectiveDays, Message: "有效估值日不足，无法计算指标",
		})
	}
	evaluateResearchFillWarnings(ds, prepared, winLo, winHi, warn)
	evaluateResearchLagWarnings(ds, prepared, commonHi, warn)
	evaluateResearchFXGaps(ds, winLo, winHi, block)
	addCorrelationWarnings(ds, prepared, winLo, winHi, warn)
}

func evaluateBenchmarkWindow(
	ds *researchDataset,
	winLo, winHi int,
	fxBounds func([]string) (int, int, bool),
	block func(ResearchReadinessIssue),
) {
	benchmark := ds.Benchmark
	if benchmark == nil {
		return
	}
	benchLo, benchHi, bounded := benchmarkHistoryBounds(benchmark, winLo, winHi, block)
	if fxLo, fxHi, fxBounded := fxBounds(benchmark.FXPairs); fxBounded {
		if !bounded {
			benchLo, benchHi = fxLo, fxHi
		} else {
			benchLo, benchHi = maxInt(benchLo, fxLo), minInt(benchHi, fxHi)
		}
		bounded = true
	}
	if bounded && (benchLo > winLo || benchHi < winHi) {
		block(ResearchReadinessIssue{
			AssetKey: benchmark.Item.AssetKey, Reason: ResearchReasonBenchmarkWindow,
			Message: fmt.Sprintf("基准可用区间 %s ~ %s 未覆盖回测区间 %s ~ %s",
				researchDayToDate(benchLo), researchDayToDate(benchHi),
				researchDayToDate(winLo), researchDayToDate(winHi)),
		})
	}
}

func benchmarkHistoryBounds(
	benchmark *researchAssetData,
	winLo, winHi int,
	block func(ResearchReadinessIssue),
) (int, int, bool) {
	if benchmark.IsCash {
		return 0, 0, false
	}
	series, err := prepareSeries(assetPointsToSeries(benchmark.Points))
	if err != nil || series.empty() {
		return 0, 0, false
	}
	fillLo, fillHi := maxInt(winLo, series.firstDay()), minInt(winHi, series.lastDay())
	if fillHi >= fillLo {
		tolerance := ResearchFillGapToleranceDays(benchmark.Asset.InstrumentType)
		if _, maxRun := series.fillStats(fillLo, fillHi); maxRun > tolerance {
			block(ResearchReadinessIssue{
				AssetKey: benchmark.Item.AssetKey, Reason: ResearchReasonBenchmarkGap,
				Message: fmt.Sprintf("基准资产连续缺口 %d 天超过容忍值 %d 天", maxRun, tolerance),
			})
		}
	}
	return series.firstDay(), series.lastDay(), true
}

func countEffectiveResearchDays(
	ds *researchDataset, prepared map[string]preparedSeries, winLo, winHi int,
) int {
	effective := map[int]bool{}
	for _, series := range prepared {
		for _, day := range series.days {
			if day >= winLo && day <= winHi {
				effective[day] = true
			}
		}
	}
	for _, pair := range ds.FXPairs {
		fx := ds.FX[pair]
		if fx == nil || !fx.Found {
			continue
		}
		for _, point := range fx.Points {
			day, err := parseResearchDate(point.TradeDate)
			if err == nil && day >= winLo && day <= winHi {
				effective[day] = true
			}
		}
	}
	return len(effective)
}

func evaluateResearchTailRisk(
	ds *researchDataset,
	out *ResearchReadiness,
	block func(ResearchReadinessIssue),
) {
	// Zero/zero is retained for focused in-memory legacy fixtures. Persisted
	// collections are migrated with explicit defaults and production creates
	// always canonicalize both fields.
	if ds.Collection.TailRiskConfidence == 0 && ds.Collection.TailRiskHorizonDays == 0 {
		return
	}
	spec, err := CanonicalTailRiskSpec(TailRiskSpec{
		Confidence:  ds.Collection.TailRiskConfidence,
		HorizonDays: ds.Collection.TailRiskHorizonDays,
	})
	if err != nil {
		block(ResearchReadinessIssue{Reason: tailRiskErrorCode(err), Message: err.Error()})
		return
	}
	lo, loErr := parseResearchDate(out.WindowStart)
	hi, hiErr := parseResearchDate(out.WindowEnd)
	if loErr != nil || hiErr != nil {
		return
	}
	effective := relevantEffectiveObservationDays(ds, lo, hi)
	count := len(effective)
	scenarios := TailRiskScenarioCount(count, spec.HorizonDays)
	minimum := MinimumTailRiskScenarios(spec.Confidence)
	out.TailRisk = &ResearchTailRiskReadiness{
		Confidence: spec.Confidence, HorizonDays: spec.HorizonDays,
		EffectiveReturnCount: count, ScenarioCount: scenarios, MinimumScenarioCount: minimum,
	}
	if scenarios < minimum {
		block(ResearchReadinessIssue{
			Reason: ResearchReasonCVARSample,
			Message: fmt.Sprintf("CVaR 场景数 %d 少于最低要求 %d（%.0f%% / %d 日）",
				scenarios, minimum, spec.Confidence*100, spec.HorizonDays),
		})
	}
}

func relevantEffectiveObservationDays(ds *researchDataset, lo, hi int) map[int]bool {
	effective := map[int]bool{}
	positiveFound, onlyBaseCash := false, true
	for _, asset := range ds.Enabled {
		if asset.Item.Weight <= ResearchWeightTolerance {
			continue
		}
		positiveFound = true
		if !asset.IsCash || len(asset.FXPairs) > 0 {
			onlyBaseCash = false
		}
		appendResearchAssetObservationDays(effective, ds, asset, lo, hi)
	}
	if positiveFound && onlyBaseCash {
		for day := lo + 1; day <= hi; day++ {
			if isResearchWeekday(day) {
				effective[day] = true
			}
		}
	}
	return effective
}

func appendResearchAssetObservationDays(
	effective map[int]bool,
	ds *researchDataset,
	asset researchAssetData,
	lo, hi int,
) {
	if !asset.IsCash {
		appendMarketAssetObservationDays(effective, asset.Points, lo, hi)
	}
	for _, pair := range asset.FXPairs {
		fx := ds.FX[pair]
		if fx == nil || !fx.Found {
			continue
		}
		for _, point := range fx.Points {
			appendResearchObservationDay(effective, point.TradeDate, lo, hi)
		}
	}
}

func appendMarketAssetObservationDays(
	effective map[int]bool, points []repository.MarketAssetPoint, lo, hi int,
) {
	for _, point := range points {
		appendResearchObservationDay(effective, point.TradeDate, lo, hi)
	}
}

func appendResearchObservationDay(effective map[int]bool, tradeDate string, lo, hi int) {
	day, err := parseResearchDate(tradeDate)
	if err == nil && day > lo && day <= hi {
		effective[day] = true
	}
}

func evaluateResearchFillWarnings(
	ds *researchDataset,
	prepared map[string]preparedSeries,
	winLo, winHi int,
	warn func(ResearchReadinessIssue),
) {
	for _, asset := range ds.Enabled {
		series, ok := prepared[asset.Item.AssetKey]
		if !ok {
			continue
		}
		tolerance := ResearchFillGapToleranceDays(asset.Asset.InstrumentType)
		fillLo, fillHi := maxInt(winLo, series.firstDay()), minInt(winHi, series.lastDay())
		if fillHi < fillLo {
			continue
		}
		if _, maxRun := series.fillStats(fillLo, fillHi); maxRun > tolerance {
			warn(ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey, Reason: ResearchWarnExcessiveFill,
				Message: fmt.Sprintf("历史存在超过 %d 天的连续缺口（最长 %d 天），将按前值填充",
					tolerance, maxRun),
			})
		}
	}
}

func evaluateResearchLagWarnings(
	ds *researchDataset,
	prepared map[string]preparedSeries,
	commonHi int,
	warn func(ResearchReadinessIssue),
) {
	for _, asset := range ds.Enabled {
		series, ok := prepared[asset.Item.AssetKey]
		if !ok || series.lastDay() != commonHi {
			continue
		}
		if anyOtherEndsLater(prepared, asset.Item.AssetKey, series.lastDay(), researchDataLaggingSlackDays) {
			warn(ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey, Reason: ResearchWarnDataLagging,
				Message: fmt.Sprintf("该资产数据截至 %s，明显滞后并使共同终点提前",
					researchDayToDate(series.lastDay())),
			})
		}
	}
}

func evaluateResearchFXGaps(
	ds *researchDataset, winLo, winHi int, block func(ResearchReadinessIssue),
) {
	for _, pair := range ds.FXPairs {
		fx := ds.FX[pair]
		if fx == nil || !fx.Found {
			continue
		}
		series, err := prepareSeries(fxPointsToSeries(fx.Points))
		if err != nil || series.empty() {
			continue
		}
		fillLo, fillHi := maxInt(winLo, series.firstDay()), minInt(winHi, series.lastDay())
		if fillHi < fillLo {
			continue
		}
		if _, maxRun := series.fillStats(fillLo, fillHi); maxRun > researchReadinessFXFillGapDays {
			block(ResearchReadinessIssue{
				Pair: pair, Reason: ResearchReasonFXGapExceeded,
				Message: fmt.Sprintf("FX %s 历史缺口超过容忍间隔（最长 %d 天）", pair, maxRun),
			})
		}
	}
}

func markResearchWindowLimiters(
	assets []ResearchReadinessAssetView, ranges []researchBoundedRange, commonLo, commonHi int,
) {
	byAsset := make(map[string]researchBoundedRange, len(ranges))
	for _, itemRange := range ranges {
		byAsset[itemRange.assetKey] = itemRange
	}
	for i := range assets {
		itemRange, ok := byAsset[assets[i].AssetKey]
		if !ok {
			continue
		}
		assets[i].LimitsCommonStart = itemRange.lo == commonLo
		assets[i].LimitsCommonEnd = itemRange.hi == commonHi
	}
}

func anyOtherEndsLater(prepared map[string]preparedSeries, selfKey string, selfEnd, slack int) bool {
	for key, series := range prepared {
		if key == selfKey {
			continue
		}
		if series.lastDay()-selfEnd > slack {
			return true
		}
	}
	return false
}

func addConcentrationWarnings(ds *researchDataset, warn func(ResearchReadinessIssue)) {
	if len(ds.Enabled) == 0 {
		return
	}
	total := 0.0
	marketWeight := map[string]float64{}
	currencyWeight := map[string]float64{}
	for _, a := range ds.Enabled {
		w := a.Item.Weight
		total += w
		marketWeight[a.Asset.Market] += w
		currencyWeight[a.Asset.Currency] += w
		if w > researchWeightConcentrationLimit {
			warn(ResearchReadinessIssue{
				AssetKey: a.Item.AssetKey, Reason: ResearchWarnWeightConcentration,
				Message: fmt.Sprintf("单资产权重 %.1f%% 过高", w*100),
			})
		}
	}
	if total <= 0 || len(ds.Enabled) < 2 {
		return
	}
	for market, w := range marketWeight {
		if w/total > researchGroupConcentrationLimit {
			warn(ResearchReadinessIssue{
				Reason:  ResearchWarnMarketConcentration,
				Message: fmt.Sprintf("组合高度集中于 %s 市场（%.1f%%）", market, w/total*100),
			})
		}
	}
	for currency, w := range currencyWeight {
		if w/total > researchGroupConcentrationLimit {
			warn(ResearchReadinessIssue{
				Reason:  ResearchWarnCurrencyConcentration,
				Message: fmt.Sprintf("组合高度集中于 %s 币种（%.1f%%）", currency, w/total*100),
			})
		}
	}
}

// addCorrelationWarnings warns on highly correlated pairs using returns over
// shared observation days inside the window.
func addCorrelationWarnings(
	ds *researchDataset,
	prepared map[string]preparedSeries,
	winLo, winHi int,
	warn func(ResearchReadinessIssue),
) {
	type entry struct {
		key    string
		series preparedSeries
	}
	var entries []entry
	for _, a := range ds.Enabled {
		if a.IsCash {
			continue
		}
		if s, ok := prepared[a.Item.AssetKey]; ok {
			entries = append(entries, entry{key: a.Item.AssetKey, series: s})
		}
	}
	if len(entries) < 2 || len(entries) > researchCorrelationMaxAssets {
		return
	}
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			corr, samples, ok := sharedObservationCorrelation(
				entries[i].series, entries[j].series, winLo, winHi,
			)
			if !ok || samples < researchReadinessCorrelationSample {
				continue
			}
			if corr > researchHighCorrelationLimit {
				warn(ResearchReadinessIssue{
					AssetKey: entries[i].key, Reason: ResearchWarnHighCorrelation,
					Message: fmt.Sprintf("%s 与 %s 的相关系数高达 %.2f",
						entries[i].key, entries[j].key, corr),
				})
			}
		}
	}
}

// sharedObservationCorrelation computes the return correlation over days
// where both series have real observations.
func sharedObservationCorrelation(a, b preparedSeries, lo, hi int) (float64, int, bool) {
	var days []int
	for _, d := range a.days {
		if d < lo || d > hi {
			continue
		}
		if b.hasObservation(d) {
			days = append(days, d)
		}
	}
	if len(days) < 3 {
		return 0, 0, false
	}
	var ra, rb []float64
	for i := 1; i < len(days); i++ {
		va0, _ := a.valueAt(days[i-1])
		va1, _ := a.valueAt(days[i])
		vb0, _ := b.valueAt(days[i-1])
		vb1, _ := b.valueAt(days[i])
		if va0 <= 0 || vb0 <= 0 {
			continue
		}
		ra = append(ra, va1/va0-1)
		rb = append(rb, vb1/vb0-1)
	}
	corr, ok := pearsonCorrelation(ra, rb)
	return corr, len(ra), ok
}

// --- conversion helpers ---

func assetPointsToSeries(points []repository.MarketAssetPoint) []ResearchSeriesPoint {
	out := make([]ResearchSeriesPoint, len(points))
	for i, p := range points {
		out[i] = ResearchSeriesPoint{Date: p.TradeDate, Value: p.Value}
	}
	return out
}

func fxPointsToSeries(points []repository.MarketDataPoint) []ResearchSeriesPoint {
	out := make([]ResearchSeriesPoint, len(points))
	for i, p := range points {
		out[i] = ResearchSeriesPoint{Date: p.TradeDate, Value: p.Value}
	}
	return out
}
