package sensitivity

import (
	"context"
	"math"
	"sort"

	"github.com/fireman/fireman/internal/simulation"
)

// PointResult is one sensitivity grid evaluation.
type PointResult struct {
	PerturbationPoint
	SuccessProbability float64 `json:"success_probability"`
	BaselineDelta      float64 `json:"baseline_delta"`
	TerminalP50Minor   int64   `json:"terminal_p50_minor"`
	MaxDrawdownP95     float64 `json:"max_drawdown_p95"`
}

// TornadoBar is one row in the tornado chart sorted by impact range.
type TornadoBar struct {
	ParameterID   string  `json:"parameter_id"`
	ParameterName string  `json:"parameter_name"`
	LowLabel      string  `json:"low_label"`
	HighLabel     string  `json:"high_label"`
	LowSuccess    float64 `json:"low_success"`
	HighSuccess   float64 `json:"high_success"`
	Range         float64 `json:"range"`
}

// CurveSeries is the parameter curve for one dimension.
type CurveSeries struct {
	ParameterID   string        `json:"parameter_id"`
	ParameterName string        `json:"parameter_name"`
	Points        []PointResult `json:"points"`
}

// HeatmapCell is one cell in the spending × return heatmap.
type HeatmapCell struct {
	SpendingDelta      float64 `json:"spending_delta"`
	ReturnDelta        float64 `json:"return_delta"`
	SpendingLabel      string  `json:"spending_label"`
	ReturnLabel        string  `json:"return_label"`
	SuccessProbability float64 `json:"success_probability"`
}

// Report is the full sensitivity-analysis output.
type Report struct {
	BaselineSuccessProbability float64         `json:"baseline_success_probability"`
	MonteCarloStdError         float64         `json:"monte_carlo_std_error"`
	StdErrorHint               string          `json:"std_error_hint"`
	Runs                       int             `json:"runs"`
	Seed                       string          `json:"seed"`
	Points                     []PointResult   `json:"points"`
	Tornado                    []TornadoBar    `json:"tornado"`
	Curves                     []CurveSeries   `json:"curves"`
	Heatmap                    [][]HeatmapCell `json:"heatmap"`
}

// RunOptions configures sensitivity execution.
type RunOptions struct {
	Runs        int
	Progress    func(done, total int, phase string)
	CancelCheck func() bool
}

// Run evaluates default perturbations with common random numbers.
//
//nolint:gocognit,gocyclo,funlen // Cancellation checks intentionally remain adjacent to each expensive phase.
func Run(base *simulation.InputSnapshot, opt RunOptions) (Report, error) {
	runs := opt.Runs
	if runs <= 0 {
		runs = base.Parameters.SimulationRuns
	}

	baseline := evaluateSnapshot(base, runs, opt)
	if baseline.canceled || (opt.CancelCheck != nil && opt.CancelCheck()) {
		return Report{}, context.Canceled
	}
	baselineProb := float64(baseline.success) / float64(runs)
	stdErr := math.Sqrt(baselineProb * (1 - baselineProb) / float64(runs))

	perturbations := DefaultPerturbations()
	points := make([]PointResult, 0, len(perturbations))
	total := len(perturbations) + 1 + len(HeatmapSpendingDeltas)*len(HeatmapReturnDeltas)
	done := 1
	if opt.Progress != nil {
		opt.Progress(done, total, "baseline")
	}

	for _, pt := range perturbations {
		if opt.CancelCheck != nil && opt.CancelCheck() {
			return Report{}, context.Canceled
		}
		in, err := ApplyPerturbation(base, pt)
		if err != nil {
			return Report{}, err
		}
		out := evaluateSnapshot(in, runs, opt)
		if out.canceled || (opt.CancelCheck != nil && opt.CancelCheck()) {
			return Report{}, context.Canceled
		}
		prob := float64(out.success) / float64(runs)
		points = append(points, PointResult{
			PerturbationPoint:  pt,
			SuccessProbability: prob,
			BaselineDelta:      prob - baselineProb,
			TerminalP50Minor:   out.terminalP50,
			MaxDrawdownP95:     out.maxDrawdownP95,
		})
		done++
		if opt.Progress != nil {
			opt.Progress(done, total, "sensitivity:"+pt.ParameterID)
		}
	}

	heatmap := make([][]HeatmapCell, len(HeatmapSpendingDeltas))
	for si, sd := range HeatmapSpendingDeltas {
		if opt.CancelCheck != nil && opt.CancelCheck() {
			return Report{}, context.Canceled
		}
		row := make([]HeatmapCell, len(HeatmapReturnDeltas))
		for ri, rd := range HeatmapReturnDeltas {
			if opt.CancelCheck != nil && opt.CancelCheck() {
				return Report{}, context.Canceled
			}
			in, _ := ApplyPerturbation(base, PerturbationPoint{
				ParameterID: ParamAnnualSpending, Delta: sd, DeltaUnit: "relative",
			})
			in, _ = ApplyPerturbation(in, PerturbationPoint{
				ParameterID: ParamNonCashReturn, Delta: rd, DeltaUnit: "pp",
			})
			out := evaluateSnapshot(in, runs, opt)
			if out.canceled || (opt.CancelCheck != nil && opt.CancelCheck()) {
				return Report{}, context.Canceled
			}
			row[ri] = HeatmapCell{
				SpendingDelta: sd, ReturnDelta: rd,
				SpendingLabel: formatPctLabel(sd), ReturnLabel: formatPPLabel(rd),
				SuccessProbability: float64(out.success) / float64(runs),
			}
			done++
			if opt.Progress != nil {
				opt.Progress(done, total, "heatmap")
			}
		}
		heatmap[si] = row
	}

	return Report{
		BaselineSuccessProbability: baselineProb,
		MonteCarloStdError:         stdErr,
		StdErrorHint:               "蒙特卡洛标准误约为 ±" + formatSigned(stdErr*100) + " 个百分点；小于该幅度的成功率差异可能只是抽样噪声",
		Runs:                       runs,
		Seed:                       base.Parameters.Seed,
		Points:                     points,
		Tornado:                    buildTornado(points),
		Curves:                     buildCurves(points),
		Heatmap:                    heatmap,
	}, nil
}

type evalOut struct {
	canceled       bool
	success        int
	terminalP50    int64
	maxDrawdownP95 float64
}

func evaluateSnapshot(in *simulation.InputSnapshot, runs int, opt RunOptions) evalOut {
	result := simulation.Run(in, simulation.RunOptions{Runs: runs, CancelCheck: opt.CancelCheck})
	var out evalOut
	out.canceled = result.Canceled
	out.success = result.SuccessCount
	if q, ok := result.Summary.TerminalQuantiles["p50"]; ok {
		out.terminalP50 = q
	}
	if result.Summary.MaxDrawdownQuantiles != nil {
		out.maxDrawdownP95 = result.Summary.MaxDrawdownQuantiles["p95"]
	}
	return out
}

func buildTornado(points []PointResult) []TornadoBar {
	byParam := map[string][]PointResult{}
	order := []string{}
	seen := map[string]bool{}
	for _, p := range points {
		byParam[p.ParameterID] = append(byParam[p.ParameterID], p)
		if !seen[p.ParameterID] {
			seen[p.ParameterID] = true
			order = append(order, p.ParameterID)
		}
	}
	bars := make([]TornadoBar, 0, len(order))
	for _, id := range order {
		pts := byParam[id]
		if len(pts) == 0 {
			continue
		}
		low, high := pts[0], pts[0]
		for _, p := range pts[1:] {
			if p.SuccessProbability < low.SuccessProbability {
				low = p
			}
			if p.SuccessProbability > high.SuccessProbability {
				high = p
			}
		}
		bars = append(bars, TornadoBar{
			ParameterID: id, ParameterName: low.ParameterName,
			LowLabel: low.Label, HighLabel: high.Label,
			LowSuccess: low.SuccessProbability, HighSuccess: high.SuccessProbability,
			Range: high.SuccessProbability - low.SuccessProbability,
		})
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Range > bars[j].Range })
	return bars
}

func buildCurves(points []PointResult) []CurveSeries {
	byParam := map[string]*CurveSeries{}
	order := []string{}
	seen := map[string]bool{}
	for _, p := range points {
		if byParam[p.ParameterID] == nil {
			byParam[p.ParameterID] = &CurveSeries{ParameterID: p.ParameterID, ParameterName: p.ParameterName}
			if !seen[p.ParameterID] {
				seen[p.ParameterID] = true
				order = append(order, p.ParameterID)
			}
		}
		byParam[p.ParameterID].Points = append(byParam[p.ParameterID].Points, p)
	}
	out := make([]CurveSeries, 0, len(order))
	for _, id := range order {
		cs := *byParam[id]
		out = append(out, cs)
	}
	return out
}
