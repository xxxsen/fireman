package sensitivity

import (
	"math"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

// ParameterID identifies a sensitivity-analysis dimension.
const (
	ParamInitialAssets  = "initial_total_assets"
	ParamAnnualSpending = "annual_spending"
	ParamFixedInflation = "fixed_inflation_rate"
	ParamNonCashReturn  = "non_cash_expected_return"
	ParamEquityWeight   = "equity_total_weight"
	ParamRetirementAge  = "retirement_age"
	ParamEndAge         = "end_age"
)

// PerturbationPoint is one evaluated sensitivity grid point.
type PerturbationPoint struct {
	ParameterID   string  `json:"parameter_id"`
	ParameterName string  `json:"parameter_name"`
	Label         string  `json:"label"`
	Delta         float64 `json:"delta"`
	DeltaUnit     string  `json:"delta_unit"`
}

// DefaultPerturbations returns the seven default parameter grids.
func DefaultPerturbations() []PerturbationPoint {
	out := make([]PerturbationPoint, 0, 35)
	out = append(out, pctPoints(ParamInitialAssets, "初始总资产", []float64{-0.20, -0.10, 0, 0.10, 0.20})...)
	out = append(out, pctPoints(ParamAnnualSpending, "退休后年支出", []float64{-0.20, -0.10, 0, 0.10, 0.20})...)
	out = append(out, ppPoints(ParamFixedInflation, "固定通胀率", []float64{-0.02, -0.01, 0, 0.01, 0.02})...)
	out = append(out, ppPoints(ParamNonCashReturn, "非现金资产期望收益", []float64{-0.02, -0.01, 0, 0.01, 0.02})...)
	out = append(out, ppPoints(ParamEquityWeight, "权益总权重", []float64{-0.20, -0.10, 0, 0.10, 0.20})...)
	out = append(out, yearPoints(ParamRetirementAge, "退休年龄", []int{-5, -2, 0, 2, 5})...)
	out = append(out, yearPoints(ParamEndAge, "规划终止年龄", []int{-10, -5, 0, 5, 10})...)
	return out
}

func pctPoints(id, name string, deltas []float64) []PerturbationPoint {
	out := make([]PerturbationPoint, len(deltas))
	for i, d := range deltas {
		out[i] = PerturbationPoint{
			ParameterID: id, ParameterName: name, Delta: d, DeltaUnit: "relative",
			Label: formatPctLabel(d),
		}
	}
	return out
}

func ppPoints(id, name string, deltas []float64) []PerturbationPoint {
	out := make([]PerturbationPoint, len(deltas))
	for i, d := range deltas {
		out[i] = PerturbationPoint{
			ParameterID: id, ParameterName: name, Delta: d, DeltaUnit: "pp",
			Label: formatPPLabel(d),
		}
	}
	return out
}

func yearPoints(id, name string, deltas []int) []PerturbationPoint {
	out := make([]PerturbationPoint, len(deltas))
	for i, d := range deltas {
		out[i] = PerturbationPoint{
			ParameterID: id, ParameterName: name, Delta: float64(d), DeltaUnit: "years",
			Label: formatYearLabel(d),
		}
	}
	return out
}

func formatPctLabel(d float64) string {
	if d == 0 {
		return "基准"
	}
	return formatSigned(d*100) + "%"
}

func formatPPLabel(d float64) string {
	if d == 0 {
		return "基准"
	}
	return formatSigned(d*100) + "pp"
}

func formatYearLabel(d int) string {
	if d == 0 {
		return "基准"
	}
	if d > 0 {
		return "+" + itoa(d) + "年"
	}
	return itoa(d) + "年"
}

func formatSigned(v float64) string {
	s := ""
	if v > 0 {
		s = "+"
	}
	// one decimal for clean display
	iv := int(math.Round(v * 10))
	if iv%10 == 0 {
		return s + itoa(iv/10)
	}
	return s + itoa(iv/10) + "." + itoa(iv%10)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ApplyPerturbation returns a copy of the input with one perturbation applied.
func ApplyPerturbation(base *simulation.InputSnapshot, pt PerturbationPoint) (*simulation.InputSnapshot, error) {
	cp := cloneSnapshot(base)
	switch pt.ParameterID {
	case ParamInitialAssets:
		applyInitialAssetsPerturbation(&cp, pt.Delta)
	case ParamAnnualSpending:
		applyAnnualSpendingPerturbation(&cp, pt.Delta)
	case ParamFixedInflation:
		applyFixedInflationPerturbation(&cp, pt.Delta)
	case ParamNonCashReturn:
		applyNonCashReturnPerturbation(&cp, pt.Delta)
	case ParamEquityWeight:
		applyEquityWeightDelta(&cp, pt.Delta)
	case ParamRetirementAge:
		applyRetirementAgePerturbation(&cp, pt.Delta)
	case ParamEndAge:
		applyEndAgePerturbation(&cp, pt.Delta)
	}
	return &cp, nil
}

func applyEquityWeightDelta(in *simulation.InputSnapshot, delta float64) {
	var equity, bond, cash float64
	for _, a := range in.Assets {
		switch a.AssetClass {
		case domain.AssetClassEquity:
			equity += a.TargetWeight
		case domain.AssetClassBond:
			bond += a.TargetWeight
		case domain.AssetClassCash:
			cash += a.TargetWeight
		}
	}
	newEquity := equity + delta
	if newEquity < 0 {
		newEquity = 0
	}
	maxEquity := 1 - cash
	if newEquity > maxEquity {
		newEquity = maxEquity
	}
	newBond := 1 - cash - newEquity
	if newBond < 0 {
		newBond = 0
	}
	if equity > 0 {
		eqScale := newEquity / equity
		for i := range in.Assets {
			if in.Assets[i].AssetClass == domain.AssetClassEquity {
				in.Assets[i].TargetWeight *= eqScale
			}
		}
	}
	if bond > 0 {
		bdScale := newBond / bond
		for i := range in.Assets {
			if in.Assets[i].AssetClass == domain.AssetClassBond {
				in.Assets[i].TargetWeight *= bdScale
			}
		}
	}
}

func cloneSnapshot(in *simulation.InputSnapshot) simulation.InputSnapshot {
	cp := *in
	cp.Assets = append([]simulation.SnapshotAsset(nil), in.Assets...)
	return cp
}

// HeatmapSpendingDeltas are the spending axis for the 5x5 heatmap.
var HeatmapSpendingDeltas = []float64{-0.20, -0.10, 0, 0.10, 0.20}

// HeatmapReturnDeltas are the return axis for the 5x5 heatmap.
var HeatmapReturnDeltas = []float64{-0.02, -0.01, 0, 0.01, 0.02}
