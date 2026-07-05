package simulation

import (
	"math"
	"strconv"
)

// Failure reason identifiers.
const (
	FailureEarlySequence = "early_sequence_risk"
	FailureHighInflation = "high_inflation"
	FailureSpendingShock = "spending_shock"
	FailureLongevity     = "longevity_risk"
	FailureOther         = "other"
)

// PathRunOpts controls optional outputs from RunPath.
type PathRunOpts struct {
	CollectDetail        bool
	CollectMonthlyWealth bool
	Shocks               ShockSchedule
}

// PathSummary is the compact outcome for one Monte Carlo path.
type PathSummary struct {
	PathNo               int
	PathSeed             int64
	Succeeded            bool
	FailureMonth         *int
	FailureReason        string
	TerminalWealthMinor  int64
	MaxDrawdown          float64
	TotalSpendingMinor   int64
	TransactionCostMinor int64
	TruncationCount      int
	MonthlyWealthMinor   []int64
	// MonthlyCumInflation is the path's realized cumulative inflation factor at
	// each month (1.0 at start). Collected alongside MonthlyWealthMinor so real
	// (start-of-plan purchasing power) wealth uses each path's own inflation
	// process, never an averaged rate.
	MonthlyCumInflation []float64
	// RealTerminalWealthMinor is TerminalWealthMinor deflated by the path's final
	// cumulative inflation.
	RealTerminalWealthMinor int64
}

// MonthRecord captures one month of path detail.
type MonthRecord struct {
	MonthOffset      int     `json:"month_offset"`
	TotalWealthMinor int64   `json:"total_wealth_minor"`
	SpendingMinor    int64   `json:"spending_minor"`
	IncomeMinor      int64   `json:"income_minor"`
	TaxMinor         int64   `json:"tax_minor"`
	TransactionCost  int64   `json:"transaction_cost"`
	Drawdown         float64 `json:"drawdown"`
	Rebalanced       bool    `json:"rebalanced"`
	// This path's realized cumulative inflation at month end and the
	// wealth deflated into start-of-plan purchasing power, so the UI can toggle
	// the amount caliber without re-deriving the inflation process.
	CumInflation         float64 `json:"cum_inflation"`
	RealTotalWealthMinor int64   `json:"real_total_wealth_minor"`
}

// YearRecord captures annual aggregates for path detail.
type YearRecord struct {
	Year               int     `json:"year"`
	StartWealthMinor   int64   `json:"start_wealth_minor"`
	IncomeMinor        int64   `json:"income_minor"`
	SpendingMinor      int64   `json:"spending_minor"`
	TaxMinor           int64   `json:"tax_minor"`
	TransactionCost    int64   `json:"transaction_cost"`
	InvestmentGainLoss int64   `json:"investment_gain_loss"`
	EndWealthMinor     int64   `json:"end_wealth_minor"`
	YearEndDrawdown    float64 `json:"year_end_drawdown"`
	MaxIntraYearDD     float64 `json:"max_intra_year_dd"`
	// AnnualReturn is the year's pure investment return relative to opening
	// wealth: InvestmentGainLoss / StartWealthMinor. Cash flows (income,
	// spending, tax, transaction cost) are excluded by construction, so it
	// reflects investment P&L only — not the year-end drawdown. It is nil when
	// StartWealthMinor <= 0 (no meaningful denominator), rendered as "—".
	AnnualReturn *float64           `json:"annual_return"`
	Rebalanced   bool               `json:"rebalanced"`
	AssetWeights map[string]float64 `json:"asset_weights"`
	// Real (start-of-plan purchasing power) opening/closing wealth,
	// deflated by this path's own cumulative inflation at year start/end.
	CumInflation         float64 `json:"cum_inflation"`
	RealStartWealthMinor int64   `json:"real_start_wealth_minor"`
	RealEndWealthMinor   int64   `json:"real_end_wealth_minor"`
}

// PathDetail is the fully regenerated monthly and annual path.
type PathDetail struct {
	PathNo        int           `json:"path_no"`
	PathSeed      string        `json:"path_seed"`
	Succeeded     bool          `json:"succeeded"`
	FailureMonth  *int          `json:"failure_month,omitempty"`
	FailureReason string        `json:"failure_reason,omitempty"`
	Monthly       []MonthRecord `json:"monthly"`
	Yearly        []YearRecord  `json:"yearly"`
}

type assetSlot struct {
	id           string
	isCash       bool
	balance      float64
	targetWeight float64
	returnParams AssetReturnParams
	fxParams     AssetReturnParams
	useFX        bool
}

// RunPath executes one deterministic path.
func RunPath(in *InputSnapshot, pathNo int, opts PathRunOpts) (PathSummary, *PathDetail) {
	pathSeed := DerivePathSeed(in.RootSeed(), pathNo)
	rng := NewRNG(pathSeed)

	horizon := in.HorizonMonths()
	retire := in.RetirementMonth()
	p := in.Parameters

	slots := make([]assetSlot, len(in.Assets))
	cashIdx := -1
	total := 0.0
	for i, a := range in.Assets {
		slots[i] = assetSlot{
			id: a.HoldingID, isCash: a.IsCash, balance: float64(a.InitialMinor),
			targetWeight: a.TargetWeight,
			returnParams: ParamsFromAnnual(a.ModeledAnnualReturn, a.AnnualVolatility),
		}
		if a.IsCash {
			cashIdx = i
		}
		if a.FXSnapshotID != "" && a.Currency != in.BaseCurrency {
			slots[i].useFX = true
			slots[i].fxParams = ParamsFromAnnual(a.FXModeledReturn, a.FXAnnualVolatility)
		}
		total += slots[i].balance
	}

	infl := NewInflationState(p.InflationMode, p.FixedInflationRate, p.InflationMu, p.InflationPhi, p.InflationSigma, rng)
	withdraw := NewWithdrawalPlanner(p.WithdrawalType, p.AnnualSpendingMinor, p.WithdrawalRate, p.WithdrawalFloorRatio,
		p.WithdrawalCeilingRatio)
	// Guardrail semantics are frozen per snapshot: replays of runs created
	// before the compounding fix keep their original annual-reset behavior so
	// regenerated paths stay consistent with the stored summary metrics.
	withdraw.LegacyAnnualReset = GuardrailUsesLegacyAnnualReset(in.EngineVersion)

	var detail *PathDetail
	if opts.CollectDetail {
		detail = &PathDetail{PathNo: pathNo, PathSeed: formatSeed(pathSeed)}
	}

	summary := PathSummary{PathNo: pathNo, PathSeed: pathSeed}
	state := pathSimState{summary: summary, detail: detail, peak: int64(math.Round(total))}
	state = runPathMonths(in, slots, cashIdx, horizon, retire, &infl, &withdraw, rng, opts, state)
	finalizePathSummary(&state.summary, slots, state, p.TerminalWealthFloorMinor)
	summary = state.summary
	detail = state.detail

	if opts.CollectMonthlyWealth {
		summary.MonthlyWealthMinor = padMonthlyWealth(summary.MonthlyWealthMinor, horizon)
		summary.MonthlyCumInflation = padCumInflation(summary.MonthlyCumInflation, horizon)
	}
	summary.RealTerminalWealthMinor = deflate(summary.TerminalWealthMinor, infl.Cumulative)

	if opts.CollectDetail {
		detail.Succeeded = summary.Succeeded
		detail.FailureReason = summary.FailureReason
		if summary.FailureMonth != nil {
			detail.FailureMonth = summary.FailureMonth
		}
	}

	return summary, detail
}

func slotWeights(slots []assetSlot) map[string]float64 {
	total := 0.0
	for _, s := range slots {
		total += s.balance
	}
	out := make(map[string]float64, len(slots))
	if total <= 0 {
		return out
	}
	for _, s := range slots {
		out[s.id] = s.balance / total
	}
	return out
}

func padMonthlyWealth(series []int64, horizon int) []int64 {
	if len(series) >= horizon {
		return series
	}
	out := make([]int64, horizon)
	copy(out, series)
	last := int64(0)
	if len(series) > 0 {
		last = series[len(series)-1]
	}
	for i := len(series); i < horizon; i++ {
		out[i] = last
	}
	return out
}

// padCumInflation extends a path's cumulative-inflation series to the horizon.
// A path that ended early (failure) keeps its last realized inflation factor for
// the padded months; the nominal wealth there is already 0, so real wealth is 0
// regardless of the factor used.
func padCumInflation(series []float64, horizon int) []float64 {
	if len(series) >= horizon {
		return series
	}
	out := make([]float64, horizon)
	copy(out, series)
	last := 1.0
	if len(series) > 0 {
		last = series[len(series)-1]
	}
	for i := len(series); i < horizon; i++ {
		out[i] = last
	}
	return out
}

// deflate converts a nominal minor amount to start-of-plan purchasing power.
func deflate(nominalMinor int64, cumInflation float64) int64 {
	if cumInflation <= 0 {
		return nominalMinor
	}
	return int64(math.Round(float64(nominalMinor) / cumInflation))
}

func totalWealth(slots []assetSlot) int64 {
	sum := 0.0
	for _, s := range slots {
		sum += s.balance
	}
	return int64(math.Round(sum))
}

func addCash(slots []assetSlot, cashIdx int, amount float64) {
	if cashIdx >= 0 {
		slots[cashIdx].balance += amount
		return
	}
	// No explicit cash: distribute by weights.
	total := 0.0
	for _, s := range slots {
		total += s.balance
	}
	for i := range slots {
		w := slots[i].targetWeight
		if total > 0 {
			w = slots[i].balance / total
		}
		slots[i].balance += amount * w
	}
}

func withdrawAmount(slots []assetSlot, cashIdx int, amount float64, txRate float64) (bool, int64) {
	remaining := amount

	if cashIdx >= 0 && slots[cashIdx].balance >= remaining {
		slots[cashIdx].balance -= remaining
		return true, 0
	}

	if cashIdx >= 0 {
		remaining -= slots[cashIdx].balance
		slots[cashIdx].balance = 0
	}

	total := 0.0
	for _, s := range slots {
		total += s.balance
	}
	if remaining <= 0 {
		return true, 0
	}
	if txRate >= 1 {
		return false, 0
	}
	grossNeeded := remaining / (1 - txRate)
	if total+1e-9 < grossNeeded {
		return false, 0
	}
	for i := range slots {
		if slots[i].balance <= 0 {
			continue
		}
		share := grossNeeded * (slots[i].balance / total)
		slots[i].balance -= share
	}
	cost := int64(math.Round(grossNeeded * txRate))
	return true, cost
}

func shouldRebalance(month int, freq string) bool {
	switch freq {
	case "monthly":
		return true
	case "quarterly":
		return month%3 == 0
	default:
		return month%12 == 0
	}
}

func needsRebalance(slots []assetSlot, threshold float64) bool {
	total := 0.0
	for _, s := range slots {
		total += s.balance
	}
	if total <= 0 {
		return false
	}
	for _, s := range slots {
		cw := s.balance / total
		if math.Abs(cw-s.targetWeight) >= threshold {
			return true
		}
	}
	return false
}

// rebalanceToTarget charges transaction cost on the first-pass trade volume
// (distance from current balances to total*weight), then distributes the
// post-cost total by target weights. This closed form is the exact semantics
// of the former 50-iteration loop, which always converged on its first pass
// because reassigning balances to newTotal*weight made the residual check
// trivially true (golden tests pin bit-for-bit equivalence).
func rebalanceToTarget(slots []assetSlot, txRate float64) int64 {
	total := 0.0
	for _, s := range slots {
		total += s.balance
	}
	var tradeVolume float64
	for i := range slots {
		tradeVolume += math.Abs(total*slots[i].targetWeight - slots[i].balance)
	}
	cost := int64(math.Round(tradeVolume * txRate))
	newTotal := total - float64(cost)
	if newTotal < 0 {
		newTotal = 0
	}
	for i := range slots {
		slots[i].balance = newTotal * slots[i].targetWeight
	}
	return cost
}

func sumBalances(slots []assetSlot) float64 {
	sum := 0.0
	for _, s := range slots {
		sum += s.balance
	}
	return sum
}

func classifyFailure(month, retire, horizon int, inflCumulative float64) string {
	if month < retire+60 {
		return FailureEarlySequence
	}
	if inflCumulative > 2.0 {
		return FailureHighInflation
	}
	if month > horizon-120 {
		return FailureLongevity
	}
	return FailureSpendingShock
}

type yearAccumulator struct {
	start                 int64
	income, netSpend, tax int64
	txCost                int64
	lastWealth            int64
	lastDD                float64
	maxDD                 float64
	rebalanced            bool
	startCumInfl          float64
	lastCumInfl           float64
}

func (y *yearAccumulator) accum(netSpend, income, tax, tx int64, wealth int64,
	dd float64, rebal bool, cumInfl float64,
) {
	y.income += income
	y.netSpend += netSpend
	y.tax += tax
	y.txCost += tx
	y.lastWealth = wealth
	y.lastDD = dd
	y.lastCumInfl = cumInfl
	if dd > y.maxDD {
		y.maxDD = dd
	}
	if rebal {
		y.rebalanced = true
	}
}

func (y *yearAccumulator) finish(yearIdx, startAge int, weights map[string]float64) YearRecord {
	gain := y.lastWealth - y.start - y.income + y.netSpend + y.tax + y.txCost
	startCum := y.startCumInfl
	if startCum <= 0 {
		startCum = 1
	}
	rec := YearRecord{
		Year: startAge + yearIdx, StartWealthMinor: y.start, IncomeMinor: y.income,
		SpendingMinor: y.netSpend, TaxMinor: y.tax, TransactionCost: y.txCost,
		InvestmentGainLoss: gain, EndWealthMinor: y.lastWealth,
		YearEndDrawdown: y.lastDD, MaxIntraYearDD: y.maxDD, Rebalanced: y.rebalanced,
		AssetWeights:         weights,
		CumInflation:         y.lastCumInfl,
		RealStartWealthMinor: deflate(y.start, startCum),
		RealEndWealthMinor:   deflate(y.lastWealth, y.lastCumInfl),
	}
	// Annual investment return only exists with a positive opening balance;
	// otherwise leave it nil so the UI renders "—" instead of dividing by zero.
	if y.start > 0 {
		r := float64(gain) / float64(y.start)
		rec.AnnualReturn = &r
	}
	return rec
}

func formatSeed(seed int64) string {
	return strconv.FormatInt(seed, 10)
}
