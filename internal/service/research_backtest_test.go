package service

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// --- helpers ---

// genDailySeries produces one point per calendar day starting at startDate.
func genDailySeries(t *testing.T, startDate string, days int, value func(i int) float64) []ResearchSeriesPoint {
	t.Helper()
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		t.Fatalf("parse start date: %v", err)
	}
	out := make([]ResearchSeriesPoint, 0, days)
	for i := 0; i < days; i++ {
		out = append(out, ResearchSeriesPoint{
			Date:  start.AddDate(0, 0, i).Format("2006-01-02"),
			Value: value(i),
		})
	}
	return out
}

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

func singleAssetInput(t *testing.T, points []ResearchSeriesPoint) BacktestInput {
	t.Helper()
	return BacktestInput{
		BaseCurrency:    "CNY",
		RebalancePolicy: ResearchRebalanceMonthly,
		Assets: []BacktestAssetInput{{
			AssetKey: "CN|cn_exchange_fund|sh|510300",
			Name:     "沪深300ETF",
			Currency: "CNY",
			Weight:   1,
			Points:   points,
		}},
	}
}

// --- series preparation ---

func TestPrepareSeriesSortsDedupesAndValidates(t *testing.T) {
	ps, err := prepareSeries([]ResearchSeriesPoint{
		{Date: "2020-01-03", Value: 3},
		{Date: "2020-01-01", Value: 1},
		{Date: "2020-01-01", Value: 1.5}, // duplicate date keeps last
		{Date: "2020-01-02", Value: 2},
	})
	if err != nil {
		t.Fatalf("prepareSeries: %v", err)
	}
	if len(ps.days) != 3 {
		t.Fatalf("expected 3 unique days, got %d", len(ps.days))
	}
	if v, ok := ps.valueAt(ps.days[0]); !ok || v != 1.5 {
		t.Fatalf("duplicate date should keep last value, got %v", v)
	}

	if _, err := prepareSeries([]ResearchSeriesPoint{{Date: "2020-01-01", Value: 0}}); !errors.Is(err, ErrResearchBadPoint) {
		t.Fatalf("expected ErrResearchBadPoint for zero value, got %v", err)
	}
	if _, err := prepareSeries([]ResearchSeriesPoint{{Date: "2020-01-01", Value: -1}}); !errors.Is(err, ErrResearchBadPoint) {
		t.Fatalf("expected ErrResearchBadPoint for negative value, got %v", err)
	}
	if _, err := prepareSeries([]ResearchSeriesPoint{{Date: "bad-date", Value: 1}}); err == nil {
		t.Fatal("expected error for invalid date")
	}
}

func TestSeriesForwardFillLookup(t *testing.T) {
	ps, err := prepareSeries([]ResearchSeriesPoint{
		{Date: "2020-01-01", Value: 1},
		{Date: "2020-01-04", Value: 2}, // 2-day gap (01-02, 01-03 filled)
	})
	if err != nil {
		t.Fatalf("prepareSeries: %v", err)
	}
	d0, _ := parseResearchDate("2020-01-01")
	if v, ok := ps.valueAt(d0 + 1); !ok || v != 1 {
		t.Fatalf("expected forward-filled value 1 at gap, got %v ok=%v", v, ok)
	}
	if _, ok := ps.valueAt(d0 - 1); ok {
		t.Fatal("must not fill before the first observation")
	}
	if ps.hasObservation(d0 + 1) {
		t.Fatal("filled day must not count as observation")
	}
	fillCount, maxRun := ps.fillStats(d0, d0+3)
	if fillCount != 2 || maxRun != 2 {
		t.Fatalf("expected fillCount=2 maxRun=2, got %d/%d", fillCount, maxRun)
	}
}

// --- FX routing & conversion ---

func TestResearchFXPairsFor(t *testing.T) {
	if got := ResearchFXPairsFor("CNY", "CNY"); got != nil {
		t.Fatalf("same currency should need no FX, got %v", got)
	}
	if got := ResearchFXPairsFor("USD", "CNY"); len(got) != 1 || got[0] != "USDCNY" {
		t.Fatalf("expected [USDCNY], got %v", got)
	}
	if got := ResearchFXPairsFor("USD", "HKD"); len(got) != 2 || got[0] != "USDCNY" || got[1] != "HKDCNY" {
		t.Fatalf("expected cross legs [USDCNY HKDCNY], got %v", got)
	}
	if got := ResearchFXPairsFor("CNY", "USD"); len(got) != 1 || got[0] != "USDCNY" {
		t.Fatalf("expected inverted leg [USDCNY], got %v", got)
	}
}

func TestFXConverterCrossRate(t *testing.T) {
	usdcny, _ := prepareSeries([]ResearchSeriesPoint{{Date: "2020-01-01", Value: 7.0}})
	hkdcny, _ := prepareSeries([]ResearchSeriesPoint{{Date: "2020-01-01", Value: 0.9}})
	route, need := researchFXRouteFor("USD", "HKD")
	if !need || !route.isCross {
		t.Fatalf("USD->HKD should be a cross route, got %+v need=%v", route, need)
	}
	conv := fxConverter{route: route, numer: usdcny, denom: hkdcny, need: true}
	day, _ := parseResearchDate("2020-01-02")
	rate, ok := conv.rateAt(day)
	if !ok || !almostEqual(rate, 7.0/0.9, 1e-12) {
		t.Fatalf("expected USDHKD=%v, got %v ok=%v", 7.0/0.9, rate, ok)
	}
}

// --- rebalance boundaries ---

func TestShouldRebalanceCalendarBoundaries(t *testing.T) {
	day := func(s string) int {
		d, err := parseResearchDate(s)
		if err != nil {
			t.Fatalf("parse %s: %v", s, err)
		}
		return d
	}
	last := day("2030-12-31")
	w := []float64{0.5, 0.5}
	tg := []float64{0.5, 0.5}

	cases := []struct {
		policy string
		date   string
		want   bool
	}{
		{ResearchRebalanceMonthly, "2020-01-31", true},
		{ResearchRebalanceMonthly, "2020-01-30", false},
		{ResearchRebalanceQuarterly, "2020-03-31", true},
		{ResearchRebalanceQuarterly, "2020-01-31", false},
		{ResearchRebalanceYearly, "2020-12-31", true},
		{ResearchRebalanceYearly, "2020-06-30", false},
		{ResearchRebalanceBuyHold, "2020-01-31", false},
		{ResearchRebalanceFixed, "2020-01-15", true},
	}
	for _, c := range cases {
		if got := shouldRebalance(c.policy, 0, day(c.date), last, w, tg); got != c.want {
			t.Fatalf("%s at %s: expected %v, got %v", c.policy, c.date, c.want, got)
		}
	}

	// The final day never rebalances.
	if shouldRebalance(ResearchRebalanceMonthly, 0, day("2020-01-31"), day("2020-01-31"), w, tg) {
		t.Fatal("final day must not rebalance")
	}

	// Threshold policy.
	drifted := []float64{0.55, 0.45}
	if !shouldRebalance(ResearchRebalanceThreshold, 0.05, day("2020-01-15"), last, drifted, tg) {
		t.Fatal("threshold 5% exceeded should rebalance")
	}
	exactThreshold := math.Abs(drifted[0] - tg[0])
	if !shouldRebalance(ResearchRebalanceThreshold, exactThreshold, day("2020-01-15"), last, drifted, tg) {
		t.Fatal("drift exactly equal to the threshold should rebalance")
	}
	if shouldRebalance(ResearchRebalanceThreshold, 0.10, day("2020-01-15"), last, drifted, tg) {
		t.Fatal("threshold 10% not exceeded should not rebalance")
	}
	if shouldRebalance(ResearchRebalanceThreshold, 0, day("2020-01-15"), last, drifted, tg) {
		t.Fatal("threshold 0 disables threshold rebalancing")
	}
}

// --- input validation errors ---

func TestRunBacktestWeightValidation(t *testing.T) {
	points := genDailySeries(t, "2020-01-01", 400, func(i int) float64 { return 100 + float64(i) })
	in := singleAssetInput(t, points)
	in.Assets[0].Weight = 0.95
	if _, err := RunResearchBacktest(in); !errors.Is(err, ErrResearchWeightInvalid) {
		t.Fatalf("expected ErrResearchWeightInvalid, got %v", err)
	}
	in.Assets[0].Weight = -0.1
	if _, err := RunResearchBacktest(in); !errors.Is(err, ErrResearchWeightInvalid) {
		t.Fatalf("expected ErrResearchWeightInvalid for negative weight, got %v", err)
	}
	// Serialization noise inside tolerance passes.
	in.Assets[0].Weight = 1 - 5e-7
	if _, err := RunResearchBacktest(in); err != nil {
		t.Fatalf("weight within tolerance should pass, got %v", err)
	}
}

func TestRunBacktestWindowTooShort(t *testing.T) {
	points := genDailySeries(t, "2020-01-01", 200, func(i int) float64 { return 100 + float64(i) })
	if _, err := RunResearchBacktest(singleAssetInput(t, points)); !errors.Is(err, ErrResearchWindowTooShort) {
		t.Fatalf("expected ErrResearchWindowTooShort, got %v", err)
	}
}

func TestRunBacktestNoAssets(t *testing.T) {
	if _, err := RunResearchBacktest(BacktestInput{BaseCurrency: "CNY"}); !errors.Is(err, ErrResearchNoAssets) {
		t.Fatalf("expected ErrResearchNoAssets, got %v", err)
	}
}

func TestRunBacktestFXMissing(t *testing.T) {
	points := genDailySeries(t, "2020-01-01", 400, func(_ int) float64 { return 100 })
	in := singleAssetInput(t, points)
	in.Assets[0].Currency = "USD"
	if _, err := RunResearchBacktest(in); !errors.Is(err, ErrResearchFXMissing) {
		t.Fatalf("expected ErrResearchFXMissing, got %v", err)
	}
}

func TestRunBacktestNoCommonWindow(t *testing.T) {
	a := genDailySeries(t, "2010-01-01", 400, func(_ int) float64 { return 100 })
	b := genDailySeries(t, "2020-01-01", 400, func(_ int) float64 { return 100 })
	in := BacktestInput{
		BaseCurrency:    "CNY",
		RebalancePolicy: ResearchRebalanceMonthly,
		Assets: []BacktestAssetInput{
			{AssetKey: "A", Currency: "CNY", Weight: 0.5, Points: a},
			{AssetKey: "B", Currency: "CNY", Weight: 0.5, Points: b},
		},
	}
	if _, err := RunResearchBacktest(in); !errors.Is(err, ErrResearchNoCommonWindow) {
		t.Fatalf("expected ErrResearchNoCommonWindow, got %v", err)
	}
}

// --- common window ---

func TestCommonWindowIntersection(t *testing.T) {
	a := genDailySeries(t, "2019-01-01", 900, func(i int) float64 { return 100 + float64(i)*0.01 })
	b := genDailySeries(t, "2020-06-01", 800, func(i int) float64 { return 50 + float64(i)*0.01 })
	in := BacktestInput{
		BaseCurrency:    "CNY",
		RebalancePolicy: ResearchRebalanceMonthly,
		Assets: []BacktestAssetInput{
			{AssetKey: "A", Currency: "CNY", Weight: 0.5, Points: a},
			{AssetKey: "B", Currency: "CNY", Weight: 0.5, Points: b},
		},
	}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	if res.WindowStart != "2020-06-01" {
		t.Fatalf("common start should be the later asset start, got %s", res.WindowStart)
	}
	wantEnd := a[len(a)-1].Date // a ends before b
	if res.WindowEnd != wantEnd {
		t.Fatalf("common end should be the earlier asset end %s, got %s", wantEnd, res.WindowEnd)
	}
	// Data quality flags the limiting series.
	var bLimitsStart, aLimitsEnd bool
	for _, q := range res.DataQuality.Assets {
		if q.AssetKey == "B" && q.LimitsCommonStart {
			bLimitsStart = true
		}
		if q.AssetKey == "A" && q.LimitsCommonEnd {
			aLimitsEnd = true
		}
	}
	if !bLimitsStart || !aLimitsEnd {
		t.Fatalf("expected B to limit start and A to limit end: %+v", res.DataQuality.Assets)
	}
}

func TestCommonWindowNarrowedByFX(t *testing.T) {
	asset := genDailySeries(t, "2019-01-01", 1000, func(_ int) float64 { return 100 })
	fx := genDailySeries(t, "2019-07-01", 600, func(_ int) float64 { return 7 })
	in := BacktestInput{
		BaseCurrency:    "CNY",
		RebalancePolicy: ResearchRebalanceMonthly,
		Assets: []BacktestAssetInput{
			{AssetKey: "US|us_etf||VOO", Currency: "USD", Weight: 1, Points: asset},
		},
		FX: map[string][]ResearchSeriesPoint{"USDCNY": fx},
	}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	if res.WindowStart != "2019-07-01" {
		t.Fatalf("FX availability should delay the common start, got %s", res.WindowStart)
	}
	if res.WindowEnd != fx[len(fx)-1].Date {
		t.Fatalf("FX availability should cap the common end, got %s", res.WindowEnd)
	}
}

func TestUserWindowMustBeInsideCommonRange(t *testing.T) {
	points := genDailySeries(t, "2019-01-01", 900, func(i int) float64 { return 100 + float64(i)*0.01 })
	in := singleAssetInput(t, points)
	in.WindowStart = "2019-06-01"
	in.WindowEnd = "2021-01-01"
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	if res.WindowStart != "2019-06-01" || res.WindowEnd != "2021-01-01" {
		t.Fatalf("expected custom window, got %s..%s", res.WindowStart, res.WindowEnd)
	}

	in.WindowStart = "2022-01-01" // beyond common end
	in.WindowEnd = ""
	if _, err := RunResearchBacktest(in); !errors.Is(err, ErrResearchNoCommonWindow) {
		t.Fatalf("expected ErrResearchNoCommonWindow, got %v", err)
	}
}

// --- FX conversion in the walk ---

func TestRunBacktestFXConversionCompoundsReturns(t *testing.T) {
	days := 400
	// Asset gains 0.1% daily in USD; USDCNY appreciates 0.05% daily.
	asset := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 100 * math.Pow(1.001, float64(i))
	})
	fx := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 7 * math.Pow(1.0005, float64(i))
	})
	in := BacktestInput{
		BaseCurrency:    "CNY",
		RebalancePolicy: ResearchRebalanceBuyHold,
		Assets: []BacktestAssetInput{
			{AssetKey: "US|us_etf||VOO", Currency: "USD", Weight: 1, Points: asset},
		},
		FX: map[string][]ResearchSeriesPoint{"USDCNY": fx},
	}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	wantNAV := math.Pow(1.001*1.0005, float64(days-1))
	gotNAV := res.Points[len(res.Points)-1].NAV
	if !almostEqual(gotNAV, wantNAV, 1e-9) {
		t.Fatalf("expected CNY nav %v, got %v", wantNAV, gotNAV)
	}
}

// --- rebalance behavior ---

func TestRunBacktestRebalanceModes(t *testing.T) {
	days := 400
	// A gains 0.2% daily, B stays flat: weights drift towards A.
	a := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 100 * math.Pow(1.002, float64(i))
	})
	b := genDailySeries(t, "2020-01-01", days, func(_ int) float64 { return 100 })
	baseInput := func(policy string) BacktestInput {
		return BacktestInput{
			BaseCurrency:    "CNY",
			RebalancePolicy: policy,
			Assets: []BacktestAssetInput{
				{AssetKey: "A", Name: "A", Currency: "CNY", Weight: 0.5, Points: a},
				{AssetKey: "B", Name: "B", Currency: "CNY", Weight: 0.5, Points: b},
			},
		}
	}

	hold, err := RunResearchBacktest(baseInput(ResearchRebalanceBuyHold))
	if err != nil {
		t.Fatalf("buy_hold: %v", err)
	}
	fixed, err := RunResearchBacktest(baseInput(ResearchRebalanceFixed))
	if err != nil {
		t.Fatalf("fixed: %v", err)
	}
	monthly, err := RunResearchBacktest(baseInput(ResearchRebalanceMonthly))
	if err != nil {
		t.Fatalf("monthly: %v", err)
	}

	// Fixed daily rebalancing: portfolio return is exactly 0.5*0.002 daily.
	wantFixed := math.Pow(1+0.5*0.002, float64(days-1))
	gotFixed := fixed.Points[len(fixed.Points)-1].NAV
	if !almostEqual(gotFixed, wantFixed, 1e-9) {
		t.Fatalf("fixed-mix nav expected %v, got %v", wantFixed, gotFixed)
	}

	// Buy & hold: nav = average of the two asset growth factors.
	wantHold := 0.5*math.Pow(1.002, float64(days-1)) + 0.5
	gotHold := hold.Points[len(hold.Points)-1].NAV
	if !almostEqual(gotHold, wantHold, 1e-9) {
		t.Fatalf("buy-hold nav expected %v, got %v", wantHold, gotHold)
	}

	// With one rising asset, buy & hold beats constant-mix; monthly sits
	// between them.
	gotMonthly := monthly.Points[len(monthly.Points)-1].NAV
	if !(gotHold > gotMonthly && gotMonthly > gotFixed) {
		t.Fatalf("expected hold %v > monthly %v > fixed %v", gotHold, gotMonthly, gotFixed)
	}

	// Buy & hold weights drift; last point weight of A must exceed target.
	lastHold := hold.Points[len(hold.Points)-1]
	if lastHold.Weights["A"] <= 0.5 {
		t.Fatalf("buy-hold weight should drift above 0.5, got %v", lastHold.Weights["A"])
	}

	// Monthly: weights reset to target on each month's last calendar day.
	var monthEnd BacktestPoint
	for _, p := range monthly.Points {
		if p.Date == "2020-01-31" {
			monthEnd = p
		}
	}
	if monthEnd.Date == "" || !almostEqual(monthEnd.Weights["A"], 0.5, 1e-12) {
		t.Fatalf("monthly rebalance should reset weight to 0.5 on 2020-01-31, got %+v", monthEnd.Weights)
	}
}

func TestRunBacktestThresholdRebalance(t *testing.T) {
	days := 500
	a := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 100 * math.Pow(1.003, float64(i))
	})
	b := genDailySeries(t, "2020-01-01", days, func(_ int) float64 { return 100 })
	in := BacktestInput{
		BaseCurrency:       "CNY",
		RebalancePolicy:    ResearchRebalanceThreshold,
		RebalanceThreshold: 0.05,
		Assets: []BacktestAssetInput{
			{AssetKey: "A", Currency: "CNY", Weight: 0.5, Points: a},
			{AssetKey: "B", Currency: "CNY", Weight: 0.5, Points: b},
		},
	}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	// The drifting weight must never exceed target+threshold at day close
	// after the rebalance step (drift happens intra-day, reset at close).
	sawReset := false
	for i, p := range res.Points {
		if i == 0 || i == len(res.Points)-1 {
			continue
		}
		if p.Weights["A"] > 0.5+0.05+0.004 { // one day's drift on top of the cap
			t.Fatalf("threshold rebalance failed to cap drift at %s: %v", p.Date, p.Weights["A"])
		}
		if almostEqual(p.Weights["A"], 0.5, 1e-12) && i > 0 {
			sawReset = true
		}
	}
	if !sawReset {
		t.Fatal("expected at least one threshold-triggered reset to target weights")
	}
}

// --- metrics ---

func TestRunBacktestCoreMetrics(t *testing.T) {
	days := 731 // window of 730 days
	growth := 1.0004
	points := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 100 * math.Pow(growth, float64(i))
	})
	in := singleAssetInput(t, points)
	in.RiskFreeRate = 0.02
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	s := res.Summary

	wantCum := math.Pow(growth, float64(days-1)) - 1
	if !almostEqual(s.CumulativeReturn, wantCum, 1e-9) {
		t.Fatalf("cumulative return expected %v, got %v", wantCum, s.CumulativeReturn)
	}
	wantCAGR := math.Pow(1+wantCum, 365.25/float64(days-1)) - 1
	if !almostEqual(s.CAGR, wantCAGR, 1e-9) {
		t.Fatalf("CAGR expected %v, got %v", wantCAGR, s.CAGR)
	}
	// Constant daily growth: volatility only carries float rounding noise
	// and the nav never draws down.
	if s.AnnualVolatility == nil || *s.AnnualVolatility > 1e-8 {
		t.Fatalf("expected ~zero volatility, got %v", s.AnnualVolatility)
	}
	if s.MaxDrawdown != 0 {
		t.Fatalf("expected zero drawdown, got %v", s.MaxDrawdown)
	}
	if s.Calmar != nil {
		t.Fatalf("Calmar must be unavailable at zero drawdown, got %v", *s.Calmar)
	}
	if s.PositiveMonthRatio == nil || *s.PositiveMonthRatio != 1 {
		t.Fatalf("all months positive, got %v", s.PositiveMonthRatio)
	}
}

func TestRunBacktestFlatSeriesMetricsUnavailable(t *testing.T) {
	// A perfectly flat nav: volatility is exactly 0, so Sharpe is
	// unavailable; drawdown is 0, so Calmar is unavailable.
	days := 400
	points := genDailySeries(t, "2020-01-01", days, func(_ int) float64 { return 100 })
	in := singleAssetInput(t, points)
	in.RiskFreeRate = 0.02
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	s := res.Summary
	if s.AnnualVolatility == nil || *s.AnnualVolatility != 0 {
		t.Fatalf("expected exactly zero volatility, got %v", s.AnnualVolatility)
	}
	if s.Sharpe != nil {
		t.Fatalf("Sharpe must be unavailable at zero volatility, got %v", *s.Sharpe)
	}
	if s.MaxDrawdown != 0 || s.Calmar != nil {
		t.Fatalf("flat series must have no drawdown and no Calmar, got %v/%v", s.MaxDrawdown, s.Calmar)
	}
	for _, contribution := range res.Summary.Contributions {
		if contribution.RiskContribution != nil {
			t.Fatalf("flat series risk contribution must be unavailable, got %v", *contribution.RiskContribution)
		}
	}
}

func TestRunBacktestDrawdownAndSharpe(t *testing.T) {
	// 500 days: rise to day 99 (200), fall to day 199 (100), recover to 300 by day 299, hold.
	days := 500
	value := func(i int) float64 {
		switch {
		case i < 100:
			return 100 + float64(i)
		case i < 200:
			return 199 - float64(i-99)
		case i < 300:
			return 100 + 2*float64(i-199)
		default:
			return 300
		}
	}
	points := genDailySeries(t, "2020-01-01", days, func(i int) float64 { return value(i) })
	res, err := RunResearchBacktest(singleAssetInput(t, points))
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	s := res.Summary

	// Peak 199 at day 99, trough 99 at day 199: drawdown = 99/199-1.
	wantDD := 99.0/199.0 - 1
	if !almostEqual(s.MaxDrawdown, wantDD, 1e-9) {
		t.Fatalf("max drawdown expected %v, got %v", wantDD, s.MaxDrawdown)
	}
	if s.MaxDrawdown >= 0 {
		t.Fatal("stored drawdown must be negative")
	}
	if s.Calmar == nil || !almostEqual(*s.Calmar, s.CAGR/math.Abs(wantDD), 1e-9) {
		t.Fatalf("Calmar must use abs(max_drawdown): %v", s.Calmar)
	}
	if s.AnnualVolatility == nil || *s.AnnualVolatility <= 0 {
		t.Fatalf("expected positive volatility, got %v", s.AnnualVolatility)
	}
	if s.Sharpe == nil || !almostEqual(*s.Sharpe, s.CAGR / *s.AnnualVolatility, 1e-9) {
		t.Fatalf("Sharpe expected %v, got %v", s.CAGR / *s.AnnualVolatility, s.Sharpe)
	}
	// Recovery: nav exceeds prior peak (199) on the way up to 300.
	if s.MaxDrawdownStart == "" || s.MaxDrawdownTrough == "" || s.MaxDrawdownRecovery == "" {
		t.Fatalf("expected full drawdown window facts, got %+v", s)
	}
	// Drawdown duration: from peak (day 99) to first day nav >= peak.
	// Recovery day: value(i) >= 199 -> 100+2(i-199) >= 199 -> i >= 248.5 -> 249.
	if s.MaxDrawdownDurationDays != 249-99 {
		t.Fatalf("max drawdown duration expected %d, got %d", 249-99, s.MaxDrawdownDurationDays)
	}
	// Fully recovered and flat at the end: no current drawdown.
	if s.CurrentDrawdownDays != 0 {
		t.Fatalf("expected no current drawdown, got %d", s.CurrentDrawdownDays)
	}
}

func TestRunBacktestCurrentDrawdown(t *testing.T) {
	days := 400
	// Peak at day 300, then decline without recovery.
	value := func(i int) float64 {
		if i <= 300 {
			return 100 + float64(i)
		}
		return 400 - float64(i-300)
	}
	points := genDailySeries(t, "2020-01-01", days, func(i int) float64 { return value(i) })
	res, err := RunResearchBacktest(singleAssetInput(t, points))
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	if res.Summary.CurrentDrawdownDays != days-1-300 {
		t.Fatalf("current drawdown days expected %d, got %d", days-1-300, res.Summary.CurrentDrawdownDays)
	}
	if res.Summary.MaxDrawdownRecovery != "" {
		t.Fatal("unrecovered drawdown must not carry a recovery date")
	}
}

// --- years / months ---

func TestRunBacktestYearsAndPartialFlags(t *testing.T) {
	// 2019-07-01 .. 2021-03-31.
	start, _ := time.Parse("2006-01-02", "2019-07-01")
	end, _ := time.Parse("2006-01-02", "2021-03-31")
	days := int(end.Sub(start).Hours()/24) + 1
	points := genDailySeries(t, "2019-07-01", days, func(i int) float64 {
		return 100 * math.Pow(1.0003, float64(i))
	})
	res, err := RunResearchBacktest(singleAssetInput(t, points))
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	if len(res.Years) != 3 {
		t.Fatalf("expected 3 year rows, got %d", len(res.Years))
	}
	// Newest first ordering.
	if res.Years[0].Year != 2021 || res.Years[1].Year != 2020 || res.Years[2].Year != 2019 {
		t.Fatalf("years must be ordered descending, got %+v", res.Years)
	}
	if !res.Years[0].IsPartial || res.Years[1].IsPartial || !res.Years[2].IsPartial {
		t.Fatalf("partial flags wrong: %+v", res.Years)
	}
	// Full-year 2020 return equals compounded daily growth over 366 days.
	want2020 := math.Pow(1.0003, 366) - 1
	if !almostEqual(res.Years[1].AnnualReturn, want2020, 1e-9) {
		t.Fatalf("2020 return expected %v, got %v", want2020, res.Years[1].AnnualReturn)
	}
	// Year navs chain: start of a year equals end of the previous year.
	if !almostEqual(res.Years[1].StartNAV, res.Years[2].EndNAV, 1e-12) {
		t.Fatalf("year navs must chain: %v vs %v", res.Years[1].StartNAV, res.Years[2].EndNAV)
	}
}

func TestRunBacktestMonthlyReturns(t *testing.T) {
	days := 366
	points := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 100 * math.Pow(1.001, float64(i))
	})
	res, err := RunResearchBacktest(singleAssetInput(t, points))
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	if len(res.Months) != 12 {
		t.Fatalf("expected 12 months, got %d", len(res.Months))
	}
	// February 2020 (leap): 29 daily gains.
	var feb *BacktestMonth
	for i := range res.Months {
		if res.Months[i].Year == 2020 && res.Months[i].Month == 2 {
			feb = &res.Months[i]
		}
	}
	if feb == nil {
		t.Fatal("missing 2020-02 month row")
	}
	wantFeb := math.Pow(1.001, 29) - 1
	if !almostEqual(feb.MonthlyReturn, wantFeb, 1e-9) {
		t.Fatalf("2020-02 return expected %v, got %v", wantFeb, feb.MonthlyReturn)
	}
}

// --- effective days & volatility sampling ---

func TestVolatilityIgnoresForwardFilledDays(t *testing.T) {
	// Observations only every 7th day; in-between days are forward filled.
	start, _ := time.Parse("2006-01-02", "2020-01-01")
	values := []float64{
		100, 103, 99, 105, 102, 108, 104, 111, 107, 115, 110, 118,
		114, 122, 117, 126, 121, 130, 125, 134, 129, 139, 133, 143, 137, 148, 142,
		153, 146, 158, 151, 163, 156, 168, 161, 174, 166, 180, 171, 186, 176, 192,
		181, 198, 187, 204, 192, 211, 198, 217, 203, 224, 209, 231, 215, 238, 221,
		246, 227, 253,
	}
	points := make([]ResearchSeriesPoint, 0, len(values))
	for i, v := range values {
		points = append(points, ResearchSeriesPoint{
			Date:  start.AddDate(0, 0, i*7).Format("2006-01-02"),
			Value: v,
		})
	}
	res, err := RunResearchBacktest(singleAssetInput(t, points))
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	// Effective return days = number of observations after the first.
	if res.Summary.EffectiveReturnDays != len(values)-1 {
		t.Fatalf("effective days expected %d, got %d", len(values)-1, res.Summary.EffectiveReturnDays)
	}
	// Expected volatility from weekly observation returns only.
	var rets []float64
	for i := 1; i < len(values); i++ {
		rets = append(rets, values[i]/values[i-1]-1)
	}
	want := sampleStd(rets) * math.Sqrt(252)
	if res.Summary.AnnualVolatility == nil || !almostEqual(*res.Summary.AnnualVolatility, want, 1e-9) {
		t.Fatalf("volatility expected %v, got %v", want, res.Summary.AnnualVolatility)
	}
}

func TestRunBacktestTooFewEffectiveDays(t *testing.T) {
	// Two observations one year apart: only one effective return sample.
	points := []ResearchSeriesPoint{
		{Date: "2020-01-01", Value: 100},
		{Date: "2021-06-01", Value: 120},
	}
	if _, err := RunResearchBacktest(singleAssetInput(t, points)); !errors.Is(err, ErrResearchNoEffectiveDays) {
		t.Fatalf("expected ErrResearchNoEffectiveDays, got %v", err)
	}
}

func TestRunBacktestTailRiskMatchesPureFunction(t *testing.T) {
	points := genDailySeries(t, "2020-01-01", 500, func(i int) float64 {
		return 100 * math.Pow(1.0002+0.0003*math.Sin(float64(i)/13), float64(i))
	})
	in := singleAssetInput(t, points)
	in.TailRisk = &TailRiskSpec{Confidence: 0.95, HorizonDays: 20}
	result, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatal(err)
	}
	returns := make([]float64, 0, len(result.Points)-1)
	for i := 1; i < len(result.Points); i++ {
		returns = append(returns, result.Points[i].PeriodReturn)
	}
	want, err := ComputeEmpiricalCVaR(returns, *in.TailRisk)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.TailRisk == nil || *result.Summary.TailRisk != want {
		t.Fatalf("summary tail risk = %+v, want %+v", result.Summary.TailRisk, want)
	}
	repeated, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(result.Summary)
	secondJSON, _ := json.Marshal(repeated.Summary)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("same input produced different summary JSON:\n%s\n%s", firstJSON, secondJSON)
	}
}

func TestRunBacktestZeroWeightAssetDoesNotChangeEffectiveMetrics(t *testing.T) {
	start, _ := time.Parse("2006-01-02", "2020-01-01")
	positive, zero := make([]ResearchSeriesPoint, 0, 251), make([]ResearchSeriesPoint, 0, 252)
	for i := 0; i <= 500; i++ {
		point := ResearchSeriesPoint{
			Date: start.AddDate(0, 0, i).Format("2006-01-02"), Value: 100 + float64(i)/10,
		}
		if i%2 == 0 {
			positive = append(positive, point)
		}
		if i >= 100 && i <= 400 && i%2 == 1 {
			zero = append(zero, point)
		}
	}
	spec := &TailRiskSpec{Confidence: 0.95, HorizonDays: 20}
	base := singleAssetInput(t, positive)
	base.TailRisk = spec
	withZero := base
	withZero.Assets = append(append([]BacktestAssetInput(nil), base.Assets...), BacktestAssetInput{
		AssetKey: "ZERO", Name: "Zero", Currency: "USD", Weight: 0, Points: zero,
	})
	withZero.FX = map[string][]ResearchSeriesPoint{"USDCNY": zero}
	one, err := RunResearchBacktest(base)
	if err != nil {
		t.Fatal(err)
	}
	two, err := RunResearchBacktest(withZero)
	if err != nil {
		t.Fatal(err)
	}
	if one.Summary.EffectiveReturnDays != len(positive)-1 ||
		one.Summary.EffectiveReturnDays != two.Summary.EffectiveReturnDays ||
		one.WindowStart != two.WindowStart || one.WindowEnd != two.WindowEnd ||
		!almostEqual(one.Summary.CumulativeReturn, two.Summary.CumulativeReturn, 1e-15) ||
		!almostEqual(one.Summary.CAGR, two.Summary.CAGR, 1e-15) ||
		!almostEqual(one.Summary.MaxDrawdown, two.Summary.MaxDrawdown, 1e-15) ||
		!almostEqual(*one.Summary.AnnualVolatility, *two.Summary.AnnualVolatility, 1e-15) ||
		one.Summary.TailRisk == nil || two.Summary.TailRisk == nil ||
		*one.Summary.TailRisk != *two.Summary.TailRisk {
		t.Fatalf("zero-weight asset changed effective metrics: one=%+v two=%+v", one.Summary, two.Summary)
	}
}

func TestRunBacktestFrozenEffectiveCalendarIsIdenticalAcrossWeights(t *testing.T) {
	start, _ := time.Parse("2006-01-02", "2020-01-01")
	even, odd := make([]ResearchSeriesPoint, 0, 301), make([]ResearchSeriesPoint, 0, 300)
	for i := 0; i <= 600; i++ {
		point := ResearchSeriesPoint{
			Date: start.AddDate(0, 0, i).Format("2006-01-02"), Value: 100 + float64(i)/10,
		}
		if i%2 == 0 {
			even = append(even, point)
		}
		if i%2 == 1 {
			odd = append(odd, point)
		}
	}
	base := BacktestInput{
		BaseCurrency: "CNY", RebalancePolicy: ResearchRebalanceBuyHold,
		WindowStart:             start.AddDate(0, 0, 1).Format("2006-01-02"),
		WindowEnd:               start.AddDate(0, 0, 599).Format("2006-01-02"),
		TailRisk:                &TailRiskSpec{Confidence: 0.95, HorizonDays: 20},
		FreezeEffectiveCalendar: true,
		Assets: []BacktestAssetInput{
			{AssetKey: "EVEN", Name: "Even", Currency: "CNY", Weight: 1, Points: even},
			{AssetKey: "ODD", Name: "Odd", Currency: "CNY", Weight: 0, Points: odd},
		},
	}
	one, err := RunResearchBacktest(base)
	if err != nil {
		t.Fatal(err)
	}
	mixed := base
	mixed.Assets = append([]BacktestAssetInput(nil), base.Assets...)
	mixed.Assets[0].Weight, mixed.Assets[1].Weight = 0.8, 0.2
	two, err := RunResearchBacktest(mixed)
	if err != nil {
		t.Fatal(err)
	}
	if one.Summary.EffectiveReturnDays != two.Summary.EffectiveReturnDays ||
		one.Summary.TailRisk == nil || two.Summary.TailRisk == nil ||
		one.Summary.TailRisk.ScenarioCount != two.Summary.TailRisk.ScenarioCount {
		t.Fatalf("frozen calendar changed across weights: one=%+v two=%+v", one.Summary, two.Summary)
	}
	if one.Summary.EffectiveReturnDays != 598 || one.Summary.TailRisk.ScenarioCount != 579 {
		t.Fatalf("unexpected frozen sample counts: %+v", one.Summary)
	}
}

func TestRunBacktestBaseCashUsesExplicitWeekdayWindow(t *testing.T) {
	in := BacktestInput{
		BaseCurrency: "CNY", RebalancePolicy: ResearchRebalanceBuyHold,
		WindowStart: "2020-01-01", WindowEnd: "2021-06-01",
		TailRisk: &TailRiskSpec{Confidence: 0.95, HorizonDays: 20},
		Assets:   []BacktestAssetInput{{AssetKey: "CASH", Name: "Cash", Currency: "CNY", Weight: 1, IsCash: true}},
	}
	result, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.TailRisk == nil || result.Summary.TailRisk.VaRLoss != 0 ||
		result.Summary.TailRisk.CVaRLoss != 0 || result.Summary.TailRisk.WorstLoss != 0 {
		t.Fatalf("unexpected all-cash tail risk: %+v", result.Summary.TailRisk)
	}
	in.WindowEnd = ""
	if _, err := RunResearchBacktest(in); !errors.Is(err, ErrResearchNoCommonWindow) {
		t.Fatalf("all-cash run without complete explicit window error = %v", err)
	}
}

func TestRunBacktestForeignCashFXObservationsAreEffective(t *testing.T) {
	start, _ := time.Parse("2006-01-02", "2020-01-01")
	fx := make([]ResearchSeriesPoint, 0, 260)
	for i := 0; i < 520; i += 2 {
		fx = append(fx, ResearchSeriesPoint{
			Date:  start.AddDate(0, 0, i).Format("2006-01-02"),
			Value: 7 + 0.1*math.Sin(float64(i)/17),
		})
	}
	result, err := RunResearchBacktest(BacktestInput{
		BaseCurrency: "CNY", RebalancePolicy: ResearchRebalanceBuyHold,
		TailRisk: &TailRiskSpec{Confidence: 0.95, HorizonDays: 20},
		Assets:   []BacktestAssetInput{{AssetKey: "USD_CASH", Currency: "USD", Weight: 1, IsCash: true}},
		FX:       map[string][]ResearchSeriesPoint{"USDCNY": fx},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.EffectiveReturnDays != len(fx)-1 || result.Summary.TailRisk == nil ||
		result.Summary.TailRisk.ScenarioCount != len(fx)-20 {
		t.Fatalf("FX observations were not used as effective days: %+v", result.Summary)
	}
}

// --- cash assets ---

func TestRunBacktestCashAsset(t *testing.T) {
	days := 400
	equity := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 100 * math.Pow(1.001, float64(i))
	})
	in := BacktestInput{
		BaseCurrency:    "CNY",
		RebalancePolicy: ResearchRebalanceFixed,
		Assets: []BacktestAssetInput{
			{AssetKey: "A", Currency: "CNY", Weight: 0.5, Points: equity},
			{AssetKey: "SYS|cash||CNY", Currency: "CNY", Weight: 0.5, IsCash: true},
		},
	}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	// Constant-mix with 0-return cash: daily portfolio return = 0.5*0.001.
	want := math.Pow(1+0.5*0.001, float64(days-1))
	got := res.Points[len(res.Points)-1].NAV
	if !almostEqual(got, want, 1e-9) {
		t.Fatalf("cash-mix nav expected %v, got %v", want, got)
	}
	// Cash never bounds the common window.
	if res.WindowStart != "2020-01-01" {
		t.Fatalf("cash asset must not shift the window, got %s", res.WindowStart)
	}
}

func TestRunBacktestForeignCashNeedsFX(t *testing.T) {
	days := 400
	equity := genDailySeries(t, "2020-01-01", days, func(_ int) float64 { return 100 })
	in := BacktestInput{
		BaseCurrency:    "CNY",
		RebalancePolicy: ResearchRebalanceMonthly,
		Assets: []BacktestAssetInput{
			{AssetKey: "A", Currency: "CNY", Weight: 0.5, Points: equity},
			{AssetKey: "SYS|cash||USD", Currency: "USD", Weight: 0.5, IsCash: true},
		},
	}
	if _, err := RunResearchBacktest(in); !errors.Is(err, ErrResearchFXMissing) {
		t.Fatalf("foreign cash without FX must fail, got %v", err)
	}
	fx := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 7 * math.Pow(1.0002, float64(i))
	})
	in.FX = map[string][]ResearchSeriesPoint{"USDCNY": fx}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest with FX: %v", err)
	}
	// USD cash appreciates with the FX rate under buy&hold-free fixed mix.
	last := res.Points[len(res.Points)-1]
	if last.NAV <= 1 {
		t.Fatalf("USD cash appreciation should lift nav above 1, got %v", last.NAV)
	}
}

// --- contributions & correlations ---

func TestRunBacktestContributions(t *testing.T) {
	days := 400
	// A oscillates (non-degenerate variance), B stays flat.
	aValue := func(i int) float64 { return 100 * (1 + 0.1*math.Sin(float64(i)/5)) }
	a := genDailySeries(t, "2020-01-01", days, aValue)
	b := genDailySeries(t, "2020-01-01", days, func(_ int) float64 { return 100 })
	in := BacktestInput{
		BaseCurrency:    "CNY",
		RebalancePolicy: ResearchRebalanceFixed,
		Assets: []BacktestAssetInput{
			{AssetKey: "A", Name: "A", Currency: "CNY", Weight: 0.6, Points: a},
			{AssetKey: "B", Name: "B", Currency: "CNY", Weight: 0.4, Points: b},
		},
	}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	if len(res.Summary.Contributions) != 2 {
		t.Fatalf("expected 2 contribution rows, got %d", len(res.Summary.Contributions))
	}
	var contribA, contribB BacktestAssetContribution
	for _, c := range res.Summary.Contributions {
		switch c.AssetKey {
		case "A":
			contribA = c
		case "B":
			contribB = c
		}
	}
	// Flat asset contributes nothing; the moving asset carries everything.
	if contribB.CumulativeContribution != 0 {
		t.Fatalf("flat asset contribution expected 0, got %v", contribB.CumulativeContribution)
	}
	// Currency contributions are additive: their sum exactly reconstructs the
	// portfolio cumulative return, including compounding.
	cumulativeSum := contribA.CumulativeContribution + contribB.CumulativeContribution
	if !almostEqual(cumulativeSum, res.Summary.CumulativeReturn, 1e-12) {
		t.Fatalf("cumulative contributions %v do not reconstruct return %v", cumulativeSum, res.Summary.CumulativeReturn)
	}
	// Drawdown contributions reconstruct the maximum drawdown over the same
	// peak-to-trough interval.
	drawdownSum := contribA.DrawdownContribution + contribB.DrawdownContribution
	if !almostEqual(drawdownSum, res.Summary.MaxDrawdown, 1e-12) {
		t.Fatalf("drawdown contributions %v do not reconstruct max drawdown %v", drawdownSum, res.Summary.MaxDrawdown)
	}
	// End weights under fixed policy equal targets.
	if !almostEqual(contribA.EndWeight, 0.6, 1e-12) || !almostEqual(contribB.EndWeight, 0.4, 1e-12) {
		t.Fatalf("fixed policy end weights should equal targets, got %v/%v",
			contribA.EndWeight, contribB.EndWeight)
	}
	// Risk decomposition under a fixed mix: rp = 0.6*rA exactly, so A's
	// risk contribution is 1 and B's is 0 regardless of A's distribution.
	if contribB.RiskContribution == nil || !almostEqual(*contribB.RiskContribution, 0, 1e-9) {
		t.Fatalf("flat asset risk contribution expected 0, got %v", contribB.RiskContribution)
	}
	if contribA.RiskContribution == nil || !almostEqual(*contribA.RiskContribution, 1, 1e-9) {
		t.Fatalf("risky asset risk contribution expected 1, got %v", contribA.RiskContribution)
	}
	if !almostEqual(*contribA.RiskContribution+*contribB.RiskContribution, 1, 1e-12) {
		t.Fatalf("risk contributions must sum to 1, got %v and %v", *contribA.RiskContribution, *contribB.RiskContribution)
	}
}

func TestRunBacktestCorrelationMatrix(t *testing.T) {
	days := 400
	// B moves exactly opposite to A around a trend.
	a := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 100 + 10*math.Sin(float64(i)/5)
	})
	b := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 100 - 10*math.Sin(float64(i)/5)
	})
	in := BacktestInput{
		BaseCurrency:    "CNY",
		RebalancePolicy: ResearchRebalanceMonthly,
		Assets: []BacktestAssetInput{
			{AssetKey: "A", Name: "A", Currency: "CNY", Weight: 0.5, Points: a},
			{AssetKey: "B", Name: "B", Currency: "CNY", Weight: 0.5, Points: b},
		},
	}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	corr := res.Summary.Correlations
	if corr == nil || len(corr.Matrix) != 2 {
		t.Fatalf("expected 2x2 correlation matrix, got %+v", corr)
	}
	if corr.Matrix[0][0] == nil || !almostEqual(*corr.Matrix[0][0], 1, 1e-9) {
		t.Fatalf("self correlation expected 1, got %v", corr.Matrix[0][0])
	}
	if corr.Matrix[0][1] == nil || *corr.Matrix[0][1] > -0.9 {
		t.Fatalf("opposite series should be strongly negative, got %v", corr.Matrix[0][1])
	}
	if *corr.Matrix[0][1] != *corr.Matrix[1][0] {
		t.Fatal("correlation matrix must be symmetric")
	}
}

// --- benchmark ---

func TestRunBacktestBenchmarkOverlay(t *testing.T) {
	days := 400
	asset := genDailySeries(t, "2020-01-01", days, func(i int) float64 {
		return 100 * math.Pow(1.001, float64(i))
	})
	bench := genDailySeries(t, "2019-01-01", days+365, func(i int) float64 {
		return 50 * math.Pow(1.0005, float64(i))
	})
	in := singleAssetInput(t, asset)
	in.Benchmark = &BacktestBenchmarkInput{
		AssetKey: "CN|cn_exchange_fund|sh|510300",
		Name:     "基准",
		Currency: "CNY",
		Points:   bench,
	}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	first := res.Points[0]
	if first.BenchmarkNAV == nil || !almostEqual(*first.BenchmarkNAV, 1, 1e-12) {
		t.Fatalf("benchmark nav must be normalized to 1 at window start, got %v", first.BenchmarkNAV)
	}
	last := res.Points[len(res.Points)-1]
	wantBench := math.Pow(1.0005, float64(days-1))
	if last.BenchmarkNAV == nil || !almostEqual(*last.BenchmarkNAV, wantBench, 1e-9) {
		t.Fatalf("benchmark end nav expected %v, got %v", wantBench, last.BenchmarkNAV)
	}
	if res.Summary.Benchmark == nil ||
		!almostEqual(res.Summary.Benchmark.CumulativeReturn, wantBench-1, 1e-9) {
		t.Fatalf("benchmark summary wrong: %+v", res.Summary.Benchmark)
	}
	if res.DataQuality.Benchmark == nil {
		t.Fatal("benchmark data quality missing")
	}
}

func TestRunBacktestBenchmarkMustCoverPortfolioWindow(t *testing.T) {
	asset := genDailySeries(t, "2020-01-01", 400, func(i int) float64 { return 100 + float64(i) })
	tests := []struct {
		name  string
		bench []ResearchSeriesPoint
	}{
		{name: "starts late", bench: genDailySeries(t, "2020-01-02", 399, func(i int) float64 { return 100 + float64(i) })},
		{name: "ends early", bench: genDailySeries(t, "2020-01-01", 399, func(i int) float64 { return 100 + float64(i) })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := singleAssetInput(t, asset)
			in.Benchmark = &BacktestBenchmarkInput{AssetKey: "BENCH", Currency: "CNY", Points: tt.bench}
			if _, err := RunResearchBacktest(in); !errors.Is(err, ErrResearchNoCommonWindow) {
				t.Fatalf("expected uncovered benchmark to fail, got %v", err)
			}
		})
	}
}

func TestRunBacktestCashBenchmark(t *testing.T) {
	asset := genDailySeries(t, "2020-01-01", 400, func(i int) float64 { return 100 + float64(i) })
	in := singleAssetInput(t, asset)
	in.Benchmark = &BacktestBenchmarkInput{
		AssetKey: "SYS|cash||CNY", Name: "人民币现金", Currency: "CNY", IsCash: true,
	}
	res, err := RunResearchBacktest(in)
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	if res.Summary.Benchmark == nil || res.Summary.Benchmark.CumulativeReturn != 0 {
		t.Fatalf("cash benchmark should have zero return, got %+v", res.Summary.Benchmark)
	}
	if res.DataQuality.Benchmark == nil || !res.DataQuality.Benchmark.IsCash {
		t.Fatalf("cash benchmark quality facts missing: %+v", res.DataQuality.Benchmark)
	}
}

func TestRunBacktestBenchmarkGapExceeded(t *testing.T) {
	asset := genDailySeries(t, "2020-01-01", 400, func(i int) float64 { return 100 + float64(i) })
	benchmark := genDailySeries(t, "2020-01-01", 400, func(i int) float64 { return 100 + float64(i) })
	benchmark = append(benchmark[:200], benchmark[221:]...)
	in := singleAssetInput(t, asset)
	in.Benchmark = &BacktestBenchmarkInput{
		AssetKey: "BENCH", Currency: "CNY", MaxFillGapDays: 7, Points: benchmark,
	}
	if _, err := RunResearchBacktest(in); !errors.Is(err, ErrResearchNoCommonWindow) {
		t.Fatalf("expected excessive benchmark gap to fail, got %v", err)
	}
}

// --- data quality / fill facts ---

func TestRunBacktestDataQualityFillFacts(t *testing.T) {
	// Weekday-only series: weekends are forward filled (2-day runs).
	start, _ := time.Parse("2006-01-02", "2020-01-06") // Monday
	var points []ResearchSeriesPoint
	day := start
	for len(points) < 300 {
		if wd := day.Weekday(); wd != time.Saturday && wd != time.Sunday {
			points = append(points, ResearchSeriesPoint{
				Date:  day.Format("2006-01-02"),
				Value: 100 + float64(len(points)),
			})
		}
		day = day.AddDate(0, 0, 1)
	}
	res, err := RunResearchBacktest(singleAssetInput(t, points))
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}
	q := res.DataQuality.Assets[0]
	if q.MaxFillGapDays != 2 {
		t.Fatalf("weekend fill run expected 2, got %d", q.MaxFillGapDays)
	}
	if q.FillGapExceeded {
		t.Fatal("weekend gaps are within tolerance")
	}
	if q.RawPointCount != len(points) {
		t.Fatalf("raw point count expected %d, got %d", len(points), q.RawPointCount)
	}
	if res.DataQuality.CommonStart == "" || res.DataQuality.CommonEnd == "" {
		t.Fatal("common window facts missing")
	}
}

func TestRunBacktestDataQualityJSONUsesEmptyArrays(t *testing.T) {
	points := genDailySeries(t, "2020-01-01", 400, func(i int) float64 {
		return 100 + float64(i)
	})
	res, err := RunResearchBacktest(singleAssetInput(t, points))
	if err != nil {
		t.Fatalf("RunResearchBacktest: %v", err)
	}

	b, err := json.Marshal(res.DataQuality)
	if err != nil {
		t.Fatalf("marshal data quality: %v", err)
	}
	got := string(b)
	if strings.Contains(got, `"fx":null`) {
		t.Fatalf("fx must encode as an empty array, got %s", got)
	}
	if !strings.Contains(got, `"fx":[]`) {
		t.Fatalf("expected empty fx array, got %s", got)
	}
	if strings.Contains(got, `"assets":null`) {
		t.Fatalf("assets must encode as an array, got %s", got)
	}
	if !strings.Contains(got, `"assets":[`) {
		t.Fatalf("expected assets array, got %s", got)
	}
}

// --- statistics helpers ---

func TestSampleStdAndCorrelationHelpers(t *testing.T) {
	xs := []float64{1, 2, 3, 4}
	// Sample variance of 1..4 = 5/3.
	if !almostEqual(sampleVariance(xs), 5.0/3.0, 1e-12) {
		t.Fatalf("sample variance expected %v, got %v", 5.0/3.0, sampleVariance(xs))
	}
	if !almostEqual(sampleStd(xs), math.Sqrt(5.0/3.0), 1e-12) {
		t.Fatalf("sample std wrong: %v", sampleStd(xs))
	}
	if _, ok := pearsonCorrelation([]float64{1, 1, 1}, []float64{1, 2, 3}); ok {
		t.Fatal("correlation with zero-variance series must be undefined")
	}
	corr, ok := pearsonCorrelation([]float64{1, 2, 3}, []float64{2, 4, 6})
	if !ok || !almostEqual(corr, 1, 1e-12) {
		t.Fatalf("perfectly correlated expected 1, got %v ok=%v", corr, ok)
	}
}
