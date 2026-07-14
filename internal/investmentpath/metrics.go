package investmentpath

import (
	"math"
	"sort"
)

func solveXIRR(flows []cashFlow) (*float64, string) {
	if len(flows) < 2 {
		return nil, "insufficient_cash_flows"
	}
	hasNegative, hasPositive := false, false
	for _, flow := range flows {
		hasNegative = hasNegative || flow.amount < 0
		hasPositive = hasPositive || flow.amount > 0
	}
	if !hasNegative || !hasPositive {
		return nil, "cash_flows_do_not_change_sign"
	}
	base := flows[0].date
	npv := func(rate float64) float64 {
		total := 0.0
		for _, flow := range flows {
			years := flow.date.Sub(base).Hours() / 24 / 365
			total += float64(flow.amount) / math.Pow(1+rate, years)
		}
		return total
	}
	lo, hi := -0.999999, 1000.0
	flo, fhi := npv(lo), npv(hi)
	if math.IsNaN(flo) || math.IsNaN(fhi) || flo*fhi > 0 {
		return nil, "no_root_in_business_range"
	}
	for i := 0; i < 256; i++ {
		mid := (lo + hi) / 2
		fm := npv(mid)
		if math.Abs(fm) < 1e-8 || hi-lo < 1e-12 {
			return &mid, ""
		}
		if flo*fm <= 0 {
			hi, fhi = mid, fm
		} else {
			lo, flo = mid, fm
		}
		_ = fhi
	}
	return nil, "solver_did_not_converge"
}

func quantiles(values []float64) Quantiles {
	if len(values) == 0 {
		return Quantiles{}
	}
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	return Quantiles{P10: percentile(values, .1), P50: percentile(values, .5), P90: percentile(values, .9)}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}
