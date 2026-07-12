package service

import (
	"math"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

// --- dataset builders ---

func rdNow(t *testing.T) time.Time {
	t.Helper()
	now, err := time.Parse("2006-01-02", "2024-06-30")
	if err != nil {
		t.Fatalf("parse now: %v", err)
	}
	return now
}

func rdPoints(t *testing.T, start string, days int, value func(i int) float64) []repository.MarketAssetPoint {
	t.Helper()
	st, err := time.Parse("2006-01-02", start)
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	out := make([]repository.MarketAssetPoint, 0, days)
	for i := 0; i < days; i++ {
		out = append(out, repository.MarketAssetPoint{
			TradeDate:  st.AddDate(0, 0, i).Format("2006-01-02"),
			Value:      value(i),
			SourceName: "test_source",
		})
	}
	return out
}

// rdAsset builds an enabled CNY equity asset with daily history ending near
// the test "now" (2024-06-30).
func rdAsset(t *testing.T, key string, weight float64, start string, days int) researchAssetData {
	t.Helper()
	points := rdPoints(t, start, days, func(i int) float64 {
		return 100 * (1 + 0.05*math.Sin(float64(i)/7)) * math.Pow(1.0002, float64(i))
	})
	return researchAssetData{
		Item: repository.ResearchCollectionItem{
			ID: "item_" + key, AssetKey: key, Enabled: true, Weight: weight,
			AdjustPolicy: "hfq", PointType: "adjusted_close",
		},
		Asset: repository.MarketAsset{
			AssetKey: key, Market: "CN", InstrumentType: "cn_exchange_fund",
			Name: key, Currency: "CNY", Active: true, ListingStatus: "active",
		},
		HasState: true,
		State: repository.MarketAssetHistoryState{
			AssetKey: key, AdjustPolicy: "hfq", PointType: "adjusted_close",
			DataAsOf: points[len(points)-1].TradeDate, PointCount: len(points),
		},
		Points:      points,
		SourceNames: []string{"test_source"},
	}
}

func rdDataset(assets ...researchAssetData) *researchDataset {
	return &researchDataset{
		Collection: repository.ResearchCollection{
			ID: "rc_test", BaseCurrency: "CNY",
			RebalancePolicy: ResearchRebalanceMonthly,
			StartPolicy:     ResearchStartPolicyCommon,
		},
		Enabled: assets,
		FX:      map[string]*researchFXData{},
	}
}

func hasBlock(r ResearchReadiness, reason string) bool {
	for _, b := range r.BlockingReasons {
		if b.Reason == reason {
			return true
		}
	}
	return false
}

func hasWarn(r ResearchReadiness, reason string) bool {
	for _, w := range r.Warnings {
		if w.Reason == reason {
			return true
		}
	}
	return false
}

// --- tests ---

func TestReadinessPassesForHealthyPortfolio(t *testing.T) {
	// Two CNY assets with ~4.5y of daily data ending at "now".
	a := rdAsset(t, "A", 0.5, "2020-01-01", 1642)
	b := rdAsset(t, "B", 0.5, "2020-01-01", 1642)
	// Decorrelate B.
	for i := range b.Points {
		b.Points[i].Value = 100 * (1 + 0.05*math.Cos(float64(i)/11))
	}
	r := evaluateResearchReadiness(rdDataset(a, b), rdNow(t))
	if !r.Ready {
		t.Fatalf("expected ready, got blocks %+v", r.BlockingReasons)
	}
	if r.CommonStart != "2020-01-01" {
		t.Fatalf("common start expected 2020-01-01, got %s", r.CommonStart)
	}
	if r.WeightSum != 1 {
		t.Fatalf("weight sum expected 1, got %v", r.WeightSum)
	}
	if r.DataDependencies.AssetCount != 2 || len(r.DataDependencies.FXPairs) != 0 {
		t.Fatalf("data dependencies wrong: %+v", r.DataDependencies)
	}
}

func TestReadinessReportsExactCVaRSampleGate(t *testing.T) {
	asset := rdAsset(t, "TAIL", 1, "2020-01-01", 400)
	ds := rdDataset(asset)
	ds.Collection.TailRiskConfidence = 0.99
	ds.Collection.TailRiskHorizonDays = 20
	readiness := evaluateResearchReadiness(ds, rdNow(t))
	if !hasBlock(readiness, ResearchReasonCVARSample) || readiness.TailRisk == nil {
		t.Fatalf("expected CVaR sample blocker: %+v", readiness)
	}
	if readiness.TailRisk.EffectiveReturnCount != 399 || readiness.TailRisk.ScenarioCount != 380 ||
		readiness.TailRisk.MinimumScenarioCount != 500 {
		t.Fatalf("unexpected CVaR readiness counts: %+v", readiness.TailRisk)
	}

	asset = rdAsset(t, "TAIL", 1, "2020-01-01", 550)
	ds = rdDataset(asset)
	ds.Collection.TailRiskConfidence = 0.99
	ds.Collection.TailRiskHorizonDays = 20
	readiness = evaluateResearchReadiness(ds, rdNow(t))
	if hasBlock(readiness, ResearchReasonCVARSample) || readiness.TailRisk == nil || readiness.TailRisk.ScenarioCount != 530 {
		t.Fatalf("CVaR readiness did not become ready after data extension: %+v", readiness)
	}
}

func TestReadinessBlocksNoEnabledAssets(t *testing.T) {
	r := evaluateResearchReadiness(rdDataset(), rdNow(t))
	if r.Ready || !hasBlock(r, ResearchReasonNoEnabledAssets) {
		t.Fatalf("expected no_enabled_assets block, got %+v", r.BlockingReasons)
	}
}

func TestReadinessBlocksWeightIssues(t *testing.T) {
	a := rdAsset(t, "A", 0.5, "2020-01-01", 1642)
	b := rdAsset(t, "B", 0.4, "2020-01-01", 1642)
	r := evaluateResearchReadiness(rdDataset(a, b), rdNow(t))
	if r.Ready || !hasBlock(r, ResearchReasonWeightSumInvalid) {
		t.Fatalf("expected weight_sum_invalid, got %+v", r.BlockingReasons)
	}

	neg := rdAsset(t, "N", -0.2, "2020-01-01", 1642)
	pos := rdAsset(t, "P", 1.2, "2020-01-01", 1642)
	r = evaluateResearchReadiness(rdDataset(neg, pos), rdNow(t))
	if !hasBlock(r, ResearchReasonNegativeWeight) || !hasBlock(r, ResearchReasonWeightExceeds100) {
		t.Fatalf("expected negative/exceed blocks, got %+v", r.BlockingReasons)
	}

	// Tolerance: 1e-7 off still passes weight validation.
	a = rdAsset(t, "A", 0.5, "2020-01-01", 1642)
	b = rdAsset(t, "B", 0.5-5e-8, "2020-01-01", 1642)
	r = evaluateResearchReadiness(rdDataset(a, b), rdNow(t))
	if hasBlock(r, ResearchReasonWeightSumInvalid) {
		t.Fatalf("weight within tolerance must pass, got %+v", r.BlockingReasons)
	}
}

func TestReadinessBlocksMissingHistory(t *testing.T) {
	a := rdAsset(t, "A", 0.5, "2020-01-01", 1642)
	b := rdAsset(t, "B", 0.5, "2020-01-01", 1642)
	b.Points = nil
	b.SourceNames = nil
	r := evaluateResearchReadiness(rdDataset(a, b), rdNow(t))
	if r.Ready || !hasBlock(r, ResearchReasonHistoryMissing) {
		t.Fatalf("expected history_missing, got %+v", r.BlockingReasons)
	}
	if r.DataDependencies.MissingHistoryCount != 1 {
		t.Fatalf("missing history count expected 1, got %d", r.DataDependencies.MissingHistoryCount)
	}
}

func TestReadinessBlocksUnsupportedSeriesButNotLargeHFQMove(t *testing.T) {
	raw := rdAsset(t, "RAW", 1, "2020-01-01", 1642)
	raw.Item.AdjustPolicy = "none"
	raw.Item.PointType = "close"
	r := evaluateResearchReadiness(rdDataset(raw), rdNow(t))
	if !hasBlock(r, ResearchReasonUnadjustedSeries) {
		t.Fatalf("expected unadjusted series block, got %+v", r.BlockingReasons)
	}

	qfq := rdAsset(t, "QFQ", 1, "2020-01-01", 1642)
	qfq.Item.AdjustPolicy = "qfq"
	r = evaluateResearchReadiness(rdDataset(qfq), rdNow(t))
	if !hasBlock(r, ResearchReasonUnsupportedSeries) {
		t.Fatalf("expected unsupported return series block, got %+v", r.BlockingReasons)
	}

	largeMove := rdAsset(t, "LARGE", 1, "2020-01-01", 1642)
	largeMove.Points[500].Value = largeMove.Points[499].Value * 2.0094
	r = evaluateResearchReadiness(rdDataset(largeMove), rdNow(t))
	if hasBlock(r, ResearchReasonUnsupportedSeries) || hasBlock(r, ResearchReasonUnadjustedSeries) {
		t.Fatalf("large hfq move must not be blocked by magnitude alone: %+v", r.BlockingReasons)
	}
}

func TestReadinessBlocksSyncingAndFailed(t *testing.T) {
	a := rdAsset(t, "A", 1, "2020-01-01", 1642)
	a.Task = &repository.WorkerTask{ID: "wt1", Status: repository.WorkerTaskStatusRunning}
	r := evaluateResearchReadiness(rdDataset(a), rdNow(t))
	if !hasBlock(r, ResearchReasonHistorySyncing) {
		t.Fatalf("expected history_syncing, got %+v", r.BlockingReasons)
	}

	// Failed sync without old data blocks; with old data only warns.
	b := rdAsset(t, "B", 1, "2020-01-01", 1642)
	b.Points = nil
	b.Task = &repository.WorkerTask{
		ID: "wt2", Status: repository.WorkerTaskStatusFailed, ErrorCode: "source_unavailable",
	}
	r = evaluateResearchReadiness(rdDataset(b), rdNow(t))
	if !hasBlock(r, ResearchReasonHistorySyncFailed) {
		t.Fatalf("expected history_sync_failed, got %+v", r.BlockingReasons)
	}

	c := rdAsset(t, "C", 1, "2020-01-01", 1642)
	c.Task = &repository.WorkerTask{ID: "wt3", Status: repository.WorkerTaskStatusFailed}
	r = evaluateResearchReadiness(rdDataset(c), rdNow(t))
	if hasBlock(r, ResearchReasonHistorySyncFailed) {
		t.Fatalf("failed sync with old data must not block, got %+v", r.BlockingReasons)
	}
	if !hasWarn(r, ResearchWarnSyncFailedStale) {
		t.Fatalf("expected history_sync_failed_stale warning, got %+v", r.Warnings)
	}
}

func TestReadinessBlocksDisjointWindows(t *testing.T) {
	a := rdAsset(t, "A", 0.5, "2010-01-01", 800)
	b := rdAsset(t, "B", 0.5, "2020-01-01", 1642)
	r := evaluateResearchReadiness(rdDataset(a, b), rdNow(t))
	if r.Ready || !hasBlock(r, ResearchReasonWindowEmpty) {
		t.Fatalf("expected common_window_empty, got %+v", r.BlockingReasons)
	}
}

func TestReadinessWindowLengthRules(t *testing.T) {
	// ~200 days of overlap: blocked.
	a := rdAsset(t, "A", 0.5, "2023-12-13", 200)
	b := rdAsset(t, "B", 0.5, "2023-12-13", 200)
	r := evaluateResearchReadiness(rdDataset(a, b), rdNow(t))
	if !hasBlock(r, ResearchReasonWindowTooShort) {
		t.Fatalf("expected common_window_too_short, got %+v", r.BlockingReasons)
	}

	// ~2 years: allowed with strong warning.
	a = rdAsset(t, "A", 0.5, "2022-07-01", 730)
	b = rdAsset(t, "B", 0.5, "2022-07-01", 730)
	r = evaluateResearchReadiness(rdDataset(a, b), rdNow(t))
	if hasBlock(r, ResearchReasonWindowTooShort) {
		t.Fatalf("2y window must not block, got %+v", r.BlockingReasons)
	}
	if !hasWarn(r, ResearchWarnShortWindow) {
		t.Fatalf("expected short_common_window warning, got %+v", r.Warnings)
	}
}

func TestReadinessCustomWindowClamp(t *testing.T) {
	a := rdAsset(t, "A", 1, "2018-01-01", 2372) // ends ~2024-06-30
	ds := rdDataset(a)
	ds.Collection.StartPolicy = ResearchStartPolicyCustom
	ds.Collection.WindowStart = "2020-01-01"
	ds.Collection.WindowEnd = "2023-01-01"
	r := evaluateResearchReadiness(ds, rdNow(t))
	if !r.Ready {
		t.Fatalf("expected ready, got %+v", r.BlockingReasons)
	}
	if r.WindowStart != "2020-01-01" || r.WindowEnd != "2023-01-01" {
		t.Fatalf("custom window not applied: %s..%s", r.WindowStart, r.WindowEnd)
	}
	// Common window facts still reflect data coverage.
	if r.CommonStart != "2018-01-01" {
		t.Fatalf("common start expected 2018-01-01, got %s", r.CommonStart)
	}

	ds.Collection.WindowStart = "2030-01-01"
	ds.Collection.WindowEnd = ""
	r = evaluateResearchReadiness(ds, rdNow(t))
	if !hasBlock(r, ResearchReasonWindowEmpty) {
		t.Fatalf("out-of-range custom window must block, got %+v", r.BlockingReasons)
	}
}

func TestReadinessFXRules(t *testing.T) {
	usd := rdAsset(t, "US|us_etf||VOO", 0.5, "2020-01-01", 1642)
	usd.Asset.Currency = "USD"
	usd.Asset.Market = "US"
	usd.FXPairs = []string{"USDCNY"}
	cny := rdAsset(t, "CN|cn_exchange_fund|sh|510300", 0.5, "2020-01-01", 1642)
	ds := rdDataset(usd, cny)
	ds.FXPairs = []string{"USDCNY"}
	ds.FX["USDCNY"] = &researchFXData{Pair: "USDCNY", Found: false}
	r := evaluateResearchReadiness(ds, rdNow(t))
	if r.Ready || !hasBlock(r, ResearchReasonFXMissing) {
		t.Fatalf("expected fx_missing, got %+v", r.BlockingReasons)
	}

	// With FX data present the check passes.
	var fxPoints []repository.MarketDataPoint
	st, _ := time.Parse("2006-01-02", "2019-01-01")
	for i := 0; i < 2008; i++ { // through ~2024-06-30
		fxPoints = append(fxPoints, repository.MarketDataPoint{
			TradeDate: st.AddDate(0, 0, i).Format("2006-01-02"), Value: 7,
		})
	}
	ds.FX["USDCNY"] = &researchFXData{Pair: "USDCNY", Found: true, Points: fxPoints}
	r = evaluateResearchReadiness(ds, rdNow(t))
	if hasBlock(r, ResearchReasonFXMissing) {
		t.Fatalf("fx present must not block, got %+v", r.BlockingReasons)
	}

	// FX sync running blocks.
	ds.FXSyncActive = true
	r = evaluateResearchReadiness(ds, rdNow(t))
	if !hasBlock(r, ResearchReasonFXSyncing) {
		t.Fatalf("expected fx_syncing, got %+v", r.BlockingReasons)
	}
	ds.FXSyncActive = false

	// FX gap over tolerance blocks: cut 30 days out of the middle.
	var gapped []repository.MarketDataPoint
	for i, p := range fxPoints {
		if i > 1000 && i <= 1030 {
			continue
		}
		gapped = append(gapped, p)
	}
	ds.FX["USDCNY"] = &researchFXData{Pair: "USDCNY", Found: true, Points: gapped}
	r = evaluateResearchReadiness(ds, rdNow(t))
	if !hasBlock(r, ResearchReasonFXGapExceeded) {
		t.Fatalf("expected fx_gap_exceeded, got %+v", r.BlockingReasons)
	}
}

func TestReadinessFXBoundsNarrowWindow(t *testing.T) {
	usd := rdAsset(t, "USD_ASSET", 1, "2015-01-01", 3468) // through ~2024-06-30
	usd.Asset.Currency = "USD"
	usd.FXPairs = []string{"USDCNY"}
	ds := rdDataset(usd)
	ds.FXPairs = []string{"USDCNY"}
	var fxPoints []repository.MarketDataPoint
	st, _ := time.Parse("2006-01-02", "2020-01-01")
	for i := 0; i < 1642; i++ {
		fxPoints = append(fxPoints, repository.MarketDataPoint{
			TradeDate: st.AddDate(0, 0, i).Format("2006-01-02"), Value: 7,
		})
	}
	ds.FX["USDCNY"] = &researchFXData{Pair: "USDCNY", Found: true, Points: fxPoints}
	r := evaluateResearchReadiness(ds, rdNow(t))
	if !r.Ready {
		t.Fatalf("expected ready, got %+v", r.BlockingReasons)
	}
	if r.CommonStart != "2020-01-01" {
		t.Fatalf("FX availability should delay common start, got %s", r.CommonStart)
	}
}

func TestReadinessBlocksBadPointsAndMixedSources(t *testing.T) {
	a := rdAsset(t, "A", 1, "2020-01-01", 1642)
	a.NonPositiveCount = 2
	r := evaluateResearchReadiness(rdDataset(a), rdNow(t))
	if !hasBlock(r, ResearchReasonNonPositivePoints) {
		t.Fatalf("expected non_positive_points, got %+v", r.BlockingReasons)
	}

	b := rdAsset(t, "B", 1, "2020-01-01", 1642)
	b.SourceNames = []string{"alpha", "beta"}
	r = evaluateResearchReadiness(rdDataset(b), rdNow(t))
	if !hasBlock(r, ResearchReasonMixedSources) {
		t.Fatalf("expected mixed_sources, got %+v", r.BlockingReasons)
	}
}

func TestReadinessBenchmarkChecks(t *testing.T) {
	a := rdAsset(t, "A", 1, "2020-01-01", 1642)
	ds := rdDataset(a)
	bench := rdAsset(t, "BENCH", 0, "2020-01-01", 1642)
	bench.Points = nil
	ds.Collection.BenchmarkAssetKey = "BENCH"
	ds.Benchmark = &bench
	r := evaluateResearchReadiness(ds, rdNow(t))
	if !hasBlock(r, ResearchReasonBenchmarkNoHistory) {
		t.Fatalf("expected benchmark_history_missing, got %+v", r.BlockingReasons)
	}

	bench = rdAsset(t, "BENCH", 0, "2020-01-01", 1642)
	bench.NonPositiveCount = 1
	bench.SourceNames = []string{"source_a", "source_b"}
	ds.Benchmark = &bench
	r = evaluateResearchReadiness(ds, rdNow(t))
	if !hasBlock(r, ResearchReasonNonPositivePoints) || !hasBlock(r, ResearchReasonMixedSources) {
		t.Fatalf("base-currency benchmark quality checks missing: %+v", r.BlockingReasons)
	}
}

func TestReadinessBenchmarkMustCoverFinalWindow(t *testing.T) {
	portfolio := rdAsset(t, "A", 1, "2020-01-01", 1642)
	for _, tt := range []struct {
		name  string
		start string
		days  int
	}{
		{name: "starts late", start: "2020-01-02", days: 1641},
		{name: "ends early", start: "2020-01-01", days: 1641},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ds := rdDataset(portfolio)
			bench := rdAsset(t, "BENCH", 0, tt.start, tt.days)
			ds.Collection.BenchmarkAssetKey = bench.Item.AssetKey
			ds.Benchmark = &bench
			r := evaluateResearchReadiness(ds, rdNow(t))
			if !hasBlock(r, ResearchReasonBenchmarkWindow) {
				t.Fatalf("expected benchmark_window_not_covered, got %+v", r.BlockingReasons)
			}
		})
	}
}

func TestReadinessBenchmarkGapBlocks(t *testing.T) {
	portfolio := rdAsset(t, "A", 1, "2020-01-01", 1642)
	bench := rdAsset(t, "BENCH", 0, "2020-01-01", 1642)
	points := make([]repository.MarketAssetPoint, 0, len(bench.Points))
	for i, point := range bench.Points {
		if i > 800 && i <= 820 {
			continue
		}
		points = append(points, point)
	}
	bench.Points = points
	ds := rdDataset(portfolio)
	ds.Collection.BenchmarkAssetKey = bench.Item.AssetKey
	ds.Benchmark = &bench
	r := evaluateResearchReadiness(ds, rdNow(t))
	if !hasBlock(r, ResearchReasonBenchmarkGap) {
		t.Fatalf("expected benchmark_gap_exceeded, got %+v", r.BlockingReasons)
	}
}

func TestReadinessCashRules(t *testing.T) {
	// CNY cash + CNY equity: cash needs no history and no FX.
	cash := researchAssetData{
		Item: repository.ResearchCollectionItem{
			ID: "item_cash", AssetKey: "SYS|cash||CNY", Enabled: true, Weight: 0.5,
			AdjustPolicy: "none", PointType: "nav",
		},
		Asset: repository.MarketAsset{
			AssetKey: "SYS|cash||CNY", Market: "SYS", InstrumentType: "cash",
			Name: "人民币现金", Currency: "CNY", Active: true, ListingStatus: "active",
		},
		IsCash: true,
	}
	equity := rdAsset(t, "A", 0.5, "2020-01-01", 1642)
	r := evaluateResearchReadiness(rdDataset(cash, equity), rdNow(t))
	if !r.Ready {
		t.Fatalf("cash portfolio should be ready, got %+v", r.BlockingReasons)
	}
	if r.CommonStart != "2020-01-01" {
		t.Fatalf("cash must not bound the window, got %s", r.CommonStart)
	}

	// Pure cash portfolio has nothing to backtest.
	solo := cash
	solo.Item.Weight = 1
	r = evaluateResearchReadiness(rdDataset(solo), rdNow(t))
	if !hasBlock(r, ResearchReasonWindowEmpty) {
		t.Fatalf("pure cash must block, got %+v", r.BlockingReasons)
	}
}

func TestReadinessWarnings(t *testing.T) {
	// Stale data warning: series ends 60 days before now.
	stale := rdAsset(t, "STALE", 0.5, "2019-01-01", 1937) // ends ~2024-04-21
	fresh := rdAsset(t, "FRESH", 0.5, "2019-01-01", 2007) // ends ~2024-06-30
	stale.State.DataAsOf = stale.Points[len(stale.Points)-1].TradeDate
	r := evaluateResearchReadiness(rdDataset(stale, fresh), rdNow(t))
	if !hasWarn(r, ResearchWarnStaleData) {
		t.Fatalf("expected stale_data warning, got %+v", r.Warnings)
	}
	if r.DataDependencies.StaleAssetCount != 1 {
		t.Fatalf("stale count expected 1, got %d", r.DataDependencies.StaleAssetCount)
	}
	// The stale asset ends 70 days earlier and caps common_end: lagging.
	if !hasWarn(r, ResearchWarnDataLagging) {
		t.Fatalf("expected data_lagging warning, got %+v", r.Warnings)
	}

	// Inactive assets warn (and skip the stale warning).
	inactive := rdAsset(t, "OLD", 0.5, "2019-01-01", 1800)
	inactive.Asset.ListingStatus = "inactive"
	other := rdAsset(t, "B", 0.5, "2019-01-01", 2007)
	r = evaluateResearchReadiness(rdDataset(inactive, other), rdNow(t))
	if !hasWarn(r, ResearchWarnAssetInactive) {
		t.Fatalf("expected asset_inactive warning, got %+v", r.Warnings)
	}

	// Weight concentration warning.
	heavy := rdAsset(t, "H", 0.8, "2020-01-01", 1642)
	light := rdAsset(t, "L", 0.2, "2020-01-01", 1642)
	r = evaluateResearchReadiness(rdDataset(heavy, light), rdNow(t))
	if !hasWarn(r, ResearchWarnWeightConcentration) {
		t.Fatalf("expected weight_concentration warning, got %+v", r.Warnings)
	}
	// Both assets are CN/CNY: market and currency concentration.
	if !hasWarn(r, ResearchWarnMarketConcentration) || !hasWarn(r, ResearchWarnCurrencyConcentration) {
		t.Fatalf("expected concentration warnings, got %+v", r.Warnings)
	}
}

func TestReadinessHighCorrelationWarning(t *testing.T) {
	a := rdAsset(t, "A", 0.5, "2020-01-01", 1642)
	b := rdAsset(t, "B", 0.5, "2020-01-01", 1642)
	// Identical value paths: correlation 1.
	for i := range b.Points {
		b.Points[i].Value = a.Points[i].Value
	}
	r := evaluateResearchReadiness(rdDataset(a, b), rdNow(t))
	if !hasWarn(r, ResearchWarnHighCorrelation) {
		t.Fatalf("expected high_correlation warning, got %+v", r.Warnings)
	}
}

func TestReadinessExcessiveFillWarning(t *testing.T) {
	a := rdAsset(t, "A", 1, "2020-01-01", 1642)
	// Remove a 20-day block in the middle (tolerance for ETF is 7).
	var pruned []repository.MarketAssetPoint
	for i, p := range a.Points {
		if i > 800 && i <= 820 {
			continue
		}
		pruned = append(pruned, p)
	}
	a.Points = pruned
	r := evaluateResearchReadiness(rdDataset(a), rdNow(t))
	if !hasWarn(r, ResearchWarnExcessiveFill) {
		t.Fatalf("expected excessive_forward_fill warning, got %+v", r.Warnings)
	}
	if hasBlock(r, ResearchReasonWindowEmpty) {
		t.Fatalf("interior gap must not block, got %+v", r.BlockingReasons)
	}
}

func TestReadinessLimitsFlags(t *testing.T) {
	late := rdAsset(t, "LATE", 0.5, "2021-01-01", 1276) // starts later, ends ~2024-06-30
	early := rdAsset(t, "EARLY", 0.5, "2019-01-01", 2007)
	r := evaluateResearchReadiness(rdDataset(late, early), rdNow(t))
	var lateView, earlyView *ResearchReadinessAssetView
	for i := range r.Assets {
		switch r.Assets[i].AssetKey {
		case "LATE":
			lateView = &r.Assets[i]
		case "EARLY":
			earlyView = &r.Assets[i]
		}
	}
	if lateView == nil || !lateView.LimitsCommonStart {
		t.Fatalf("late starter should limit common start: %+v", lateView)
	}
	if earlyView == nil || earlyView.LimitsCommonStart {
		t.Fatalf("early starter should not limit common start: %+v", earlyView)
	}
}
