package service

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

// research_metrics.go computes the precomputed screener metrics projection
// (research_asset_metrics) for one market asset history dimension. Metrics
// that cannot be derived stay nil and are never stored as 0.

// researchMetricsMinReturnSamples is the minimum number of return samples
// required before volatility-family metrics are considered defined.
const researchMetricsMinReturnSamples = 2

type researchMetricObservation struct {
	date  string
	day   int
	value float64
}

// ComputeResearchAssetMetrics derives screener metrics from one stored
// history series (actual observations, no forward fill needed because the
// series itself is the observation set).
func ComputeResearchAssetMetrics(
	assetKey, adjustPolicy, pointType string,
	points []repository.MarketAssetPoint,
	computedAt int64,
) repository.ResearchAssetMetrics {
	m := repository.ResearchAssetMetrics{
		AssetKey:     assetKey,
		AdjustPolicy: adjustPolicy,
		PointType:    pointType,
		ComputedAt:   computedAt,
	}
	series := researchMetricSeries(points)
	populateResearchMetricCoverage(&m, points)
	if len(series) == 0 {
		return m
	}
	populateResearchMetricGrowth(&m, series)
	populateResearchMetricVolatility(&m, researchMetricReturns(series))
	populateResearchMetricDrawdown(&m, series)
	m.Return1Y = researchTrailingReturn(series, 1)
	m.Return3Y = researchTrailingReturn(series, 3)
	m.Return5Y = researchTrailingReturn(series, 5)
	return m
}

func researchMetricSeries(points []repository.MarketAssetPoint) []researchMetricObservation {
	series := make([]researchMetricObservation, 0, len(points))
	for _, point := range points {
		if point.Value <= 0 || math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
			continue
		}
		day, err := parseResearchDate(point.TradeDate)
		if err == nil {
			series = append(series, researchMetricObservation{
				date: point.TradeDate, day: day, value: point.Value,
			})
		}
	}
	sort.Slice(series, func(i, j int) bool { return series[i].day < series[j].day })
	return series
}

func populateResearchMetricCoverage(
	metrics *repository.ResearchAssetMetrics, points []repository.MarketAssetPoint,
) {
	metrics.PointCount = len(points)
	if len(points) == 0 {
		return
	}
	rawDates := make([]string, len(points))
	for i, point := range points {
		rawDates[i] = point.TradeDate
	}
	sort.Strings(rawDates)
	metrics.StartDate, metrics.EndDate = rawDates[0], rawDates[len(rawDates)-1]
}

func populateResearchMetricGrowth(
	metrics *repository.ResearchAssetMetrics, series []researchMetricObservation,
) {
	first, last := series[0], series[len(series)-1]
	spanDays := last.day - first.day
	metrics.HistoryYears = float64(spanDays) / 365.25
	if spanDays > 0 {
		cagr := math.Pow(last.value/first.value, 365.25/float64(spanDays)) - 1
		metrics.CAGR = &cagr
	}
}

func researchMetricReturns(series []researchMetricObservation) []float64 {
	returns := make([]float64, 0, maxInt(0, len(series)-1))
	for i := 1; i < len(series); i++ {
		returns = append(returns, series[i].value/series[i-1].value-1)
	}
	return returns
}

func populateResearchMetricVolatility(
	metrics *repository.ResearchAssetMetrics, returns []float64,
) {
	if len(returns) < researchMetricsMinReturnSamples {
		return
	}
	volatility := sampleStd(returns) * math.Sqrt(252)
	metrics.AnnualVolatility = &volatility
	sumSquaredDownside := 0.0
	for _, value := range returns {
		if value < 0 {
			sumSquaredDownside += value * value
		}
	}
	downside := math.Sqrt(sumSquaredDownside/float64(len(returns))) * math.Sqrt(252)
	metrics.DownsideVolatility = &downside
	if metrics.CAGR != nil && volatility > 0 {
		sharpe := *metrics.CAGR / volatility
		metrics.Sharpe = &sharpe
	}
}

func populateResearchMetricDrawdown(
	metrics *repository.ResearchAssetMetrics, series []researchMetricObservation,
) {
	peak, maxDrawdown := series[0].value, 0.0
	for _, observation := range series {
		peak = math.Max(peak, observation.value)
		maxDrawdown = math.Min(maxDrawdown, observation.value/peak-1)
	}
	if maxDrawdown >= 0 {
		return
	}
	metrics.MaxDrawdown = &maxDrawdown
	if metrics.CAGR != nil {
		calmar := *metrics.CAGR / math.Abs(maxDrawdown)
		metrics.Calmar = &calmar
	}
}

func researchTrailingReturn(series []researchMetricObservation, years int) *float64 {
	first, last := series[0], series[len(series)-1]
	endDate, err := time.Parse("2006-01-02", last.date)
	if err != nil {
		return nil
	}
	targetDay, err := parseResearchDate(endDate.AddDate(-years, 0, 0).Format("2006-01-02"))
	if err != nil || first.day > targetDay {
		return nil
	}
	index := sort.Search(len(series), func(i int) bool { return series[i].day > targetDay })
	if index == 0 || series[index-1].value <= 0 {
		return nil
	}
	value := last.value/series[index-1].value - 1
	return &value
}

// BackfillResearchAssetMetrics lazily recomputes metrics for history
// dimensions whose projection is missing or was computed against different
// coverage. It runs before screener queries so assets synced prior to the
// research module still expose metrics. Individual failures are logged and
// skipped; the screener then simply shows those assets without metrics.
func BackfillResearchAssetMetrics(
	ctx context.Context,
	assets *repository.MarketAssetRepo,
	research *repository.ResearchRepo,
	limit int,
	now int64,
) int {
	stale, err := research.ListStaleMetricsDimensions(ctx, limit)
	if err != nil {
		slog.WarnContext(ctx, "research metrics backfill: list stale dimensions", "error", err)
		return 0
	}
	done := 0
	for _, d := range stale {
		points, err := assets.ListPoints(ctx, d.AssetKey, d.AdjustPolicy, d.PointType)
		if err != nil {
			slog.WarnContext(ctx, "research metrics backfill: load points",
				"asset_key", d.AssetKey, "error", err)
			continue
		}
		m := ComputeResearchAssetMetrics(d.AssetKey, d.AdjustPolicy, d.PointType, points, now)
		if err := research.UpsertMetricsTx(ctx, nil, m); err != nil {
			slog.WarnContext(ctx, "research metrics backfill: upsert",
				"asset_key", d.AssetKey, "error", err)
			continue
		}
		done++
	}
	return done
}
