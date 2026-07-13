package simulation

import (
	"context"
	"math"
	"sort"
)

// OutcomeEvaluation is the compact result used by search-style analyses that
// need path outcomes but not monthly wealth series.
type OutcomeEvaluation struct {
	Runs               int     `json:"runs"`
	SuccessCount       int     `json:"success_count"`
	SuccessProbability float64 `json:"success_probability"`
	SuccessWilsonLow   float64 `json:"success_wilson_low"`
	SuccessWilsonHigh  float64 `json:"success_wilson_high"`
	TerminalP50Minor   int64   `json:"terminal_p50_minor"`
	MaxDrawdownP95     float64 `json:"max_drawdown_p95"`
	Outcomes           []bool  `json:"outcomes,omitempty"`
}

// EvaluateOutcomes executes the same deterministic paths as Run while keeping
// only the aggregates needed by plan-improvement searches.
func EvaluateOutcomes(in *InputSnapshot, opt RunOptions) (OutcomeEvaluation, error) {
	runs := opt.Runs
	if runs <= 0 {
		runs = in.Parameters.SimulationRuns
	}
	if runs <= 0 {
		return OutcomeEvaluation{}, nil
	}
	terminals := make([]float64, 0, runs)
	drawdowns := make([]float64, 0, runs)
	outcomes := make([]bool, 0, runs)
	successes := 0
	for pathNo := 0; pathNo < runs; pathNo++ {
		if opt.CancelCheck != nil && opt.CancelCheck() {
			return OutcomeEvaluation{}, context.Canceled
		}
		path, _ := RunPath(in, pathNo, PathRunOpts{CollectMonthlyWealth: false})
		outcomes = append(outcomes, path.Succeeded)
		terminals = append(terminals, float64(path.TerminalWealthMinor))
		drawdowns = append(drawdowns, path.MaxDrawdown)
		if path.Succeeded {
			successes++
		}
		if opt.Progress != nil && (pathNo+1)%maxInt(1, runs/100) == 0 {
			opt.Progress(pathNo+1, runs, "evaluating")
		}
	}
	sort.Float64s(terminals)
	sort.Float64s(drawdowns)
	low, high := WilsonInterval(successes, runs, 1.96)
	return OutcomeEvaluation{
		Runs: runs, SuccessCount: successes,
		SuccessProbability: float64(successes) / float64(runs),
		SuccessWilsonLow:   low, SuccessWilsonHigh: high,
		TerminalP50Minor: int64(math.Round(Quantile(terminals, 0.5))),
		MaxDrawdownP95:   Quantile(drawdowns, 0.95), Outcomes: outcomes,
	}, nil
}
