package service

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// research_backtest.go implements the pure computation core of research
// portfolio backtests (td/099 §3.6/§6): calendar-day valuation series with
// bounded forward fill, FX conversion into the collection base currency,
// common usable window, rebalance policies and all summary metrics.
//
// The engine never touches the database: the service layer freezes inputs
// and the job runner feeds them here.

// ResearchEngineVersion participates in input_hash so engine changes never
// silently reuse old runs.
const ResearchEngineVersion = "research_backtest_v2"

// Rebalance policies (td/099 §3.6).
const (
	ResearchRebalanceMonthly   = "monthly"
	ResearchRebalanceQuarterly = "quarterly"
	ResearchRebalanceYearly    = "yearly"
	ResearchRebalanceBuyHold   = "buy_hold"
	ResearchRebalanceFixed     = "fixed"
	ResearchRebalanceThreshold = "threshold"
)

// Start policies.
const (
	ResearchStartPolicyCommon = "common_intersection"
	ResearchStartPolicyCustom = "custom_range"
)

// Data-quality defaults (td/099 §3.6/§7).
//
// Forward-fill tolerance is expressed in natural days of consecutive filled
// values between two real observations. Exceeding the tolerance is recorded
// as a data-quality fact; readiness turns FX gaps into blockers and asset
// gaps into warnings (§7 lists only FX gaps as blocking).
const (
	// ResearchWeightTolerance absorbs float serialization noise when
	// validating that enabled weights sum to 100%.
	ResearchWeightTolerance = 1e-6
	// researchFillGapDefaultDays is the default max forward-fill run for
	// exchange-traded assets.
	researchFillGapDefaultDays = 7
	// researchFillGapMutualFundDays relaxes the tolerance for mutual funds.
	researchFillGapMutualFundDays = 14
	// researchFXFillGapDays tolerates FX publication holidays (CNY fixing
	// pauses over Spring Festival can span ~10 natural days).
	researchFXFillGapDays = 14
	// researchMinWindowDays blocks runs shorter than one year.
	researchMinWindowDays = 365
	// researchShortWindowDays warns for windows shorter than three years.
	researchShortWindowDays = 3 * 365
)

// Engine errors. The service maps them onto stable API error codes.
var (
	ErrResearchNoAssets        = errors.New("research backtest requires at least one enabled asset")
	ErrResearchNoCommonWindow  = errors.New("research backtest has no common usable window")
	ErrResearchWindowTooShort  = errors.New("research backtest window is shorter than the minimum length")
	ErrResearchFXMissing       = errors.New("research backtest is missing FX history for a foreign-currency asset")
	ErrResearchBadPoint        = errors.New("research backtest input contains a non-positive point value")
	ErrResearchWeightInvalid   = errors.New("research backtest weights do not sum to 100%")
	ErrResearchNoEffectiveDays = errors.New(
		"research backtest window has fewer than 2 effective valuation days")
)

// ResearchSeriesPoint is one raw (date, value) observation.
type ResearchSeriesPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// BacktestAssetInput is one enabled collection item with its raw local
// history, already resolved to a single (adjust_policy, point_type) series.
type BacktestAssetInput struct {
	AssetKey string
	Name     string
	Currency string
	// Weight is the target weight as a fraction (enabled weights sum to 1
	// within ResearchWeightTolerance).
	Weight float64
	// IsCash marks system cash assets: constant value 1.0 in their own
	// currency with no history requirement.
	IsCash bool
	// MaxFillGapDays is the forward-fill tolerance for this asset.
	MaxFillGapDays int
	Points         []ResearchSeriesPoint
}

// BacktestBenchmarkInput is the optional comparison series.
type BacktestBenchmarkInput struct {
	AssetKey       string
	Name           string
	Currency       string
	IsCash         bool
	MaxFillGapDays int
	Points         []ResearchSeriesPoint
}

// BacktestInput is the full engine input.
type BacktestInput struct {
	BaseCurrency        string
	RebalancePolicy     string
	RebalanceThreshold  float64
	RiskFreeRate        float64
	TransactionCostRate float64
	// WindowStart/WindowEnd optionally narrow the window; empty means the
	// full common intersection.
	WindowStart string
	WindowEnd   string
	Assets      []BacktestAssetInput
	// FX maps pair code (e.g. USDCNY) to raw fixing points.
	FX map[string][]ResearchSeriesPoint
	// FXMaxFillGapDays defaults to researchFXFillGapDays when 0.
	FXMaxFillGapDays int
	Benchmark        *BacktestBenchmarkInput
}

// BacktestPoint is one calendar-day output observation.
type BacktestPoint struct {
	Date             string             `json:"date"`
	NAV              float64            `json:"nav"`
	CumulativeReturn float64            `json:"cumulative_return"`
	PeriodReturn     float64            `json:"period_return"`
	Drawdown         float64            `json:"drawdown"`
	BenchmarkNAV     *float64           `json:"benchmark_nav,omitempty"`
	BenchmarkReturn  *float64           `json:"benchmark_return,omitempty"`
	Weights          map[string]float64 `json:"weights"`
	Contributions    map[string]float64 `json:"contributions"`
}

// BacktestYear is one natural-year row.
type BacktestYear struct {
	Year         int     `json:"year"`
	AnnualReturn float64 `json:"annual_return"`
	Volatility   float64 `json:"volatility"`
	MaxDrawdown  float64 `json:"max_drawdown"`
	StartNAV     float64 `json:"start_nav"`
	EndNAV       float64 `json:"end_nav"`
	IsPartial    bool    `json:"is_partial"`
}

// BacktestMonth is one calendar-month return.
type BacktestMonth struct {
	Year          int     `json:"year"`
	Month         int     `json:"month"`
	MonthlyReturn float64 `json:"monthly_return"`
}

// YearExtreme labels the best/worst year.
type YearExtreme struct {
	Year   int     `json:"year"`
	Return float64 `json:"return"`
}

// MonthExtreme labels the best/worst month.
type MonthExtreme struct {
	Year   int     `json:"year"`
	Month  int     `json:"month"`
	Return float64 `json:"return"`
}

// BacktestAssetContribution explains one asset's linked contribution to the
// portfolio's cumulative return, risk, and maximum drawdown.
type BacktestAssetContribution struct {
	AssetKey               string   `json:"asset_key"`
	Name                   string   `json:"name"`
	TargetWeight           float64  `json:"target_weight"`
	EndWeight              float64  `json:"end_weight"`
	CumulativeContribution float64  `json:"cumulative_contribution"`
	RiskContribution       *float64 `json:"risk_contribution,omitempty"`
	DrawdownContribution   float64  `json:"drawdown_contribution"`
}

// BacktestCorrelations is the pairwise correlation matrix over effective-day
// returns. Nil cells mean "undefined" (zero-variance series).
type BacktestCorrelations struct {
	AssetKeys []string     `json:"asset_keys"`
	Names     []string     `json:"names"`
	Matrix    [][]*float64 `json:"matrix"`
}

// BacktestBenchmarkSummary summarizes the optional benchmark over the window.
type BacktestBenchmarkSummary struct {
	AssetKey         string  `json:"asset_key"`
	Name             string  `json:"name"`
	CumulativeReturn float64 `json:"cumulative_return"`
	CAGR             float64 `json:"cagr"`
	MaxDrawdown      float64 `json:"max_drawdown"`
}

// BacktestSummary is the run overview block (td/099 §3.7). Metrics that can
// be undefined are pointers and are never written as 0.
type BacktestSummary struct {
	CumulativeReturn        float64                     `json:"cumulative_return"`
	CAGR                    float64                     `json:"cagr"`
	AnnualVolatility        *float64                    `json:"annual_volatility,omitempty"`
	MaxDrawdown             float64                     `json:"max_drawdown"`
	Sharpe                  *float64                    `json:"sharpe,omitempty"`
	Calmar                  *float64                    `json:"calmar,omitempty"`
	BestYear                *YearExtreme                `json:"best_year,omitempty"`
	WorstYear               *YearExtreme                `json:"worst_year,omitempty"`
	BestMonth               *MonthExtreme               `json:"best_month,omitempty"`
	WorstMonth              *MonthExtreme               `json:"worst_month,omitempty"`
	PositiveMonthRatio      *float64                    `json:"positive_month_ratio,omitempty"`
	CurrentDrawdownDays     int                         `json:"current_drawdown_days"`
	MaxDrawdownDurationDays int                         `json:"max_drawdown_duration_days"`
	MaxDrawdownStart        string                      `json:"max_drawdown_start,omitempty"`
	MaxDrawdownTrough       string                      `json:"max_drawdown_trough,omitempty"`
	MaxDrawdownRecovery     string                      `json:"max_drawdown_recovery,omitempty"`
	EffectiveReturnDays     int                         `json:"effective_return_days"`
	RiskFreeRate            float64                     `json:"risk_free_rate"`
	Contributions           []BacktestAssetContribution `json:"contributions"`
	Correlations            *BacktestCorrelations       `json:"correlations,omitempty"`
	Benchmark               *BacktestBenchmarkSummary   `json:"benchmark,omitempty"`
}

// BacktestSeriesQuality reports data-quality facts for one input series.
type BacktestSeriesQuality struct {
	AssetKey        string `json:"asset_key,omitempty"`
	Name            string `json:"name,omitempty"`
	Pair            string `json:"pair,omitempty"`
	Currency        string `json:"currency,omitempty"`
	FXPair          string `json:"fx_pair,omitempty"`
	IsCash          bool   `json:"is_cash,omitempty"`
	RawStart        string `json:"raw_start,omitempty"`
	RawEnd          string `json:"raw_end,omitempty"`
	RawPointCount   int    `json:"raw_point_count"`
	UsableStart     string `json:"usable_start,omitempty"`
	UsableEnd       string `json:"usable_end,omitempty"`
	FillCount       int    `json:"fill_count"`
	MaxFillGapDays  int    `json:"max_fill_gap_days"`
	FillTolerance   int    `json:"fill_tolerance_days"`
	FillGapExceeded bool   `json:"fill_gap_exceeded"`
	// LimitsCommonStart / LimitsCommonEnd flag the series that determined the
	// common window bounds so the UI can explain "why this window".
	LimitsCommonStart bool `json:"limits_common_start,omitempty"`
	LimitsCommonEnd   bool `json:"limits_common_end,omitempty"`
}

// BacktestDataQuality explains the window derivation and fill behavior.
type BacktestDataQuality struct {
	CommonStartPolicy  string                  `json:"common_start_policy"`
	CommonEndPolicy    string                  `json:"common_end_policy"`
	ForwardFillDaysMax int                     `json:"forward_fill_days_max"`
	CommonStart        string                  `json:"common_start"`
	CommonEnd          string                  `json:"common_end"`
	WindowStart        string                  `json:"window_start"`
	WindowEnd          string                  `json:"window_end"`
	Assets             []BacktestSeriesQuality `json:"assets"`
	FX                 []BacktestSeriesQuality `json:"fx"`
	Benchmark          *BacktestSeriesQuality  `json:"benchmark,omitempty"`
}

// BacktestResult bundles the full engine output.
type BacktestResult struct {
	WindowStart string
	WindowEnd   string
	Points      []BacktestPoint
	Years       []BacktestYear
	Months      []BacktestMonth
	Summary     BacktestSummary
	DataQuality BacktestDataQuality
}

// --- date helpers ---

func parseResearchDate(s string) (int, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0, fmt.Errorf("parse date %q: %w", s, err)
	}
	return int(t.Unix() / 86400), nil
}

func researchDayToDate(day int) string {
	return time.Unix(int64(day)*86400, 0).UTC().Format("2006-01-02")
}

func researchDayYMD(day int) (int, time.Month, int) {
	return time.Unix(int64(day)*86400, 0).UTC().Date()
}

// --- series preparation ---

// preparedSeries is one raw series indexed by day with forward-fill lookup.
type preparedSeries struct {
	days   []int
	values []float64
}

func prepareSeries(points []ResearchSeriesPoint) (preparedSeries, error) {
	ps := preparedSeries{
		days:   make([]int, 0, len(points)),
		values: make([]float64, 0, len(points)),
	}
	sorted := make([]ResearchSeriesPoint, len(points))
	copy(sorted, points)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date < sorted[j].Date })
	for _, p := range sorted {
		if p.Value <= 0 || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			return ps, fmt.Errorf("%w: %s=%v", ErrResearchBadPoint, p.Date, p.Value)
		}
		day, err := parseResearchDate(p.Date)
		if err != nil {
			return ps, err
		}
		if n := len(ps.days); n > 0 && ps.days[n-1] == day {
			// Duplicate date: keep the last value.
			ps.values[n-1] = p.Value
			continue
		}
		ps.days = append(ps.days, day)
		ps.values = append(ps.values, p.Value)
	}
	return ps, nil
}

func (s preparedSeries) empty() bool { return len(s.days) == 0 }

func (s preparedSeries) firstDay() int { return s.days[0] }
func (s preparedSeries) lastDay() int  { return s.days[len(s.days)-1] }

// valueAt returns the forward-filled value at day (last observation <= day).
// ok is false before the first observation.
func (s preparedSeries) valueAt(day int) (float64, bool) {
	idx := sort.SearchInts(s.days, day)
	if idx < len(s.days) && s.days[idx] == day {
		return s.values[idx], true
	}
	if idx == 0 {
		return 0, false
	}
	return s.values[idx-1], true
}

// hasObservation reports whether the series has a real point on day.
func (s preparedSeries) hasObservation(day int) bool {
	idx := sort.SearchInts(s.days, day)
	return idx < len(s.days) && s.days[idx] == day
}

// fillStats computes forward-fill facts over [start, end]: filled day count
// and the longest consecutive filled run.
func (s preparedSeries) fillStats(start, end int) (int, int) {
	fillCount, maxRun, run := 0, 0, 0
	for d := start; d <= end; d++ {
		if s.hasObservation(d) {
			run = 0
			continue
		}
		fillCount++
		run++
		if run > maxRun {
			maxRun = run
		}
	}
	return fillCount, maxRun
}

// ResearchFillGapToleranceDays returns the forward-fill tolerance for one
// instrument type (td/099 §3.6).
func ResearchFillGapToleranceDays(instrumentType string) int {
	if instrumentType == "cn_mutual_fund" {
		return researchFillGapMutualFundDays
	}
	return researchFillGapDefaultDays
}

// ResearchStaleToleranceDays returns the data-staleness threshold for one
// instrument type (td/099 §3.5).
func ResearchStaleToleranceDays(instrumentType string) int {
	if instrumentType == "cn_mutual_fund" {
		return 10
	}
	return 7
}

// ResearchFXPair returns the FX pair converting assetCurrency into
// baseCurrency, or "" when no conversion is needed. Cross rates through CNY
// are expressed as "AAA/BBB via CNY" and resolved by the engine using both
// CNY legs.
type researchFXRoute struct {
	direct  string // direct pair, e.g. USDCNY
	numer   string // cross numerator leg (XXXCNY)
	denom   string // cross denominator leg (YYYCNY)
	isCross bool
}

func researchFXRouteFor(assetCurrency, baseCurrency string) (researchFXRoute, bool) {
	if assetCurrency == baseCurrency {
		return researchFXRoute{}, false
	}
	if baseCurrency == "CNY" {
		return researchFXRoute{direct: assetCurrency + "CNY"}, true
	}
	if assetCurrency == "CNY" {
		// CNY asset under non-CNY base: invert the base leg.
		return researchFXRoute{numer: "CNYCNY", denom: baseCurrency + "CNY", isCross: true}, true
	}
	// Cross through CNY: asset->CNY divided by base->CNY.
	return researchFXRoute{numer: assetCurrency + "CNY", denom: baseCurrency + "CNY", isCross: true}, true
}

// ResearchFXPairsFor lists the CNY-leg FX pairs needed to convert
// assetCurrency into baseCurrency (td/099 §3.6 基准币种处理).
func ResearchFXPairsFor(assetCurrency, baseCurrency string) []string {
	route, need := researchFXRouteFor(assetCurrency, baseCurrency)
	if !need {
		return nil
	}
	if !route.isCross {
		return []string{route.direct}
	}
	var out []string
	if route.numer != "CNYCNY" {
		out = append(out, route.numer)
	}
	out = append(out, route.denom)
	return out
}

// fxConverter converts one currency into the base currency at a given day.
type fxConverter struct {
	route researchFXRoute
	numer preparedSeries
	denom preparedSeries
	need  bool
}

func (c fxConverter) rateAt(day int) (float64, bool) {
	if !c.need {
		return 1, true
	}
	if !c.route.isCross {
		return c.numer.valueAt(day)
	}
	num := 1.0
	var ok bool
	if c.route.numer != "CNYCNY" {
		num, ok = c.numer.valueAt(day)
		if !ok {
			return 0, false
		}
	}
	den, ok := c.denom.valueAt(day)
	if !ok || den == 0 {
		return 0, false
	}
	return num / den, true
}

// firstDay/lastDay report the FX availability bounds (0 when unbounded).
func (c fxConverter) bounds() (int, int, bool) {
	if !c.need {
		return 0, 0, false
	}
	var first, last int
	set := false
	consider := func(s preparedSeries) {
		if s.empty() {
			return
		}
		if !set {
			first, last = s.firstDay(), s.lastDay()
			set = true
			return
		}
		if s.firstDay() > first {
			first = s.firstDay()
		}
		if s.lastDay() < last {
			last = s.lastDay()
		}
	}
	if !c.route.isCross {
		consider(c.numer)
	} else {
		if c.route.numer != "CNYCNY" {
			consider(c.numer)
		}
		consider(c.denom)
	}
	return first, last, set
}

// --- engine ---

type preparedAsset struct {
	input     BacktestAssetInput
	series    preparedSeries
	fx        fxConverter
	fxLabel   string
	usableLo  int
	usableHi  int
	unbounded bool
}

// RunResearchBacktest executes one deterministic portfolio backtest.
func RunResearchBacktest(in BacktestInput) (*BacktestResult, error) {
	if len(in.Assets) == 0 {
		return nil, ErrResearchNoAssets
	}
	weightSum := 0.0
	for _, a := range in.Assets {
		if a.Weight < 0 || a.Weight > 1+ResearchWeightTolerance {
			return nil, fmt.Errorf("%w: %s weight %v", ErrResearchWeightInvalid, a.AssetKey, a.Weight)
		}
		weightSum += a.Weight
	}
	if math.Abs(weightSum-1) > ResearchWeightTolerance {
		return nil, fmt.Errorf("%w: sum %v", ErrResearchWeightInvalid, weightSum)
	}

	fxSeries := make(map[string]preparedSeries, len(in.FX))
	for pair, pts := range in.FX {
		ps, err := prepareSeries(pts)
		if err != nil {
			return nil, fmt.Errorf("fx %s: %w", pair, err)
		}
		fxSeries[pair] = ps
	}
	fxTolerance := in.FXMaxFillGapDays
	if fxTolerance <= 0 {
		fxTolerance = researchFXFillGapDays
	}

	assets, err := prepareAssets(in, fxSeries)
	if err != nil {
		return nil, err
	}

	lo, hi, err := commonWindow(assets)
	if err != nil {
		return nil, err
	}
	lo, hi, err = clampUserWindow(lo, hi, in.WindowStart, in.WindowEnd)
	if err != nil {
		return nil, err
	}
	if hi-lo < researchMinWindowDays {
		return nil, fmt.Errorf("%w: %d days", ErrResearchWindowTooShort, hi-lo)
	}

	res, err := simulatePortfolio(in, assets, fxSeries, fxTolerance, lo, hi)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func prepareAssets(in BacktestInput, fxSeries map[string]preparedSeries) ([]preparedAsset, error) {
	out := make([]preparedAsset, 0, len(in.Assets))
	for _, a := range in.Assets {
		pa := preparedAsset{input: a}
		conv, label, err := prepareFXConverter(a.AssetKey, a.Currency, in.BaseCurrency, fxSeries)
		if err != nil {
			return nil, err
		}
		pa.fx = conv
		pa.fxLabel = label

		if a.IsCash {
			pa.unbounded = true
			if fxLo, fxHi, bounded := conv.bounds(); bounded {
				pa.usableLo, pa.usableHi = fxLo, fxHi
				pa.unbounded = false
			}
			out = append(out, pa)
			continue
		}

		series, err := prepareSeries(a.Points)
		if err != nil {
			return nil, fmt.Errorf("asset %s: %w", a.AssetKey, err)
		}
		if series.empty() {
			return nil, fmt.Errorf("%w: asset %s has no history", ErrResearchNoCommonWindow, a.AssetKey)
		}
		pa.series = series
		pa.usableLo, pa.usableHi = series.firstDay(), series.lastDay()
		if fxLo, fxHi, bounded := conv.bounds(); bounded {
			if fxLo > pa.usableLo {
				pa.usableLo = fxLo
			}
			if fxHi < pa.usableHi {
				pa.usableHi = fxHi
			}
		}
		out = append(out, pa)
	}
	return out, nil
}

func prepareFXConverter(
	assetKey, currency, baseCurrency string,
	fxSeries map[string]preparedSeries,
) (fxConverter, string, error) {
	route, need := researchFXRouteFor(currency, baseCurrency)
	conv := fxConverter{route: route, need: need}
	if !need {
		return conv, "", nil
	}
	if !route.isCross {
		series, err := requireFXSeries(fxSeries, assetKey, route.direct)
		conv.numer = series
		return conv, route.direct, err
	}
	label := "1/" + route.denom
	if route.numer != "CNYCNY" {
		series, err := requireFXSeries(fxSeries, assetKey, route.numer)
		if err != nil {
			return conv, "", err
		}
		conv.numer = series
		label = route.numer + "/" + route.denom
	}
	series, err := requireFXSeries(fxSeries, assetKey, route.denom)
	if err != nil {
		return conv, "", err
	}
	conv.denom = series
	return conv, label, nil
}

func requireFXSeries(
	fxSeries map[string]preparedSeries, assetKey, pair string,
) (preparedSeries, error) {
	series, ok := fxSeries[pair]
	if !ok || series.empty() {
		return preparedSeries{}, fmt.Errorf("%w: %s needs %s", ErrResearchFXMissing, assetKey, pair)
	}
	return series, nil
}

// commonWindow computes max(first usable) .. min(last usable) over bounded
// assets. Cash assets in the base currency are unbounded and never constrain
// the window.
func commonWindow(assets []preparedAsset) (int, int, error) {
	lo, hi := 0, 0
	found := false
	for _, a := range assets {
		if a.unbounded {
			continue
		}
		if !found {
			lo, hi = a.usableLo, a.usableHi
			found = true
			continue
		}
		if a.usableLo > lo {
			lo = a.usableLo
		}
		if a.usableHi < hi {
			hi = a.usableHi
		}
	}
	if !found {
		return 0, 0, fmt.Errorf("%w: every asset is unbounded cash", ErrResearchNoCommonWindow)
	}
	if hi <= lo {
		return 0, 0, ErrResearchNoCommonWindow
	}
	return lo, hi, nil
}

func clampUserWindow(lo, hi int, startStr, endStr string) (int, int, error) {
	if startStr != "" {
		day, err := parseResearchDate(startStr)
		if err != nil {
			return 0, 0, err
		}
		if day > lo {
			lo = day
		}
	}
	if endStr != "" {
		day, err := parseResearchDate(endStr)
		if err != nil {
			return 0, 0, err
		}
		if day < hi {
			hi = day
		}
	}
	if hi <= lo {
		return 0, 0, ErrResearchNoCommonWindow
	}
	return lo, hi, nil
}

// assetValueAt returns the asset's forward-filled base-currency value.
func assetValueAt(a preparedAsset, day int) (float64, bool) {
	rate, ok := a.fx.rateAt(day)
	if !ok {
		return 0, false
	}
	if a.input.IsCash {
		return rate, true
	}
	v, ok := a.series.valueAt(day)
	if !ok {
		return 0, false
	}
	return v * rate, true
}

func simulatePortfolio(
	in BacktestInput,
	assets []preparedAsset,
	fxSeries map[string]preparedSeries,
	fxTolerance int,
	lo, hi int,
) (*BacktestResult, error) {
	values, effective, err := buildResearchValueGrid(assets, fxSeries, lo, hi)
	if err != nil {
		return nil, err
	}
	targets := normalizedResearchTargets(assets)
	walk := walkResearchPortfolio(in, values, targets, lo, hi)
	effReturns, effAssetReturns, effContribReturns := collectEffectiveResearchReturns(
		effective, walk.periodReturns, walk.assetReturns, walk.contribRows,
	)
	if len(effReturns) < 2 {
		return nil, ErrResearchNoEffectiveDays
	}
	drawdowns := researchDrawdowns(walk.navs)
	points := buildResearchPoints(assets, walk, drawdowns, lo)

	// Benchmark overlay.
	var benchSummary *BacktestBenchmarkSummary
	var benchQuality *BacktestSeriesQuality
	if in.Benchmark != nil {
		var err error
		benchSummary, benchQuality, err = overlayBenchmark(in, fxSeries, points, lo, hi)
		if err != nil {
			return nil, err
		}
	}

	years := buildYears(points, effective, lo)
	months := buildMonths(points)
	summary := buildSummary(
		in, points, years, months, effReturns, effAssetReturns, effContribReturns,
		assets, targets, walk.weightRows, walk.contribRows, drawdowns, walk.navs, lo, hi,
	)
	summary.Benchmark = benchSummary

	dq := buildDataQuality(assets, fxSeries, fxTolerance, lo, hi)
	dq.Benchmark = benchQuality

	return &BacktestResult{
		WindowStart: researchDayToDate(lo),
		WindowEnd:   researchDayToDate(hi),
		Points:      points,
		Years:       years,
		Months:      months,
		Summary:     summary,
		DataQuality: dq,
	}, nil
}

type researchPortfolioWalk struct {
	navs          []float64
	periodReturns []float64
	weightRows    [][]float64
	contribRows   [][]float64
	assetReturns  [][]float64
}

func buildResearchValueGrid(
	assets []preparedAsset, fxSeries map[string]preparedSeries, lo, hi int,
) ([][]float64, []bool, error) {
	n := hi - lo + 1
	values := make([][]float64, len(assets))
	for i := range values {
		values[i] = make([]float64, n)
	}
	effective := make([]bool, n)
	usedPairs := usedFXPairs(assets)
	for t := 0; t < n; t++ {
		day := lo + t
		for i, asset := range assets {
			value, ok := assetValueAt(asset, day)
			if !ok {
				return nil, nil, fmt.Errorf("%w: asset %s at %s",
					ErrResearchNoCommonWindow, asset.input.AssetKey, researchDayToDate(day))
			}
			values[i][t] = value
			effective[t] = effective[t] || (!asset.input.IsCash && asset.series.hasObservation(day))
		}
		for _, pair := range usedPairs {
			series, ok := fxSeries[pair]
			effective[t] = effective[t] || (ok && series.hasObservation(day))
		}
	}
	return values, effective, nil
}

func normalizedResearchTargets(assets []preparedAsset) []float64 {
	targets := make([]float64, len(assets))
	sum := 0.0
	for i, asset := range assets {
		targets[i] = asset.input.Weight
		sum += asset.input.Weight
	}
	if sum <= 0 {
		return targets
	}
	for i := range targets {
		targets[i] /= sum
	}
	return targets
}

func walkResearchPortfolio(
	in BacktestInput, values [][]float64, targets []float64, lo, hi int,
) researchPortfolioWalk {
	n, numAssets := hi-lo+1, len(values)
	walk := researchPortfolioWalk{
		navs: make([]float64, n), periodReturns: make([]float64, n),
		weightRows: make([][]float64, n), contribRows: make([][]float64, n),
		assetReturns: make([][]float64, numAssets),
	}
	for i := range walk.assetReturns {
		walk.assetReturns[i] = make([]float64, n)
	}
	weights := append([]float64(nil), targets...)
	walk.navs[0] = 1
	walk.weightRows[0] = append([]float64(nil), weights...)
	walk.contribRows[0] = make([]float64, numAssets)
	for t := 1; t < n; t++ {
		portfolioReturn := researchPeriodReturn(values, weights, walk.assetReturns, t)
		contributions := make([]float64, numAssets)
		for i := range weights {
			contributions[i] = weights[i] * walk.assetReturns[i][t]
		}
		walk.navs[t] = walk.navs[t-1] * (1 + portfolioReturn)
		walk.periodReturns[t] = portfolioReturn
		walk.contribRows[t] = contributions
		driftResearchWeights(weights, walk.assetReturns, portfolioReturn, t)
		if shouldRebalance(in.RebalancePolicy, in.RebalanceThreshold, lo+t, hi, weights, targets) {
			copy(weights, targets)
		}
		walk.weightRows[t] = append([]float64(nil), weights...)
	}
	return walk
}

func researchPeriodReturn(values [][]float64, weights []float64, returns [][]float64, t int) float64 {
	portfolioReturn := 0.0
	for i := range weights {
		returns[i][t] = values[i][t]/values[i][t-1] - 1
		portfolioReturn += weights[i] * returns[i][t]
	}
	return portfolioReturn
}

func driftResearchWeights(weights []float64, returns [][]float64, portfolioReturn float64, t int) {
	if 1+portfolioReturn <= 0 {
		return
	}
	for i := range weights {
		weights[i] = weights[i] * (1 + returns[i][t]) / (1 + portfolioReturn)
	}
}

func collectEffectiveResearchReturns(
	effective []bool, periodReturns []float64, assetReturns, contribRows [][]float64,
) ([]float64, [][]float64, [][]float64) {
	effReturns := make([]float64, 0)
	effAssetReturns := make([][]float64, len(assetReturns))
	effContribReturns := make([][]float64, len(assetReturns))
	for t := 1; t < len(effective); t++ {
		if !effective[t] {
			continue
		}
		effReturns = append(effReturns, periodReturns[t])
		for i := range assetReturns {
			effAssetReturns[i] = append(effAssetReturns[i], assetReturns[i][t])
			effContribReturns[i] = append(effContribReturns[i], contribRows[t][i])
		}
	}
	return effReturns, effAssetReturns, effContribReturns
}

func researchDrawdowns(navs []float64) []float64 {
	drawdowns := make([]float64, len(navs))
	peak := navs[0]
	for i, nav := range navs {
		peak = math.Max(peak, nav)
		drawdowns[i] = nav/peak - 1
	}
	return drawdowns
}

func buildResearchPoints(
	assets []preparedAsset, walk researchPortfolioWalk, drawdowns []float64, lo int,
) []BacktestPoint {
	points := make([]BacktestPoint, len(walk.navs))
	for t := range points {
		point := BacktestPoint{
			Date: researchDayToDate(lo + t), NAV: walk.navs[t],
			CumulativeReturn: walk.navs[t]/walk.navs[0] - 1,
			PeriodReturn:     walk.periodReturns[t], Drawdown: drawdowns[t],
			Weights:       make(map[string]float64, len(assets)),
			Contributions: make(map[string]float64, len(assets)),
		}
		for i, asset := range assets {
			point.Weights[asset.input.AssetKey] = walk.weightRows[t][i]
			point.Contributions[asset.input.AssetKey] = walk.contribRows[t][i]
		}
		points[t] = point
	}
	return points
}

func usedFXPairs(assets []preparedAsset) []string {
	set := map[string]bool{}
	for _, a := range assets {
		if !a.fx.need {
			continue
		}
		if a.fx.route.isCross {
			if a.fx.route.numer != "CNYCNY" {
				set[a.fx.route.numer] = true
			}
			set[a.fx.route.denom] = true
		} else {
			set[a.fx.route.direct] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// shouldRebalance decides whether target weights are restored after day's
// close. Calendar policies rebalance on the last calendar day of each
// period; the final day never needs one.
func shouldRebalance(policy string, threshold float64, day, lastDay int, weights, targets []float64) bool {
	switch policy {
	case ResearchRebalanceBuyHold:
		return false
	case ResearchRebalanceFixed:
		return true
	case ResearchRebalanceThreshold:
		if threshold <= 0 {
			return false
		}
		for i := range weights {
			if math.Abs(weights[i]-targets[i]) >= threshold {
				return true
			}
		}
		return false
	}
	if day >= lastDay {
		return false
	}
	y1, m1, _ := researchDayYMD(day)
	y2, m2, _ := researchDayYMD(day + 1)
	switch policy {
	case ResearchRebalanceMonthly:
		return y1 != y2 || m1 != m2
	case ResearchRebalanceQuarterly:
		q1 := (int(m1) - 1) / 3
		q2 := (int(m2) - 1) / 3
		return y1 != y2 || q1 != q2
	case ResearchRebalanceYearly:
		return y1 != y2
	default:
		// Unknown policy behaves like monthly (validated upstream).
		return y1 != y2 || m1 != m2
	}
}

func overlayBenchmark(
	in BacktestInput,
	fxSeries map[string]preparedSeries,
	points []BacktestPoint,
	lo, hi int,
) (*BacktestBenchmarkSummary, *BacktestSeriesQuality, error) {
	b := in.Benchmark
	series, err := prepareBenchmarkSeries(b)
	if err != nil {
		return nil, nil, err
	}
	conv, _, err := prepareFXConverter("benchmark", b.Currency, in.BaseCurrency, fxSeries)
	if err != nil {
		return nil, nil, err
	}
	if !b.IsCash && (series.firstDay() > lo || series.lastDay() < hi) {
		return nil, nil, fmt.Errorf("%w: benchmark %s does not cover %s..%s",
			ErrResearchNoCommonWindow, b.AssetKey, researchDayToDate(lo), researchDayToDate(hi))
	}
	if fxLo, fxHi, bounded := conv.bounds(); bounded && (fxLo > lo || fxHi < hi) {
		return nil, nil, fmt.Errorf("%w: benchmark FX does not cover %s..%s",
			ErrResearchNoCommonWindow, researchDayToDate(lo), researchDayToDate(hi))
	}

	start, ok := benchmarkValueAt(b, series, conv, lo)
	if !ok || start <= 0 {
		return nil, nil, fmt.Errorf("%w: benchmark %s not usable at window start",
			ErrResearchNoCommonWindow, b.AssetKey)
	}
	maxDD := overlayBenchmarkPoints(b, series, conv, points, lo, start)
	endNav := *points[len(points)-1].BenchmarkNAV
	days := hi - lo
	cagr := 0.0
	if days > 0 && endNav > 0 {
		cagr = math.Pow(endNav, 365.25/float64(days)) - 1
	}
	quality, err := buildBenchmarkQuality(b, series, lo, hi)
	if err != nil {
		return nil, nil, err
	}
	return &BacktestBenchmarkSummary{
		AssetKey:         b.AssetKey,
		Name:             b.Name,
		CumulativeReturn: endNav - 1,
		CAGR:             cagr,
		MaxDrawdown:      maxDD,
	}, quality, nil
}

func prepareBenchmarkSeries(benchmark *BacktestBenchmarkInput) (preparedSeries, error) {
	if benchmark.IsCash {
		return preparedSeries{}, nil
	}
	series, err := prepareSeries(benchmark.Points)
	if err != nil {
		return preparedSeries{}, fmt.Errorf("benchmark %s: %w", benchmark.AssetKey, err)
	}
	if series.empty() {
		return preparedSeries{}, fmt.Errorf(
			"%w: benchmark %s has no history", ErrResearchNoCommonWindow, benchmark.AssetKey,
		)
	}
	return series, nil
}

func benchmarkValueAt(
	benchmark *BacktestBenchmarkInput, series preparedSeries, conv fxConverter, day int,
) (float64, bool) {
	rate, ok := conv.rateAt(day)
	if !ok {
		return 0, false
	}
	if benchmark.IsCash {
		return rate, true
	}
	value, ok := series.valueAt(day)
	if !ok || day > series.lastDay() {
		return 0, false
	}
	return value * rate, true
}

func overlayBenchmarkPoints(
	benchmark *BacktestBenchmarkInput,
	series preparedSeries,
	conv fxConverter,
	points []BacktestPoint,
	lo int,
	start float64,
) float64 {
	prev, peak, maxDrawdown := start, 1.0, 0.0
	for i := range points {
		value, ok := benchmarkValueAt(benchmark, series, conv, lo+i)
		if !ok {
			value = prev
		}
		nav, periodReturn := value/start, 0.0
		if i > 0 && prev > 0 {
			periodReturn = value/prev - 1
		}
		navCopy, returnCopy := nav, periodReturn
		points[i].BenchmarkNAV = &navCopy
		points[i].BenchmarkReturn = &returnCopy
		peak = math.Max(peak, nav)
		maxDrawdown = math.Min(maxDrawdown, nav/peak-1)
		prev = value
	}
	return maxDrawdown
}

func buildBenchmarkQuality(
	benchmark *BacktestBenchmarkInput, series preparedSeries, lo, hi int,
) (*BacktestSeriesQuality, error) {
	tolerance := benchmark.MaxFillGapDays
	if tolerance <= 0 {
		tolerance = researchFillGapDefaultDays
	}
	quality := &BacktestSeriesQuality{
		AssetKey: benchmark.AssetKey, Name: benchmark.Name, Currency: benchmark.Currency,
		IsCash: benchmark.IsCash, FillTolerance: tolerance,
	}
	if benchmark.IsCash {
		return quality, nil
	}
	quality.RawStart = researchDayToDate(series.firstDay())
	quality.RawEnd = researchDayToDate(series.lastDay())
	quality.RawPointCount = len(series.days)
	quality.FillCount, quality.MaxFillGapDays = series.fillStats(
		maxInt(lo, series.firstDay()), minInt(hi, series.lastDay()))
	quality.FillGapExceeded = quality.MaxFillGapDays > tolerance
	if quality.FillGapExceeded {
		return nil, fmt.Errorf("%w: benchmark %s fill gap %d exceeds tolerance %d",
			ErrResearchNoCommonWindow, benchmark.AssetKey, quality.MaxFillGapDays, tolerance)
	}
	return quality, nil
}

// --- derived tables ---

func buildYears(points []BacktestPoint, effective []bool, lo int) []BacktestYear {
	if len(points) == 0 {
		return nil
	}
	startYear, _, _ := researchDayYMD(lo)
	endYear, _, _ := researchDayYMD(lo + len(points) - 1)

	var years []BacktestYear
	prevEndNAV := points[0].NAV
	idx := 0
	for year := startYear; year <= endYear; year++ {
		var yearPoints []BacktestPoint
		var yearEff []bool
		for idx < len(points) {
			y, _, _ := researchDayYMD(lo + idx)
			if y != year {
				break
			}
			yearPoints = append(yearPoints, points[idx])
			yearEff = append(yearEff, effective[idx])
			idx++
		}
		if len(yearPoints) == 0 {
			continue
		}
		yearResult := buildResearchYear(year, prevEndNAV, yearPoints, yearEff)
		years = append(years, yearResult)
		prevEndNAV = yearResult.EndNAV
	}
	// Display order: newest first (td/099 §6.2).
	sort.Slice(years, func(i, j int) bool { return years[i].Year > years[j].Year })
	return years
}

func buildResearchYear(
	year int, startNAV float64, points []BacktestPoint, effective []bool,
) BacktestYear {
	peak, maxDrawdown, prevNAV := startNAV, 0.0, startNAV
	returns := make([]float64, 0, len(points))
	for i, point := range points {
		peak = math.Max(peak, point.NAV)
		maxDrawdown = math.Min(maxDrawdown, point.NAV/peak-1)
		if effective[i] && prevNAV > 0 {
			returns = append(returns, point.NAV/prevNAV-1)
		}
		prevNAV = point.NAV
	}
	volatility := 0.0
	if len(returns) >= 2 {
		volatility = sampleStd(returns) * math.Sqrt(252)
	}
	endNAV := points[len(points)-1].NAV
	annualReturn := 0.0
	if startNAV > 0 {
		annualReturn = endNAV/startNAV - 1
	}
	return BacktestYear{
		Year: year, AnnualReturn: annualReturn, Volatility: volatility,
		MaxDrawdown: maxDrawdown, StartNAV: startNAV, EndNAV: endNAV,
		IsPartial: points[0].Date != fmt.Sprintf("%04d-01-01", year) ||
			points[len(points)-1].Date != fmt.Sprintf("%04d-12-31", year),
	}
}

func buildMonths(points []BacktestPoint) []BacktestMonth {
	if len(points) == 0 {
		return nil
	}
	var months []BacktestMonth
	prevNAV := points[0].NAV
	curKey := points[0].Date[:7]
	lastNAV := points[0].NAV
	flush := func(key string, endNAV float64) {
		var y, m int
		_, _ = fmt.Sscanf(key, "%d-%d", &y, &m)
		ret := 0.0
		if prevNAV > 0 {
			ret = endNAV/prevNAV - 1
		}
		months = append(months, BacktestMonth{Year: y, Month: m, MonthlyReturn: ret})
		prevNAV = endNAV
	}
	for _, p := range points[1:] {
		key := p.Date[:7]
		if key != curKey {
			flush(curKey, lastNAV)
			curKey = key
		}
		lastNAV = p.NAV
	}
	flush(curKey, lastNAV)
	return months
}

func buildSummary(
	in BacktestInput,
	points []BacktestPoint,
	years []BacktestYear,
	months []BacktestMonth,
	effReturns []float64,
	effAssetReturns [][]float64,
	effContribReturns [][]float64,
	assets []preparedAsset,
	targets []float64,
	weightRows, contribRows [][]float64,
	drawdowns, navs []float64,
	lo, hi int,
) BacktestSummary {
	n := len(points)
	summary := BacktestSummary{
		CumulativeReturn:    navs[n-1]/navs[0] - 1,
		EffectiveReturnDays: len(effReturns),
		RiskFreeRate:        in.RiskFreeRate,
	}
	populateResearchRiskSummary(&summary, in.RiskFreeRate, effReturns, navs, hi-lo)
	populateResearchDrawdownWindow(&summary, points, navs, drawdowns)
	summary.MaxDrawdownDurationDays, summary.CurrentDrawdownDays = drawdownDurations(navs)
	populateResearchExtremes(&summary, years, months)
	summary.Contributions = buildContributions(
		assets, targets, weightRows, contribRows, effReturns, effContribReturns, navs, drawdowns,
	)
	summary.Correlations = buildCorrelations(assets, effAssetReturns)
	return summary
}

func populateResearchRiskSummary(
	summary *BacktestSummary, riskFreeRate float64, returns, navs []float64, days int,
) {
	if days > 0 && navs[0] > 0 && navs[len(navs)-1] > 0 {
		summary.CAGR = math.Pow(navs[len(navs)-1]/navs[0], 365.25/float64(days)) - 1
	}
	if len(returns) < 2 {
		return
	}
	volatility := sampleStd(returns) * math.Sqrt(252)
	summary.AnnualVolatility = &volatility
	if volatility > 0 {
		sharpe := (summary.CAGR - riskFreeRate) / volatility
		summary.Sharpe = &sharpe
	}
}

func populateResearchDrawdownWindow(
	summary *BacktestSummary, points []BacktestPoint, navs, drawdowns []float64,
) {
	troughIdx := 0
	for i, drawdown := range drawdowns {
		if drawdown < summary.MaxDrawdown {
			summary.MaxDrawdown = drawdown
			troughIdx = i
		}
	}
	if summary.MaxDrawdown == 0 {
		return
	}
	calmar := summary.CAGR / math.Abs(summary.MaxDrawdown)
	summary.Calmar = &calmar
	peakIdx, peakValue := 0, navs[0]
	for i := 0; i <= troughIdx; i++ {
		if navs[i] > peakValue {
			peakIdx, peakValue = i, navs[i]
		}
	}
	summary.MaxDrawdownStart = points[peakIdx].Date
	summary.MaxDrawdownTrough = points[troughIdx].Date
	for i := troughIdx + 1; i < len(navs); i++ {
		if navs[i] >= peakValue {
			summary.MaxDrawdownRecovery = points[i].Date
			return
		}
	}
}

func populateResearchExtremes(
	summary *BacktestSummary, years []BacktestYear, months []BacktestMonth,
) {
	for _, year := range years {
		if summary.BestYear == nil || year.AnnualReturn > summary.BestYear.Return {
			summary.BestYear = &YearExtreme{Year: year.Year, Return: year.AnnualReturn}
		}
		if summary.WorstYear == nil || year.AnnualReturn < summary.WorstYear.Return {
			summary.WorstYear = &YearExtreme{Year: year.Year, Return: year.AnnualReturn}
		}
	}
	positive := 0
	for _, month := range months {
		if summary.BestMonth == nil || month.MonthlyReturn > summary.BestMonth.Return {
			summary.BestMonth = &MonthExtreme{Year: month.Year, Month: month.Month, Return: month.MonthlyReturn}
		}
		if summary.WorstMonth == nil || month.MonthlyReturn < summary.WorstMonth.Return {
			summary.WorstMonth = &MonthExtreme{Year: month.Year, Month: month.Month, Return: month.MonthlyReturn}
		}
		if month.MonthlyReturn > 0 {
			positive++
		}
	}
	if len(months) > 0 {
		ratio := float64(positive) / float64(len(months))
		summary.PositiveMonthRatio = &ratio
	}
}

// drawdownDurations returns the longest historical drawdown episode length
// (calendar days, ongoing episodes included) and the current episode length.
func drawdownDurations(navs []float64) (int, int) {
	maxDur, current := 0, 0
	peakIdx := 0
	peakVal := navs[0]
	for t := 1; t < len(navs); t++ {
		if navs[t] >= peakVal {
			if dur := t - peakIdx; dur > maxDur && navs[t-1] < peakVal {
				maxDur = dur
			}
			peakVal = navs[t]
			peakIdx = t
			continue
		}
	}
	if navs[len(navs)-1] < peakVal {
		current = len(navs) - 1 - peakIdx
		if current > maxDur {
			maxDur = current
		}
	}
	return maxDur, current
}

func buildContributions(
	assets []preparedAsset,
	targets []float64,
	weightRows, contribRows [][]float64,
	effReturns []float64,
	effContribReturns [][]float64,
	navs, drawdowns []float64,
) []BacktestAssetContribution {
	n := len(weightRows)
	numAssets := len(assets)

	// Max drawdown window for drawdown contributions.
	minDD := 0.0
	troughIdx := 0
	for t, dd := range drawdowns {
		if dd < minDD {
			minDD = dd
			troughIdx = t
		}
	}
	peakIdx := 0
	if minDD < 0 {
		peakVal := navs[0]
		for t := 0; t <= troughIdx; t++ {
			if navs[t] > peakVal {
				peakVal = navs[t]
				peakIdx = t
			}
		}
	}

	portVar := sampleVariance(effReturns)
	out := make([]BacktestAssetContribution, 0, numAssets)
	for i := range assets {
		c := BacktestAssetContribution{
			AssetKey:     assets[i].input.AssetKey,
			Name:         assets[i].input.Name,
			TargetWeight: targets[i],
			EndWeight:    weightRows[n-1][i],
		}
		for t := 1; t < n; t++ {
			c.CumulativeContribution += (navs[t-1] / navs[0]) * contribRows[t][i]
		}
		if minDD < 0 {
			for t := peakIdx + 1; t <= troughIdx; t++ {
				c.DrawdownContribution += (navs[t-1] / navs[peakIdx]) * contribRows[t][i]
			}
		}
		if portVar > 0 {
			cov := sampleCovariance(effContribReturns[i], effReturns)
			rc := cov / portVar
			c.RiskContribution = &rc
		}
		out = append(out, c)
	}
	return out
}

func buildCorrelations(assets []preparedAsset, effAssetReturns [][]float64) *BacktestCorrelations {
	numAssets := len(assets)
	if numAssets == 0 {
		return nil
	}
	out := &BacktestCorrelations{
		AssetKeys: make([]string, numAssets),
		Names:     make([]string, numAssets),
		Matrix:    make([][]*float64, numAssets),
	}
	for i := range assets {
		out.AssetKeys[i] = assets[i].input.AssetKey
		out.Names[i] = assets[i].input.Name
		out.Matrix[i] = make([]*float64, numAssets)
	}
	for i := 0; i < numAssets; i++ {
		for j := i; j < numAssets; j++ {
			corr, ok := pearsonCorrelation(effAssetReturns[i], effAssetReturns[j])
			if !ok {
				continue
			}
			v := corr
			out.Matrix[i][j] = &v
			if i != j {
				w := corr
				out.Matrix[j][i] = &w
			}
		}
	}
	return out
}

func buildDataQuality(
	assets []preparedAsset,
	fxSeries map[string]preparedSeries,
	fxTolerance int,
	lo, hi int,
) BacktestDataQuality {
	dq := BacktestDataQuality{
		CommonStartPolicy:  "max_asset_start",
		CommonEndPolicy:    "min_asset_end",
		ForwardFillDaysMax: researchFillGapDefaultDays,
		WindowStart:        researchDayToDate(lo),
		WindowEnd:          researchDayToDate(hi),
		Assets:             make([]BacktestSeriesQuality, 0, len(assets)),
		FX:                 make([]BacktestSeriesQuality, 0),
	}
	commonLo, commonHi, err := commonWindow(assets)
	if err == nil {
		dq.CommonStart = researchDayToDate(commonLo)
		dq.CommonEnd = researchDayToDate(commonHi)
	}
	for _, a := range assets {
		q := BacktestSeriesQuality{
			AssetKey:      a.input.AssetKey,
			Name:          a.input.Name,
			Currency:      a.input.Currency,
			FXPair:        a.fxLabel,
			IsCash:        a.input.IsCash,
			FillTolerance: a.input.MaxFillGapDays,
		}
		if q.FillTolerance <= 0 {
			q.FillTolerance = researchFillGapDefaultDays
		}
		if !a.input.IsCash && !a.series.empty() {
			q.RawStart = researchDayToDate(a.series.firstDay())
			q.RawEnd = researchDayToDate(a.series.lastDay())
			q.RawPointCount = len(a.series.days)
			fillLo := maxInt(lo, a.series.firstDay())
			fillHi := minInt(hi, a.series.lastDay())
			if fillHi >= fillLo {
				q.FillCount, q.MaxFillGapDays = a.series.fillStats(fillLo, fillHi)
			}
			q.FillGapExceeded = q.MaxFillGapDays > q.FillTolerance
		}
		if !a.unbounded {
			q.UsableStart = researchDayToDate(a.usableLo)
			q.UsableEnd = researchDayToDate(a.usableHi)
			q.LimitsCommonStart = a.usableLo == commonLo
			q.LimitsCommonEnd = a.usableHi == commonHi
		}
		dq.Assets = append(dq.Assets, q)
	}
	for _, pair := range usedFXPairs(assets) {
		s, ok := fxSeries[pair]
		if !ok || s.empty() {
			continue
		}
		fillLo := maxInt(lo, s.firstDay())
		fillHi := minInt(hi, s.lastDay())
		fillCount, maxRun := 0, 0
		if fillHi >= fillLo {
			fillCount, maxRun = s.fillStats(fillLo, fillHi)
		}
		dq.FX = append(dq.FX, BacktestSeriesQuality{
			Pair:            pair,
			RawStart:        researchDayToDate(s.firstDay()),
			RawEnd:          researchDayToDate(s.lastDay()),
			RawPointCount:   len(s.days),
			FillCount:       fillCount,
			MaxFillGapDays:  maxRun,
			FillTolerance:   fxTolerance,
			FillGapExceeded: maxRun > fxTolerance,
		})
	}
	return dq
}

// --- shared statistics helpers ---

func sampleMean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// sampleVariance is the unbiased (n-1) variance; 0 for fewer than 2 samples.
func sampleVariance(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	mean := sampleMean(xs)
	sum := 0.0
	for _, x := range xs {
		d := x - mean
		sum += d * d
	}
	return sum / float64(len(xs)-1)
}

func sampleStd(xs []float64) float64 {
	return math.Sqrt(sampleVariance(xs))
}

// sampleCovariance is the unbiased covariance of equally-sized samples.
func sampleCovariance(xs, ys []float64) float64 {
	if len(xs) != len(ys) || len(xs) < 2 {
		return 0
	}
	mx, my := sampleMean(xs), sampleMean(ys)
	sum := 0.0
	for i := range xs {
		sum += (xs[i] - mx) * (ys[i] - my)
	}
	return sum / float64(len(xs)-1)
}

// pearsonCorrelation returns (corr, true) or (0, false) when undefined.
func pearsonCorrelation(xs, ys []float64) (float64, bool) {
	if len(xs) != len(ys) || len(xs) < 2 {
		return 0, false
	}
	sx, sy := sampleStd(xs), sampleStd(ys)
	if sx == 0 || sy == 0 {
		return 0, false
	}
	return sampleCovariance(xs, ys) / (sx * sy), true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
