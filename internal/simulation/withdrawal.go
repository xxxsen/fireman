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
	// lastAnnualReal is the previous year's guardrail spending expressed in
	// retirement-start purchasing power (inflation stripped). Anniversary
	// adjustments compound on it, so consecutive ±10% cuts/raises accumulate
	// across years instead of resetting to the inflation baseline.
	lastAnnualReal float64
	// LegacyAnnualReset selects the guardrail semantics frozen into snapshots
	// created before the compounding fix (engine versions 2.0.0 / 3.0.0):
	// every anniversary resets the proposal to the inflation baseline instead
	// of compounding on last year's spending. It must be set from the input
	// snapshot's engine version so stored runs replay with the exact semantics
	// their persisted summaries were computed with.
	LegacyAnnualReset  bool
	stableIncomeAnnual float64
}

// RetirementSettlement is the requested cash-flow contract for one retirement
// month before the portfolio is touched. Stable income is already after tax, so
// only PortfolioNetNeededMinor is grossed up for withdrawal tax.
type RetirementSettlement struct {
	SpendingRequestedMinor   int64
	StableIncomeMinor        int64
	PortfolioNetNeededMinor  int64
	GrossWithdrawalMinor     int64
	WithdrawalTaxMinor       int64
	StableIncomeSurplusMinor int64
}

// WithdrawalResult records what the portfolio could actually fund. Failed
// paths use these values for their final-month ledger instead of recording the
// full requested spending.
type WithdrawalResult struct {
	Sufficient              bool
	GrossRequestedMinor     int64
	GrossFundedMinor        int64
	PortfolioNetFundedMinor int64
	TaxFundedMinor          int64
	TransactionCostMinor    int64
}

// SettleRetirementMonth nets after-tax stable income against living spending
// before calculating the taxable portfolio withdrawal.
func SettleRetirementMonth(
	spendingRequested, stableIncome int64,
	taxRate, taxableRatio float64,
) RetirementSettlement {
	spendingRequested = max(spendingRequested, 0)
	stableIncome = max(stableIncome, 0)
	netNeeded := max(spendingRequested-stableIncome, 0)
	gross, tax := GrossWithdrawal(netNeeded, taxRate, taxableRatio)
	return RetirementSettlement{
		SpendingRequestedMinor:   spendingRequested,
		StableIncomeMinor:        stableIncome,
		PortfolioNetNeededMinor:  netNeeded,
		GrossWithdrawalMinor:     gross,
		WithdrawalTaxMinor:       tax,
		StableIncomeSurplusMinor: max(stableIncome-spendingRequested, 0),
	}
}

func fundedWithdrawalResult(
	grossRequested, grossFunded, netNeeded int64,
	taxRate, taxableRatio float64,
	txCost int64,
) WithdrawalResult {
	effectiveTaxRate := taxRate * taxableRatio
	if effectiveTaxRate < 0 || effectiveTaxRate >= 1 {
		effectiveTaxRate = 0
	}
	portfolioNetFunded := int64(math.Floor(float64(grossFunded) * (1 - effectiveTaxRate)))
	portfolioNetFunded = min(max(portfolioNetFunded, 0), netNeeded)
	taxFunded := max(grossFunded-portfolioNetFunded, 0)
	return WithdrawalResult{
		Sufficient:              grossFunded >= grossRequested && portfolioNetFunded >= netNeeded,
		GrossRequestedMinor:     grossRequested,
		GrossFundedMinor:        grossFunded,
		PortfolioNetFundedMinor: portfolioNetFunded,
		TaxFundedMinor:          taxFunded,
		TransactionCostMinor:    txCost,
	}
}

// SetStableIncomeAnnual sets the current nominal annual after-tax retirement
// income used only by guardrail rate checks. It does not change total living
// spending or the floor/ceiling contract.
func (w *WithdrawalPlanner) SetStableIncomeAnnual(income int64) {
	w.stableIncomeAnnual = float64(max(income, 0))
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
		netNeed := math.Max(float64(w.AnnualSpending)-w.stableIncomeAnnual, 0)
		w.InitialRate = netNeed / float64(w.WealthAtRetire)
	}
	w.lastAnnualReal = float64(w.AnnualSpending)
}

func (w *WithdrawalPlanner) MonthlySpending(month, retirementMonth int, monthStartWealth int64, inflCumulative float64,
	isRetirementAnniversary bool,
) int64 {
	if month < retirementMonth {
		return 0
	}
	switch w.Type {
	case "fixed_portfolio":
		return int64(math.Round(float64(monthStartWealth) * w.WithdrawalRate / 12))
	case "guardrail":
		if w.LegacyAnnualReset {
			return w.legacyAnnualResetGuardrail(monthStartWealth, inflCumulative, isRetirementAnniversary)
		}
		if w.ProposedAnnual == 0 { // first retirement month
			w.ProposedAnnual = float64(w.AnnualSpending) * inflCumulative
			w.lastAnnualReal = float64(w.AnnualSpending)
		}
		if isRetirementAnniversary && w.WealthAtRetire > 0 {
			// Guyton-Klinger style: previous year's spending + inflation is the
			// base; the ±10% guardrail adjustment compounds on it across years.
			proposed := w.lastAnnualReal * inflCumulative
			yearStartWealth := float64(monthStartWealth)
			if yearStartWealth > 0 {
				currentRate := math.Max(proposed-w.stableIncomeAnnual, 0) / yearStartWealth
				switch {
				case currentRate > 1.2*w.InitialRate:
					proposed *= 0.90
				case currentRate < 0.8*w.InitialRate:
					proposed *= 1.10
				}
			}
			inflBase := float64(w.AnnualSpending) * inflCumulative
			proposed = math.Max(inflBase*w.FloorRatio, math.Min(inflBase*w.CeilingRatio, proposed))
			w.ProposedAnnual = proposed
			w.lastAnnualReal = proposed / inflCumulative
		}
		return int64(math.Round(w.ProposedAnnual / 12))
	default: // fixed_real
		return int64(math.Round(float64(w.AnnualSpending) * inflCumulative / 12))
	}
}

// legacyAnnualResetGuardrail is the guardrail behavior shipped in engine
// versions 2.0.0 and 3.0.0, kept verbatim so snapshots frozen at those
// versions replay bit-for-bit: each anniversary resets the proposal to the
// inflation-adjusted baseline, then applies a single ±10% adjustment and the
// floor/ceiling clamp, so cuts/raises never accumulate across years. Serves
// replay only — never wire it up for newly created snapshots.
func (w *WithdrawalPlanner) legacyAnnualResetGuardrail(
	monthStartWealth int64, inflCumulative float64, isRetirementAnniversary bool,
) int64 {
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
}

// GrossWithdrawal applies the effective withdrawal-tax approximation.
func GrossWithdrawal(netSpending int64, taxRate, taxableRatio float64) (int64, int64) {
	denom := 1 - taxRate*taxableRatio
	if denom <= 0 {
		return netSpending, 0
	}
	g := float64(netSpending) / denom
	gross := int64(math.Ceil(g))
	tax := gross - netSpending
	if tax < 0 {
		tax = 0
	}
	return gross, tax
}
