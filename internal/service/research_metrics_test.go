package service

import (
	"math"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

func genMarketPoints(t *testing.T, startDate string, days int, value func(i int) float64) []repository.MarketAssetPoint {
	t.Helper()
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		t.Fatalf("parse start date: %v", err)
	}
	out := make([]repository.MarketAssetPoint, 0, days)
	for i := 0; i < days; i++ {
		out = append(out, repository.MarketAssetPoint{
			TradeDate: start.AddDate(0, 0, i).Format("2006-01-02"),
			Value:     value(i),
		})
	}
	return out
}

func TestComputeResearchAssetMetricsBasics(t *testing.T) {
	days := 6*365 + 2 // ~6 years of daily data
	growth := 1.0003
	points := genMarketPoints(t, "2018-01-01", days, func(i int) float64 {
		return 100 * math.Pow(growth, float64(i))
	})
	m := ComputeResearchAssetMetrics("K", "none", "price", points, 123)

	if m.AssetKey != "K" || m.AdjustPolicy != "none" || m.PointType != "price" || m.ComputedAt != 123 {
		t.Fatalf("identity fields wrong: %+v", m)
	}
	if m.StartDate != "2018-01-01" || m.PointCount != days {
		t.Fatalf("coverage facts wrong: %+v", m)
	}
	if m.HistoryYears < 5.9 || m.HistoryYears > 6.1 {
		t.Fatalf("history years expected ~6, got %v", m.HistoryYears)
	}
	spanDays := float64(days - 1)
	wantCAGR := math.Pow(math.Pow(growth, spanDays), 365.25/spanDays) - 1
	if m.CAGR == nil || !almostEqual(*m.CAGR, wantCAGR, 1e-9) {
		t.Fatalf("CAGR expected %v, got %v", wantCAGR, m.CAGR)
	}
	// Monotonic growth: no drawdown, no Calmar, zero downside volatility.
	if m.MaxDrawdown != nil || m.Calmar != nil {
		t.Fatalf("monotonic series must have nil drawdown metrics: %+v", m)
	}
	if m.DownsideVolatility == nil || *m.DownsideVolatility != 0 {
		t.Fatalf("downside volatility expected 0, got %v", m.DownsideVolatility)
	}
	// Trailing returns: 1y = growth^365 etc.
	want1y := math.Pow(growth, 365) - 1
	if m.Return1Y == nil || !almostEqual(*m.Return1Y, want1y, 1e-9) {
		t.Fatalf("1y return expected %v, got %v", want1y, m.Return1Y)
	}
	if m.Return3Y == nil || m.Return5Y == nil {
		t.Fatalf("3y/5y returns must exist for a 6y series: %+v", m)
	}
}

func TestComputeResearchAssetMetricsDrawdownAndSharpe(t *testing.T) {
	// Rise 100 -> 200, drop to 120, recover to 220.
	value := func(i int) float64 {
		switch {
		case i < 100:
			return 100 + float64(i)
		case i < 180:
			return 199 - float64(i-99)
		default:
			return 119 + float64(i-179)
		}
	}
	points := genMarketPoints(t, "2020-01-01", 500, func(i int) float64 { return value(i) })
	m := ComputeResearchAssetMetrics("K", "forward", "nav", points, 1)
	// Peak 199 at i=99, trough at i=179: 199-80=119.
	wantDD := 119.0/199.0 - 1
	if m.MaxDrawdown == nil || !almostEqual(*m.MaxDrawdown, wantDD, 1e-9) {
		t.Fatalf("max drawdown expected %v, got %v", wantDD, m.MaxDrawdown)
	}
	if m.AnnualVolatility == nil || *m.AnnualVolatility <= 0 {
		t.Fatalf("volatility must be positive, got %v", m.AnnualVolatility)
	}
	if m.Sharpe == nil || !almostEqual(*m.Sharpe, *m.CAGR / *m.AnnualVolatility, 1e-9) {
		t.Fatalf("Sharpe expected CAGR/vol, got %v", m.Sharpe)
	}
	if m.Calmar == nil || !almostEqual(*m.Calmar, *m.CAGR/math.Abs(wantDD), 1e-9) {
		t.Fatalf("Calmar expected CAGR/|dd|, got %v", m.Calmar)
	}
	if m.DownsideVolatility == nil || *m.DownsideVolatility <= 0 {
		t.Fatalf("downside volatility must be positive, got %v", m.DownsideVolatility)
	}
}

func TestComputeResearchAssetMetricsShortSeries(t *testing.T) {
	// A single observation: coverage facts only, all metrics nil.
	points := genMarketPoints(t, "2024-01-01", 1, func(i int) float64 { return 100 })
	m := ComputeResearchAssetMetrics("K", "none", "price", points, 1)
	if m.PointCount != 1 || m.StartDate != "2024-01-01" || m.EndDate != "2024-01-01" {
		t.Fatalf("coverage facts wrong: %+v", m)
	}
	if m.CAGR != nil || m.AnnualVolatility != nil || m.Sharpe != nil ||
		m.Return1Y != nil || m.MaxDrawdown != nil {
		t.Fatalf("single point must not produce metrics: %+v", m)
	}

	// Empty series.
	empty := ComputeResearchAssetMetrics("K", "none", "price", nil, 1)
	if empty.PointCount != 0 || empty.CAGR != nil {
		t.Fatalf("empty series must be all-nil: %+v", empty)
	}

	// Short history: 1y trailing defined, 3y/5y nil.
	short := genMarketPoints(t, "2023-01-01", 400, func(i int) float64 { return 100 + float64(i)*0.1 })
	ms := ComputeResearchAssetMetrics("K", "none", "price", short, 1)
	if ms.Return1Y == nil || ms.Return3Y != nil || ms.Return5Y != nil {
		t.Fatalf("400d series should have only 1y trailing: %+v", ms)
	}
}

func TestComputeResearchAssetMetricsSkipsInvalidValues(t *testing.T) {
	points := genMarketPoints(t, "2020-01-01", 500, func(i int) float64 { return 100 + float64(i) })
	points[10].Value = 0  // invalid, skipped for math
	points[11].Value = -5 // invalid, skipped for math
	m := ComputeResearchAssetMetrics("K", "none", "price", points, 1)
	// Raw coverage still reports the full series so staleness stays stable.
	if m.PointCount != 500 {
		t.Fatalf("raw point count expected 500, got %d", m.PointCount)
	}
	if m.CAGR == nil {
		t.Fatal("metrics must still compute from the valid subset")
	}
}
