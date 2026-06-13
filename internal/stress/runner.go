package stress

import (
	"math"
	"sort"

	"github.com/fireman/fireman/internal/simulation"
)

// ScenarioResult is the output for one stress scenario.
type ScenarioResult struct {
	ScenarioID            string  `json:"scenario_id"`
	ScenarioName          string  `json:"scenario_name"`
	Description           string  `json:"description"`
	RiskHint              string  `json:"risk_hint"`
	SuccessProbability    float64 `json:"success_probability"`
	BaselineDelta         float64 `json:"baseline_delta"`
	TerminalP25Minor      int64   `json:"terminal_p25_minor"`
	TerminalP50Minor      int64   `json:"terminal_p50_minor"`
	TerminalP95Minor      int64   `json:"terminal_p95_minor"`
	MaxDrawdownP95        float64 `json:"max_drawdown_p95"`
	FailureYearP50        float64 `json:"failure_year_p50,omitempty"`
	RecoveryMonthP50      *int    `json:"recovery_month_p50,omitempty"`
	RecoveryNotWithinPlan bool    `json:"recovery_not_within_plan"`
}

// Report is the full stress test output.
type Report struct {
	BaselineSuccessProbability float64          `json:"baseline_success_probability"`
	Runs                       int              `json:"runs"`
	Seed                       string           `json:"seed"`
	Scenarios                  []ScenarioResult `json:"scenarios"`
	WorstScenarioID            string           `json:"worst_scenario_id,omitempty"`
}

// RunOptions configures stress execution.
type RunOptions struct {
	Runs        int
	Progress    func(done, total int, phase string)
	CancelCheck func() bool
}

// Run executes all built-in scenarios with common random numbers against the frozen input.
func Run(in *simulation.InputSnapshot, opt RunOptions) Report {
	runs := opt.Runs
	if runs <= 0 {
		runs = in.Parameters.SimulationRuns
	}

	baseline := runPaths(in, runs, nil, opt)
	baselineSuccess := float64(baseline.success) / float64(runs)

	scenarios := BuiltinScenarios()
	results := make([]ScenarioResult, len(scenarios))
	worstID := ""
	worstSuccess := 2.0

	totalSteps := len(scenarios) + 1
	step := 1
	if opt.Progress != nil {
		opt.Progress(step, totalSteps, "baseline")
	}

	for i, sc := range scenarios {
		if opt.CancelCheck != nil && opt.CancelCheck() {
			break
		}
		sched := CompileSchedule(sc.ID, in)
		out := runPaths(in, runs, sched, opt)
		sr := aggregateScenario(sc, out, baselineSuccess, in.Parameters.CurrentAge)
		results[i] = sr
		if sr.SuccessProbability < worstSuccess {
			worstSuccess = sr.SuccessProbability
			worstID = sc.ID
		}
		step++
		if opt.Progress != nil {
			opt.Progress(step, totalSteps, "stress:"+sc.ID)
		}
	}

	return Report{
		BaselineSuccessProbability: baselineSuccess,
		Runs:                       runs,
		Seed:                       in.Parameters.Seed,
		Scenarios:                  results,
		WorstScenarioID:            worstID,
	}
}

type pathBatch struct {
	success        int
	terminals      []float64
	drawdowns      []float64
	failureYears   []float64
	recoveryMonths []float64
}

func runPaths(in *simulation.InputSnapshot, runs int, sched simulation.ShockSchedule, opt RunOptions) pathBatch {
	var batch pathBatch
	batch.terminals = make([]float64, runs)
	batch.drawdowns = make([]float64, runs)
	if sched != nil {
		batch.recoveryMonths = make([]float64, runs)
		for i := range batch.recoveryMonths {
			batch.recoveryMonths[i] = math.MaxFloat64
		}
	}
	shockEnd := ShockEndMonth(sched)
	shockStart := shockStartMonth(in)
	for m := range sched {
		if shockStart < 0 || m < shockStart {
			shockStart = m
		}
	}

	for i := 0; i < runs; i++ {
		if opt.CancelCheck != nil && opt.CancelCheck() {
			break
		}
		ps, _ := simulation.RunPath(in, i, simulation.PathRunOpts{
			CollectMonthlyWealth: sched != nil,
			Shocks:               sched,
		})
		if ps.Succeeded {
			batch.success++
		}
		batch.terminals[i] = float64(ps.TerminalWealthMinor)
		batch.drawdowns[i] = ps.MaxDrawdown
		if !ps.Succeeded && ps.FailureMonth != nil {
			batch.failureYears = append(batch.failureYears, float64(in.Parameters.CurrentAge+*ps.FailureMonth/12))
		}
		recordPathRecovery(&batch, i, in, ps, sched, shockStart, shockEnd)
	}
	return batch
}

func recordPathRecovery(
	batch *pathBatch,
	i int,
	in *simulation.InputSnapshot,
	ps simulation.PathSummary,
	sched simulation.ShockSchedule,
	shockStart, shockEnd int,
) {
	if sched == nil || len(ps.MonthlyWealthMinor) <= shockEnd {
		return
	}
	var recoveryTarget int64
	if shockStart > 0 {
		recoveryTarget = wealthAt(ps.MonthlyWealthMinor, shockStart-1)
	} else {
		recoveryTarget = in.Parameters.TotalAssetsMinor
	}
	rec := recoveryMonth(ps.MonthlyWealthMinor, shockEnd+1, recoveryTarget)
	if rec >= 0 {
		batch.recoveryMonths[i] = float64(rec)
	}
}

func wealthAt(series []int64, month int) int64 {
	if month < 0 || month >= len(series) {
		return 0
	}
	return series[month]
}

func recoveryMonth(series []int64, after int, target int64) int {
	if target <= 0 {
		return 0
	}
	for m := after; m < len(series); m++ {
		if series[m] >= target {
			return m - after
		}
	}
	return -1
}

func aggregateScenario(sc Scenario, batch pathBatch, baselineSuccess float64, _ int) ScenarioResult {
	runs := len(batch.terminals)
	if runs == 0 {
		return ScenarioResult{ScenarioID: sc.ID, ScenarioName: sc.Name, Description: sc.Description, RiskHint: sc.RiskHint}
	}
	sort.Float64s(batch.terminals)
	sort.Float64s(batch.drawdowns)

	successProb := float64(batch.success) / float64(runs)
	sr := ScenarioResult{
		ScenarioID:         sc.ID,
		ScenarioName:       sc.Name,
		Description:        sc.Description,
		RiskHint:           sc.RiskHint,
		SuccessProbability: successProb,
		BaselineDelta:      successProb - baselineSuccess,
		TerminalP25Minor:   int64(math.Round(simulation.Quantile(batch.terminals, 0.25))),
		TerminalP50Minor:   int64(math.Round(simulation.Quantile(batch.terminals, 0.50))),
		TerminalP95Minor:   int64(math.Round(simulation.Quantile(batch.terminals, 0.95))),
		MaxDrawdownP95:     simulation.Quantile(batch.drawdowns, 0.95),
	}
	if len(batch.failureYears) > 0 {
		sort.Float64s(batch.failureYears)
		sr.FailureYearP50 = simulation.Quantile(batch.failureYears, 0.50)
	}
	if len(batch.recoveryMonths) > 0 {
		rec, withinPlan := recoveryP50(batch.recoveryMonths)
		if withinPlan {
			sr.RecoveryMonthP50 = rec
		} else {
			sr.RecoveryNotWithinPlan = true
		}
	}
	return sr
}

func recoveryP50(recoveryMonths []float64) (*int, bool) {
	n := len(recoveryMonths)
	if n == 0 {
		return nil, false
	}
	sorted := append([]float64(nil), recoveryMonths...)
	sort.Float64s(sorted)
	rankIdx := int(math.Ceil(float64(n)*0.50)) - 1
	if rankIdx < 0 {
		rankIdx = 0
	}
	if sorted[rankIdx] >= math.MaxFloat64 {
		return nil, false
	}
	rec := int(math.Round(sorted[rankIdx]))
	return &rec, true
}
