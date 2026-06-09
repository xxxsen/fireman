package simulation

import "math"

// WithdrawalPlanner computes retirement spending for the configured strategy.
type WithdrawalPlanner struct {
	Type           string
	AnnualSpending int64
	WithdrawalRate float64
	FloorRatio     float64
	CeilingRatio   float64
	WealthAtRetire int64
	InitialRate    float64
	ProposedAnnual float64
}

func NewWithdrawalPlanner(wType string, annualSpending int64, rate, floor, ceiling float64) WithdrawalPlanner {
	return WithdrawalPlanner{
		Type: wType, AnnualSpending: annualSpending,
		WithdrawalRate: rate, FloorRatio: floor, CeilingRatio: ceiling,
	}
}

func (w *WithdrawalPlanner) InitAtRetirement(wealth int64) {
	w.WealthAtRetire = wealth
	if w.WealthAtRetire > 0 {
		w.InitialRate = float64(w.AnnualSpending) / float64(w.WealthAtRetire)
	}
}

func (w *WithdrawalPlanner) MonthlySpending(month, retirementMonth int, monthStartWealth int64, inflCumulative float64, isRetirementAnniversary bool) int64 {
	if month < retirementMonth {
		return 0
	}
	switch w.Type {
	case "fixed_portfolio":
		return int64(math.Round(float64(monthStartWealth) * w.WithdrawalRate / 12))
	case "guardrail":
		inflBase := float64(w.AnnualSpending) * inflCumulative
		if isRetirementAnniversary || w.ProposedAnnual == 0 {
			w.ProposedAnnual = inflBase
		}
		if isRetirementAnniversary && w.WealthAtRetire > 0 {
			yearStartWealth := float64(monthStartWealth)
			if yearStartWealth > 0 {
				currentRate := w.ProposedAnnual / yearStartWealth
				switch {
				case currentRate > 1.2*w.InitialRate:
					w.ProposedAnnual *= 0.90
				case currentRate < 0.8*w.InitialRate:
					w.ProposedAnnual *= 1.10
				}
			}
			floor := inflBase * w.FloorRatio
			ceil := inflBase * w.CeilingRatio
			if w.ProposedAnnual < floor {
				w.ProposedAnnual = floor
			}
			if w.ProposedAnnual > ceil {
				w.ProposedAnnual = ceil
			}
		}
		return int64(math.Round(w.ProposedAnnual / 12))
	default: // fixed_real
		return int64(math.Round(float64(w.AnnualSpending) * inflCumulative / 12))
	}
}

// GrossWithdrawal applies the effective withdrawal-tax approximation.
func GrossWithdrawal(netSpending int64, taxRate, taxableRatio float64) (gross int64, tax int64) {
	denom := 1 - taxRate*taxableRatio
	if denom <= 0 {
		return netSpending, 0
	}
	g := float64(netSpending) / denom
	gross = int64(math.Ceil(g))
	tax = gross - netSpending
	if tax < 0 {
		tax = 0
	}
	return gross, tax
}
