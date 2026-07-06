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
	ResearchReasonTooFewEffectiveDays = "too_few_effective_days"
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
	adjustPolicy, pointType := "none", DefaultPointType(asset.InstrumentType, asset.InstrumentKind)
	states, err := s.assets.ListHistoryStatesByAsset(ctx, assetKey)
	if err != nil {
		return researchAssetData{}, wrapRepo("list benchmark history states", err)
	}
	bestPoints := -1
	for _, st := range states {
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

	// 1. Weights.
	weightSum := 0.0
	for _, a := range ds.Enabled {
		weightSum += a.Item.Weight
		if a.Item.Weight < 0 {
			block(ResearchReadinessIssue{
				AssetKey: a.Item.AssetKey, Reason: ResearchReasonNegativeWeight,
				Message: "权重为负数",
			})
		}
		if a.Item.Weight > 1+ResearchWeightTolerance {
			block(ResearchReadinessIssue{
				AssetKey: a.Item.AssetKey, Reason: ResearchReasonWeightExceeds100,
				Message: "单资产权重大于 100%",
			})
		}
	}
	out.WeightSum = weightSum
	if len(ds.Enabled) == 0 {
		block(ResearchReadinessIssue{
			Reason:  ResearchReasonNoEnabledAssets,
			Message: "集合没有启用的资产",
		})
	} else if math.Abs(weightSum-1) > ResearchWeightTolerance {
		block(ResearchReadinessIssue{
			Reason:  ResearchReasonWeightSumInvalid,
			Message: fmt.Sprintf("权重合计不是 100%%（当前 %.4f%%）", weightSum*100),
		})
	}

	// 2. Per-asset history checks + usable window assembly.
	type boundedRange struct {
		lo, hi   int
		assetKey string
	}
	var ranges []boundedRange
	prepared := map[string]preparedSeries{}
	staleCount, missingCount := 0, 0

	fxBounds := func(pairs []string) (int, int, bool, string) {
		lo, hi, set := 0, 0, false
		for _, pair := range pairs {
			fx, ok := ds.FX[pair]
			if !ok || !fx.Found {
				return 0, 0, false, pair
			}
			first, err1 := parseResearchDate(fx.Points[0].TradeDate)
			last, err2 := parseResearchDate(fx.Points[len(fx.Points)-1].TradeDate)
			if err1 != nil || err2 != nil {
				return 0, 0, false, pair
			}
			if !set {
				lo, hi, set = first, last, true
				continue
			}
			if first > lo {
				lo = first
			}
			if last < hi {
				hi = last
			}
		}
		return lo, hi, set, ""
	}

	for _, a := range ds.Enabled {
		view := ResearchReadinessAssetView{
			ItemID:        a.Item.ID,
			AssetKey:      a.Item.AssetKey,
			Name:          a.Asset.Name,
			Currency:      a.Asset.Currency,
			IsCash:        a.IsCash,
			Enabled:       a.Item.Enabled,
			Weight:        a.Item.Weight,
			AdjustPolicy:  a.Item.AdjustPolicy,
			PointType:     a.Item.PointType,
			ListingStatus: a.Asset.ListingStatus,
			FXPairs:       a.FXPairs,
		}

		// FX requirements first: they apply to cash and non-cash alike.
		for _, pair := range a.FXPairs {
			fx, ok := ds.FX[pair]
			if !ok || !fx.Found {
				block(ResearchReadinessIssue{
					AssetKey: a.Item.AssetKey, Pair: pair, Reason: ResearchReasonFXMissing,
					Message: fmt.Sprintf("%s 资产需要 %s 历史汇率", a.Asset.Currency, pair),
				})
			}
		}

		if a.IsCash {
			// Cash: no history requirement; FX bounds (if any) constrain the
			// window.
			if lo, hi, ok, _ := fxBounds(a.FXPairs); ok {
				ranges = append(ranges, boundedRange{lo: lo, hi: hi, assetKey: a.Item.AssetKey})
				view.HistoryStart = researchDayToDate(lo)
				view.HistoryEnd = researchDayToDate(hi)
			}
			view.HasHistory = true
			out.Assets = append(out.Assets, view)
			continue
		}

		if a.Task != nil {
			view.SyncStatus = a.Task.Status
			if a.Task.Status == repository.WorkerTaskStatusFailed {
				view.SyncError = a.Task.ErrorMessage
				if view.SyncError == "" {
					view.SyncError = a.Task.ErrorCode
				}
			}
			if repository.IsActiveWorkerTaskStatus(a.Task.Status) {
				block(ResearchReadinessIssue{
					AssetKey: a.Item.AssetKey, Reason: ResearchReasonHistorySyncing,
					Message: "历史同步正在运行",
				})
			}
		}

		if len(a.Points) == 0 {
			missingCount++
			if a.Task != nil && a.Task.Status == repository.WorkerTaskStatusFailed {
				block(ResearchReadinessIssue{
					AssetKey: a.Item.AssetKey, Reason: ResearchReasonHistorySyncFailed,
					Message: "历史同步失败且没有可用旧数据",
				})
			} else if a.Task == nil || !repository.IsActiveWorkerTaskStatus(a.Task.Status) {
				block(ResearchReadinessIssue{
					AssetKey: a.Item.AssetKey, Reason: ResearchReasonHistoryMissing,
					Message: "缺少历史点位",
				})
			}
			out.Assets = append(out.Assets, view)
			continue
		}

		view.HasHistory = true
		view.PointCount = len(a.Points)
		view.HistoryStart = a.Points[0].TradeDate
		view.HistoryEnd = a.Points[len(a.Points)-1].TradeDate
		view.DataAsOf = a.State.DataAsOf
		if view.DataAsOf == "" {
			view.DataAsOf = view.HistoryEnd
		}

		if a.Task != nil && a.Task.Status == repository.WorkerTaskStatusFailed {
			warn(ResearchReadinessIssue{
				AssetKey: a.Item.AssetKey, Reason: ResearchWarnSyncFailedStale,
				Message: "最近一次历史同步失败，当前使用旧数据",
			})
		}
		if a.NonPositiveCount > 0 {
			block(ResearchReadinessIssue{
				AssetKey: a.Item.AssetKey, Reason: ResearchReasonNonPositivePoints,
				Message: fmt.Sprintf("存在 %d 个非正数点位", a.NonPositiveCount),
			})
		}
		if len(a.SourceNames) > 1 {
			block(ResearchReadinessIssue{
				AssetKey: a.Item.AssetKey, Reason: ResearchReasonMixedSources,
				Message: "历史点位来自多个数据源，无法确认口径：" + strings.Join(a.SourceNames, ", "),
			})
		}

		if a.Asset.ListingStatus != "" && a.Asset.ListingStatus != "active" {
			warn(ResearchReadinessIssue{
				AssetKey: a.Item.AssetKey, Reason: ResearchWarnAssetInactive,
				Message: "资产已停更/退市，数据不再更新",
			})
		} else if view.DataAsOf != "" {
			if asOfDay, err := parseResearchDate(view.DataAsOf); err == nil {
				nowDay := int(now.Unix() / 86400)
				tolerance := ResearchStaleToleranceDays(a.Asset.InstrumentType)
				if nowDay-asOfDay > tolerance {
					view.Stale = true
					staleCount++
					warn(ResearchReadinessIssue{
						AssetKey: a.Item.AssetKey, Reason: ResearchWarnStaleData,
						Message: fmt.Sprintf("数据截至 %s，超过 %d 天未更新", view.DataAsOf, tolerance),
					})
				}
			}
		}

		series, err := prepareSeries(assetPointsToSeries(a.Points))
		if err != nil || series.empty() {
			// Non-positive points already blocked above; skip range math.
			out.Assets = append(out.Assets, view)
			continue
		}
		prepared[a.Item.AssetKey] = series
		lo, hi := series.firstDay(), series.lastDay()
		// FX-missing pairs are already blocked per asset above; when FX data
		// exists it narrows the asset's usable range.
		if fxLo, fxHi, ok, _ := fxBounds(a.FXPairs); ok {
			if fxLo > lo {
				lo = fxLo
			}
			if fxHi < hi {
				hi = fxHi
			}
		}
		if hi > lo {
			ranges = append(ranges, boundedRange{lo: lo, hi: hi, assetKey: a.Item.AssetKey})
		}
		out.Assets = append(out.Assets, view)
	}
	out.DataDependencies.StaleAssetCount = staleCount
	out.DataDependencies.MissingHistoryCount = missingCount

	// 3. FX-level checks.
	if ds.FXSyncActive {
		for _, pair := range ds.FXPairs {
			block(ResearchReadinessIssue{
				Pair: pair, Reason: ResearchReasonFXSyncing,
				Message: "汇率同步正在运行",
			})
		}
	}
	for _, pair := range ds.FXPairs {
		fx, ok := ds.FX[pair]
		if !ok || !fx.Found {
			continue // already blocked per asset
		}
		if fx.NonPositiveCount > 0 {
			block(ResearchReadinessIssue{
				Pair: pair, Reason: ResearchReasonNonPositivePoints,
				Message: fmt.Sprintf("FX %s 存在 %d 个非正数点位", pair, fx.NonPositiveCount),
			})
		}
	}

	// 4. Benchmark.
	if ds.Benchmark != nil {
		b := ds.Benchmark
		if !b.IsCash && len(b.Points) == 0 {
			block(ResearchReadinessIssue{
				AssetKey: b.Item.AssetKey, Reason: ResearchReasonBenchmarkNoHistory,
				Message: "基准资产缺少历史点位",
			})
		}
		for _, pair := range b.FXPairs {
			fx, ok := ds.FX[pair]
			if !ok || !fx.Found {
				block(ResearchReadinessIssue{
					AssetKey: b.Item.AssetKey, Pair: pair, Reason: ResearchReasonFXMissing,
					Message: fmt.Sprintf("基准资产需要 %s 历史汇率", pair),
				})
			}
		}
	}

	// 5. Common window.
	haveWindow := false
	var commonLo, commonHi int
	if len(ds.Enabled) > 0 {
		if len(ranges) == 0 {
			if out.DataDependencies.MissingHistoryCount == 0 && allCash(ds) {
				block(ResearchReadinessIssue{
					Reason:  ResearchReasonWindowEmpty,
					Message: "纯现金组合没有可回测的历史区间",
				})
			}
		} else {
			commonLo, commonHi = ranges[0].lo, ranges[0].hi
			for _, r := range ranges[1:] {
				if r.lo > commonLo {
					commonLo = r.lo
				}
				if r.hi < commonHi {
					commonHi = r.hi
				}
			}
			if commonHi <= commonLo {
				block(ResearchReadinessIssue{
					Reason:  ResearchReasonWindowEmpty,
					Message: "资产历史没有共同重叠区间",
				})
			} else {
				haveWindow = true
				out.CommonStart = researchDayToDate(commonLo)
				out.CommonEnd = researchDayToDate(commonHi)
				for i := range out.Assets {
					for _, r := range ranges {
						if r.assetKey != out.Assets[i].AssetKey {
							continue
						}
						out.Assets[i].LimitsCommonStart = r.lo == commonLo
						out.Assets[i].LimitsCommonEnd = r.hi == commonHi
					}
				}
			}
		}
	}

	if haveWindow {
		winLo, winHi := commonLo, commonHi
		if ds.Collection.StartPolicy == ResearchStartPolicyCustom {
			if ds.Collection.WindowStart != "" {
				if d, err := parseResearchDate(ds.Collection.WindowStart); err == nil && d > winLo {
					winLo = d
				}
			}
			if ds.Collection.WindowEnd != "" {
				if d, err := parseResearchDate(ds.Collection.WindowEnd); err == nil && d < winHi {
					winHi = d
				}
			}
		}
		switch {
		case winHi <= winLo:
			block(ResearchReadinessIssue{
				Reason:  ResearchReasonWindowEmpty,
				Message: "指定的回测区间与共同可用区间没有重叠",
			})
		case winHi-winLo < researchReadinessMinWindowDays:
			block(ResearchReadinessIssue{
				Reason: ResearchReasonWindowTooShort,
				Message: fmt.Sprintf("回测区间 %s ~ %s 短于最小长度 1 年",
					researchDayToDate(winLo), researchDayToDate(winHi)),
			})
		default:
			out.WindowStart = researchDayToDate(winLo)
			out.WindowEnd = researchDayToDate(winHi)
			if winHi-winLo < researchReadinessShortWindowDays {
				warn(ResearchReadinessIssue{
					Reason:  ResearchWarnShortWindow,
					Message: "共同回测区间短于 3 年，结果不建议作为 FIRE 资产决策依据",
				})
			}

			// Effective valuation days inside the window.
			effectiveObs := map[int]bool{}
			for _, series := range prepared {
				for _, day := range series.days {
					if day >= winLo && day <= winHi {
						effectiveObs[day] = true
					}
				}
			}
			for _, pair := range ds.FXPairs {
				fx := ds.FX[pair]
				if fx == nil || !fx.Found {
					continue
				}
				for _, p := range fx.Points {
					if d, err := parseResearchDate(p.TradeDate); err == nil && d >= winLo && d <= winHi {
						effectiveObs[d] = true
					}
				}
			}
			if len(prepared) > 0 && len(effectiveObs) < researchReadinessMinEffectiveObs {
				block(ResearchReadinessIssue{
					Reason:  ResearchReasonTooFewEffectiveDays,
					Message: "有效估值日不足，无法计算指标",
				})
			}

			// Per-asset fill gaps inside the window (warning).
			for _, a := range ds.Enabled {
				series, ok := prepared[a.Item.AssetKey]
				if !ok {
					continue
				}
				tolerance := ResearchFillGapToleranceDays(a.Asset.InstrumentType)
				fillLo := maxInt(winLo, series.firstDay())
				fillHi := minInt(winHi, series.lastDay())
				if fillHi >= fillLo {
					if _, maxRun := series.fillStats(fillLo, fillHi); maxRun > tolerance {
						warn(ResearchReadinessIssue{
							AssetKey: a.Item.AssetKey, Reason: ResearchWarnExcessiveFill,
							Message: fmt.Sprintf("历史存在超过 %d 天的连续缺口（最长 %d 天），将按前值填充",
								tolerance, maxRun),
						})
					}
				}
			}
			// Lagging data (warning): this asset's series end determines
			// common_end while another asset's data reaches clearly further.
			for _, a := range ds.Enabled {
				series, ok := prepared[a.Item.AssetKey]
				if !ok || series.lastDay() != commonHi {
					continue
				}
				if anyOtherEndsLater(prepared, a.Item.AssetKey, series.lastDay(), researchDataLaggingSlackDays) {
					warn(ResearchReadinessIssue{
						AssetKey: a.Item.AssetKey, Reason: ResearchWarnDataLagging,
						Message: fmt.Sprintf("该资产数据截至 %s，明显滞后并使共同终点提前",
							researchDayToDate(series.lastDay())),
					})
				}
			}

			// FX gaps inside the window (blocking).
			for _, pair := range ds.FXPairs {
				fx := ds.FX[pair]
				if fx == nil || !fx.Found {
					continue
				}
				fxSeries, err := prepareSeries(fxPointsToSeries(fx.Points))
				if err != nil || fxSeries.empty() {
					continue
				}
				fillLo := maxInt(winLo, fxSeries.firstDay())
				fillHi := minInt(winHi, fxSeries.lastDay())
				if fillHi < fillLo {
					continue
				}
				if _, maxRun := fxSeries.fillStats(fillLo, fillHi); maxRun > researchReadinessFXFillGapDays {
					block(ResearchReadinessIssue{
						Pair: pair, Reason: ResearchReasonFXGapExceeded,
						Message: fmt.Sprintf("FX %s 历史缺口超过容忍间隔（最长 %d 天）", pair, maxRun),
					})
				}
			}

			// High pairwise correlation (warning).
			addCorrelationWarnings(ds, prepared, winLo, winHi, warn)
		}
	}

	// 6. Concentration warnings.
	addConcentrationWarnings(ds, warn)

	out.Ready = len(out.BlockingReasons) == 0
	return out
}

func allCash(ds *researchDataset) bool {
	for _, a := range ds.Enabled {
		if !a.IsCash {
			return false
		}
	}
	return len(ds.Enabled) > 0
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
				entries[i].series, entries[j].series, winLo, winHi)
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
