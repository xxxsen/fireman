package simulation

import (
	"math"
	"sort"
	"strconv"
)

// RunOptions configures a Monte Carlo batch.
type RunOptions struct {
	Runs        int
	Progress    func(done, total int, phase string)
	CancelCheck func() bool
}

// RunResult holds aggregated simulation output.
type RunResult struct {
	HorizonMonths      int
	SuccessCount       int
	FailureCount       int
	Summary            Summary
	Paths              []PathSummary
	QuantileSeries     []QuantilePoint
	RealQuantileSeries []QuantilePoint
	Representative     map[string]int // percentile label -> path_no
}

// Summary is the persisted aggregate simulation result.
type Summary struct {
	SuccessProbability         float64            `json:"success_probability"`
	FailureProbability         float64            `json:"failure_probability"`
	SuccessWilsonLow           float64            `json:"success_wilson_low"`
	SuccessWilsonHigh          float64            `json:"success_wilson_high"`
	TerminalQuantiles          map[string]int64   `json:"terminal_quantiles"`
	RealTerminalQuantiles      map[string]int64   `json:"real_terminal_quantiles,omitempty"`
	MonthlyWealthQuantiles     []QuantilePoint    `json:"monthly_wealth_quantiles"`
	RealMonthlyWealthQuantiles []QuantilePoint    `json:"real_monthly_wealth_quantiles,omitempty"`
	SuccessTerminal            []int64            `json:"success_terminal_distribution,omitempty"`
	FailureTerminal            []int64            `json:"failure_terminal_distribution,omitempty"`
	FailureYearQuantiles       map[string]float64 `json:"failure_year_quantiles,omitempty"`
	MaxDrawdownQuantiles       map[string]float64 `json:"max_drawdown_quantiles,omitempty"`
	SpendingP50Minor           int64              `json:"spending_p50_minor"`
	TransactionCostP50Minor    int64              `json:"transaction_cost_p50_minor"`
	FailureReasons             map[string]int     `json:"failure_reasons"`
	TruncationPathCount        int                `json:"truncation_path_count"`
	TruncationPathRatio        float64            `json:"truncation_path_ratio"`
	ModelWarnings              []string           `json:"model_warnings,omitempty"`
	CorrelationDisclaimer      string             `json:"correlation_disclaimer"`
}

// QuantilePoint is one month of wealth quantiles.
type QuantilePoint struct {
	MonthOffset int   `json:"month_offset"`
	P00Minor    int64 `json:"p00_minor"`
	P05Minor    int64 `json:"p05_minor"`
	P25Minor    int64 `json:"p25_minor"`
	P50Minor    int64 `json:"p50_minor"`
	P75Minor    int64 `json:"p75_minor"`
	P95Minor    int64 `json:"p95_minor"`
}

// Run executes all paths deterministically sorted by path_no.
func Run(in *InputSnapshot, opt RunOptions) RunResult {
	runs := opt.Runs
	if runs <= 0 {
		runs = in.Parameters.SimulationRuns
	}
	horizon := in.HorizonMonths()
	paths := make([]PathSummary, runs)
	truncPaths := 0

	for i := 0; i < runs; i++ {
		if opt.CancelCheck != nil && opt.CancelCheck() {
			break
		}
		ps, _ := RunPath(in, i, PathRunOpts{CollectMonthlyWealth: true})
		paths[i] = ps
		if ps.TruncationCount > 0 {
			truncPaths++
		}
		if opt.Progress != nil && (i+1)%maxInt(1, runs/100) == 0 {
			opt.Progress(i+1, runs, "simulating")
		}
	}

	sort.Slice(paths, func(i, j int) bool { return paths[i].PathNo < paths[j].PathNo })

	success := 0
	for _, p := range paths {
		if p.Succeeded {
			success++
		}
	}

	terminals := make([]float64, len(paths))
	realTerminals := make([]float64, len(paths))
	monthlyByPath := make([][]int64, len(paths))
	realMonthlyByPath := make([][]int64, len(paths))
	for i, p := range paths {
		terminals[i] = float64(p.TerminalWealthMinor)
		realTerminals[i] = float64(p.RealTerminalWealthMinor)
		monthlyByPath[i] = p.MonthlyWealthMinor
		realMonthlyByPath[i] = realWealthSeries(p.MonthlyWealthMinor, p.MonthlyCumInflation)
	}

	low, high := WilsonInterval(success, len(paths), 1.96)
	summary := Summary{
		SuccessProbability:         float64(success) / float64(len(paths)),
		FailureProbability:         float64(len(paths)-success) / float64(len(paths)),
		SuccessWilsonLow:           low,
		SuccessWilsonHigh:          high,
		TerminalQuantiles:          terminalQuantiles(terminals),
		RealTerminalQuantiles:      terminalQuantiles(realTerminals),
		MonthlyWealthQuantiles:     monthlyQuantileSeries(monthlyByPath),
		RealMonthlyWealthQuantiles: monthlyQuantileSeries(realMonthlyByPath),
		FailureReasons:             failureReasonCounts(paths),
		TruncationPathCount:        truncPaths,
		TruncationPathRatio:        float64(truncPaths) / float64(len(paths)),
		CorrelationDisclaimer:      "未使用基金间历史相关性，分散化结果可能偏乐观",
	}
	summary.SuccessTerminal, summary.FailureTerminal = splitTerminals(paths)
	summary.FailureYearQuantiles = failureYearQuantiles(paths, in.Parameters.CurrentAge)
	summary.MaxDrawdownQuantiles = drawdownQuantiles(paths)
	summary.SpendingP50Minor = medianInt64(spendingSlice(paths))
	summary.TransactionCostP50Minor = medianInt64(txCostSlice(paths))
	if summary.TruncationPathRatio > 0.001 {
		summary.ModelWarnings = append(summary.ModelWarnings, "超过 0.1% 的路径出现收益截断，请关注尾部风险")
	}
	summary.ModelWarnings = append(summary.ModelWarnings, collectDataWarnings(in)...)
	applyCorrelationModelSummary(&summary, in)

	rep := pickRepresentativePaths(paths, summary.TerminalQuantiles)

	return RunResult{
		HorizonMonths:      horizon,
		SuccessCount:       success,
		FailureCount:       len(paths) - success,
		Summary:            summary,
		Paths:              paths,
		QuantileSeries:     summary.MonthlyWealthQuantiles,
		RealQuantileSeries: summary.RealMonthlyWealthQuantiles,
		Representative:     rep,
	}
}

// applyCorrelationModelSummary replaces the legacy independent-factor disclaimer
// with an accurate description when the run uses the joint factor model, and
// surfaces the frozen correlation/PSD-repair warnings.
func applyCorrelationModelSummary(summary *Summary, in *InputSnapshot) {
	if in.FactorModel == nil {
		return
	}
	audit := in.FactorModel.Audit
	priorOnly := len(audit.PriorOnlyPairs) > 0
	if priorOnly {
		summary.CorrelationDisclaimer = "资产间相关性主要来自假设集先验（历史共同月份不足），" +
			"已使用联合厚尾因子模型，分散化结果偏保守"
	} else {
		summary.CorrelationDisclaimer = "已使用基金/FX 间相关性的联合厚尾因子模型，" +
			"极端月度收益会共同发生"
	}
	for _, w := range audit.Warnings {
		summary.ModelWarnings = append(summary.ModelWarnings, factorWarningText(w, audit.MaxRepairDelta))
	}
	if priorOnly {
		summary.ModelWarnings = append(summary.ModelWarnings,
			"部分资产相关性依赖先验，请在“模拟假设”核对相关性矩阵")
	}
}

func factorWarningText(code string, repair float64) string {
	switch code {
	case "correlation_psd_repair_significant":
		return "相关性矩阵 PSD 修复幅度较大（" +
			strconvFloat(repair) + "），联合风险结果可能受影响"
	default:
		return code
	}
}

func strconvFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}

// realWealthSeries deflates a path's nominal monthly wealth by its own realized
// cumulative inflation, month by month, into start-of-plan purchasing power.
func realWealthSeries(nominal []int64, cumInflation []float64) []int64 {
	out := make([]int64, len(nominal))
	for i, v := range nominal {
		factor := 1.0
		if i < len(cumInflation) {
			factor = cumInflation[i]
		}
		out[i] = deflate(v, factor)
	}
	return out
}

func terminalQuantiles(vals []float64) map[string]int64 {
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	return map[string]int64{
		"p00": int64(math.Round(Quantile(sorted, 0))),
		"p25": int64(math.Round(Quantile(sorted, 0.25))),
		"p50": int64(math.Round(Quantile(sorted, 0.50))),
		"p75": int64(math.Round(Quantile(sorted, 0.75))),
		"p95": int64(math.Round(Quantile(sorted, 0.95))),
	}
}

func monthlyQuantileSeries(byPath [][]int64) []QuantilePoint {
	if len(byPath) == 0 {
		return nil
	}
	horizon := len(byPath[0])
	out := make([]QuantilePoint, horizon)
	for m := 0; m < horizon; m++ {
		vals := make([]float64, len(byPath))
		for i := range byPath {
			vals[i] = float64(byPath[i][m])
		}
		sort.Float64s(vals)
		out[m] = QuantilePoint{
			MonthOffset: m,
			P00Minor:    int64(math.Round(Quantile(vals, 0))),
			P05Minor:    int64(math.Round(Quantile(vals, 0.05))),
			P25Minor:    int64(math.Round(Quantile(vals, 0.25))),
			P50Minor:    int64(math.Round(Quantile(vals, 0.50))),
			P75Minor:    int64(math.Round(Quantile(vals, 0.75))),
			P95Minor:    int64(math.Round(Quantile(vals, 0.95))),
		}
	}
	return out
}

// Quantile computes linear-interpolation quantile; p00 is minimum.
func Quantile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	h := (float64(n) - 1) * p
	lo := int(math.Floor(h))
	hi := int(math.Ceil(h))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (h-float64(lo))*(sorted[hi]-sorted[lo])
}

// WilsonInterval returns Wilson score interval for binomial proportion.
func WilsonInterval(successes, n int, z float64) (float64, float64) {
	if n == 0 {
		return 0, 0
	}
	p := float64(successes) / float64(n)
	z2 := z * z
	denom := 1 + z2/float64(n)
	center := (p + z2/(2*float64(n))) / denom
	margin := z * math.Sqrt((p*(1-p)/float64(n))+z2/(4*float64(n)*float64(n))) / denom
	low := center - margin
	high := center + margin
	if low < 0 {
		low = 0
	}
	if high > 1 {
		high = 1
	}
	return low, high
}

func failureReasonCounts(paths []PathSummary) map[string]int {
	out := make(map[string]int)
	for _, p := range paths {
		if p.Succeeded {
			continue
		}
		r := p.FailureReason
		if r == "" {
			r = FailureOther
		}
		out[r]++
	}
	return out
}

func splitTerminals(paths []PathSummary) ([]int64, []int64) {
	var success, failure []int64
	for _, p := range paths {
		if p.Succeeded {
			success = append(success, p.TerminalWealthMinor)
		} else {
			failure = append(failure, p.TerminalWealthMinor)
		}
	}
	return success, failure
}

func failureYearQuantiles(paths []PathSummary, startAge int) map[string]float64 {
	var years []float64
	for _, p := range paths {
		if p.Succeeded || p.FailureMonth == nil {
			continue
		}
		years = append(years, float64(startAge+*p.FailureMonth/12))
	}
	if len(years) == 0 {
		return nil
	}
	sort.Float64s(years)
	return map[string]float64{
		"p25": Quantile(years, 0.25),
		"p50": Quantile(years, 0.50),
		"p75": Quantile(years, 0.75),
	}
}

func drawdownQuantiles(paths []PathSummary) map[string]float64 {
	vals := make([]float64, len(paths))
	for i, p := range paths {
		vals[i] = p.MaxDrawdown
	}
	sort.Float64s(vals)
	return map[string]float64{
		"p50": Quantile(vals, 0.50),
		"p95": Quantile(vals, 0.95),
	}
}

func spendingSlice(paths []PathSummary) []int64 {
	out := make([]int64, len(paths))
	for i, p := range paths {
		out[i] = p.TotalSpendingMinor
	}
	return out
}

func txCostSlice(paths []PathSummary) []int64 {
	out := make([]int64, len(paths))
	for i, p := range paths {
		out[i] = p.TransactionCostMinor
	}
	return out
}

func medianInt64(vals []int64) int64 {
	if len(vals) == 0 {
		return 0
	}
	cp := append([]int64(nil), vals...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	return cp[len(cp)/2]
}

func pickRepresentativePaths(paths []PathSummary, q map[string]int64) map[string]int {
	labels := []string{"p00", "p25", "p50", "p75", "p95"}
	out := make(map[string]int, len(labels))
	for _, label := range labels {
		target := q[label]
		best := paths[0].PathNo
		bestDist := abs64(paths[0].TerminalWealthMinor - target)
		for _, p := range paths {
			d := abs64(p.TerminalWealthMinor - target)
			if d < bestDist {
				bestDist = d
				best = p.PathNo
			}
		}
		out[label] = best
	}
	return out
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RegeneratePathDetail rebuilds one simulation path from its deterministic seed.
func RegeneratePathDetail(in *InputSnapshot, pathNo int) *PathDetail {
	_, detail := RunPath(in, pathNo, PathRunOpts{CollectDetail: true})
	return detail
}

func collectDataWarnings(in *InputSnapshot) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(warnings []string, instName, code string) {
		for _, w := range warnings {
			if w == "" {
				continue
			}
			var msg string
			if instName == "" {
				msg = w
			} else {
				msg = instName + "（" + code + "）" + w
			}
			if _, ok := seen[msg]; ok {
				continue
			}
			seen[msg] = struct{}{}
			out = append(out, msg)
		}
	}
	for _, a := range in.Assets {
		if a.IsCash {
			continue
		}
		add(a.DataWarnings, a.InstrumentName, a.InstrumentCode)
		add(a.FXDataWarnings, a.Currency+" FX", "")
	}
	return out
}
