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
	type obs struct {
		date  string
		day   int
		value float64
	}
	series := make([]obs, 0, len(points))
	for _, p := range points {
		if p.Value <= 0 || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			continue
		}
		day, err := parseResearchDate(p.TradeDate)
		if err != nil {
			continue
		}
		series = append(series, obs{date: p.TradeDate, day: day, value: p.Value})
	}
	sort.Slice(series, func(i, j int) bool { return series[i].day < series[j].day })

	// Coverage facts mirror the raw stored series (not the filtered one) so
	// the staleness check against market_asset_history_state stays stable.
	m.PointCount = len(points)
	if len(points) > 0 {
		rawDates := make([]string, len(points))
		for i, p := range points {
			rawDates[i] = p.TradeDate
		}
		sort.Strings(rawDates)
		m.StartDate = rawDates[0]
		m.EndDate = rawDates[len(rawDates)-1]
	}
	if len(series) == 0 {
		return m
	}

	first, last := series[0], series[len(series)-1]
	spanDays := last.day - first.day
	m.HistoryYears = float64(spanDays) / 365.25

	if spanDays > 0 && first.value > 0 {
		cagr := math.Pow(last.value/first.value, 365.25/float64(spanDays)) - 1
		m.CAGR = &cagr
	}

	// Observation-to-observation returns.
	rets := make([]float64, 0, len(series)-1)
	for i := 1; i < len(series); i++ {
		rets = append(rets, series[i].value/series[i-1].value-1)
	}
	if len(rets) >= researchMetricsMinReturnSamples {
		vol := sampleStd(rets) * math.Sqrt(252)
		m.AnnualVolatility = &vol

		// Downside deviation vs 0 (annualized).
		sumSq := 0.0
		for _, r := range rets {
			if r < 0 {
				sumSq += r * r
			}
		}
		downside := math.Sqrt(sumSq/float64(len(rets))) * math.Sqrt(252)
		m.DownsideVolatility = &downside

		if m.CAGR != nil && vol > 0 {
			sharpe := *m.CAGR / vol
			m.Sharpe = &sharpe
		}
	}

	// Max drawdown over observations.
	peak := series[0].value
	minDD := 0.0
	for _, o := range series {
		if o.value > peak {
			peak = o.value
		}
		if dd := o.value/peak - 1; dd < minDD {
			minDD = dd
		}
	}
	if minDD < 0 {
		dd := minDD
		m.MaxDrawdown = &dd
		if m.CAGR != nil {
			calmar := *m.CAGR / math.Abs(minDD)
			m.Calmar = &calmar
		}
	}

	// Trailing returns: end value vs the last observation at or before the
	// target date; only defined when history reaches back far enough.
	trailing := func(years int) *float64 {
		endT, err := time.Parse("2006-01-02", last.date)
		if err != nil {
			return nil
		}
		targetDay, err := parseResearchDate(endT.AddDate(-years, 0, 0).Format("2006-01-02"))
		if err != nil {
			return nil
		}
		if first.day > targetDay {
			return nil
		}
		idx := sort.Search(len(series), func(i int) bool { return series[i].day > targetDay })
		if idx == 0 {
			return nil
		}
		base := series[idx-1].value
		if base <= 0 {
			return nil
		}
		r := last.value/base - 1
		return &r
	}
	m.Return1Y = trailing(1)
	m.Return3Y = trailing(3)
	m.Return5Y = trailing(5)
	return m
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
