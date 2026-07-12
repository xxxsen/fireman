package service

import (
	"container/heap"
	"errors"
	"fmt"
	"math"
	"sort"
)

const (
	TailRiskAlgorithmVersion  = "empirical_cvar_v1"
	DefaultTailRiskConfidence = 0.95
	DefaultTailRiskHorizon    = 20
)

var (
	errCVARConfidenceInvalid  = errors.New("cvar confidence must be 0.90, 0.95, or 0.99")
	errCVARHorizonInvalid     = errors.New("cvar horizon_days must be 1 or 20")
	errCVARSampleInsufficient = errors.New("cvar sample is insufficient")
	errCVARReturnInvalid      = errors.New("cvar return must be finite and greater than -100%")
)

// TailRiskSpec identifies one empirical CVaR measurement contract.
type TailRiskSpec struct {
	Confidence  float64 `json:"confidence"`
	HorizonDays int     `json:"horizon_days"`
}

// BacktestTailRisk is the frozen tail-risk summary for one backtest path.
type BacktestTailRisk struct {
	AlgorithmVersion string  `json:"algorithm_version"`
	Confidence       float64 `json:"confidence"`
	HorizonDays      int     `json:"horizon_days"`
	ScenarioCount    int     `json:"scenario_count"`
	TailCount        int     `json:"tail_count"`
	VaRLoss          float64 `json:"var_loss"`
	CVaRLoss         float64 `json:"cvar_loss"`
	WorstLoss        float64 `json:"worst_loss"`
}

func DefaultTailRiskSpec() TailRiskSpec {
	return TailRiskSpec{Confidence: DefaultTailRiskConfidence, HorizonDays: DefaultTailRiskHorizon}
}

// CanonicalTailRiskSpec validates and normalizes supported floating values.
func CanonicalTailRiskSpec(spec TailRiskSpec) (TailRiskSpec, error) {
	confidence, ok := canonicalTailConfidence(spec.Confidence)
	if !ok {
		return TailRiskSpec{}, errCVARConfidenceInvalid
	}
	if spec.HorizonDays != 1 && spec.HorizonDays != 20 {
		return TailRiskSpec{}, errCVARHorizonInvalid
	}
	return TailRiskSpec{Confidence: confidence, HorizonDays: spec.HorizonDays}, nil
}

func tailRiskErrorCode(err error) string {
	switch {
	case errors.Is(err, errCVARConfidenceInvalid):
		return "cvar_confidence_invalid"
	case errors.Is(err, errCVARHorizonInvalid):
		return "cvar_horizon_invalid"
	case errors.Is(err, errCVARSampleInsufficient):
		return ResearchReasonCVARSample
	case errors.Is(err, errCVARReturnInvalid):
		return "cvar_return_invalid"
	default:
		return "cvar_config_invalid"
	}
}

func tailRiskAppError(err error) *AppError {
	return newErr(tailRiskErrorCode(err), err.Error(), nil)
}

func canonicalTailConfidence(value float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	for _, allowed := range []float64{0.90, 0.95, 0.99} {
		if math.Abs(value-allowed) <= 1e-12 {
			return allowed, true
		}
	}
	return 0, false
}

func tailPercent(confidence float64) int {
	switch confidence {
	case 0.90:
		return 10
	case 0.95:
		return 5
	case 0.99:
		return 1
	default:
		return 0
	}
}

func MinimumTailRiskScenarios(confidence float64) int {
	switch confidence {
	case 0.90:
		return 50
	case 0.95:
		return 100
	case 0.99:
		return 500
	default:
		return 0
	}
}

func TailRiskScenarioCount(effectiveReturnCount, horizonDays int) int {
	if effectiveReturnCount < horizonDays || horizonDays <= 0 {
		return 0
	}
	return effectiveReturnCount - horizonDays + 1
}

// ComputeEmpiricalCVaR calculates exact empirical expected shortfall over
// overlapping compounded holding-period returns.
func ComputeEmpiricalCVaR(effectiveReturns []float64, rawSpec TailRiskSpec) (BacktestTailRisk, error) {
	spec, err := CanonicalTailRiskSpec(rawSpec)
	if err != nil {
		return BacktestTailRisk{}, err
	}
	losses, err := holdingPeriodLosses(effectiveReturns, spec.HorizonDays)
	if err != nil {
		return BacktestTailRisk{}, err
	}
	minimum := MinimumTailRiskScenarios(spec.Confidence)
	if len(losses) < minimum {
		return BacktestTailRisk{}, fmt.Errorf(
			"%w: scenarios=%d minimum=%d", errCVARSampleInsufficient, len(losses), minimum,
		)
	}
	return computeTailLossStats(losses, spec), nil
}

func computeTailLossStats(losses []float64, spec TailRiskSpec) BacktestTailRisk {
	tailUnits := tailPercent(spec.Confidence) * len(losses)
	k := tailUnits / 100
	remainder := tailUnits % 100
	tailCount := (tailUnits + 99) / 100
	tail := largestLosses(losses, tailCount)
	sort.Float64s(tail)

	sum := 0.0
	for i := 0; i < k; i++ {
		sum += tail[len(tail)-1-i]
	}
	if remainder > 0 {
		sum += (float64(remainder) / 100) * tail[len(tail)-1-k]
	}
	mass := float64(k) + float64(remainder)/100
	return BacktestTailRisk{
		AlgorithmVersion: TailRiskAlgorithmVersion,
		Confidence:       spec.Confidence,
		HorizonDays:      spec.HorizonDays,
		ScenarioCount:    len(losses),
		TailCount:        tailCount,
		VaRLoss:          tail[0],
		CVaRLoss:         sum / mass,
		WorstLoss:        tail[len(tail)-1],
	}
}

func holdingPeriodLosses(returns []float64, horizon int) ([]float64, error) {
	for _, r := range returns {
		if math.IsNaN(r) || math.IsInf(r, 0) || r <= -1 {
			return nil, errCVARReturnInvalid
		}
	}
	count := TailRiskScenarioCount(len(returns), horizon)
	if count == 0 {
		return nil, fmt.Errorf("%w: effective_returns=%d horizon=%d", errCVARSampleInsufficient, len(returns), horizon)
	}
	if horizon == 1 {
		losses := make([]float64, len(returns))
		for i, singleReturn := range returns {
			losses[i] = -singleReturn
		}
		return losses, nil
	}
	losses := make([]float64, count)
	product := 1.0
	for i := 0; i < horizon; i++ {
		product *= 1 + returns[i]
	}
	if math.IsNaN(product) || math.IsInf(product, 0) {
		return nil, errCVARReturnInvalid
	}
	losses[0] = 1 - product
	for end := horizon; end < len(returns); end++ {
		product /= 1 + returns[end-horizon]
		product *= 1 + returns[end]
		if math.IsNaN(product) || math.IsInf(product, 0) {
			return nil, errCVARReturnInvalid
		}
		losses[end-horizon+1] = 1 - product
	}
	return losses, nil
}

type lossMinHeap []float64

func (h lossMinHeap) Len() int           { return len(h) }
func (h lossMinHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h lossMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *lossMinHeap) Push(value any) {
	loss, ok := value.(float64)
	if !ok {
		panic("lossMinHeap accepts only float64")
	}
	*h = append(*h, loss)
}

func (h *lossMinHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

func largestLosses(losses []float64, count int) []float64 {
	h := lossMinHeap(append([]float64(nil), losses[:count]...))
	heap.Init(&h)
	for _, loss := range losses[count:] {
		if loss > h[0] {
			h[0] = loss
			heap.Fix(&h, 0)
		}
	}
	return []float64(h)
}
