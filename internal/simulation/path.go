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
}

// MonthRecord captures one month of path detail.
type MonthRecord struct {
	MonthOffset      int
	TotalWealthMinor int64
	SpendingMinor    int64
	IncomeMinor      int64
	TaxMinor         int64
	TransactionCost  int64
	Drawdown         float64
	Rebalanced       bool
}

// YearRecord captures annual aggregates for path detail.
type YearRecord struct {
	Year               int
	StartWealthMinor   int64
	IncomeMinor        int64
	SpendingMinor      int64
	TaxMinor           int64
	TransactionCost    int64
	InvestmentGainLoss int64
	EndWealthMinor     int64
	YearEndDrawdown    float64
	MaxIntraYearDD     float64
	Rebalanced         bool
	AssetWeights       map[string]float64 `json:"asset_weights"`
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
	}

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

func rebalanceToTarget(slots []assetSlot, txRate float64) int64 {
	const cent = 0.005
	var recorded int64
	for iter := 0; iter < 50; iter++ {
		total := 0.0
		for _, s := range slots {
			total += s.balance
		}
		var tradeVolume float64
		targets := make([]float64, len(slots))
		for i := range slots {
			targets[i] = total * slots[i].targetWeight
			tradeVolume += math.Abs(targets[i] - slots[i].balance)
		}
		cost := int64(math.Round(tradeVolume * txRate))
		if cost == 0 {
			for i := range slots {
				slots[i].balance = targets[i]
			}
			return recorded
		}
		newTotal := total - float64(cost)
		if newTotal < 0 {
			newTotal = 0
		}
		recorded += cost
		for i := range slots {
			slots[i].balance = newTotal * slots[i].targetWeight
		}
		if math.Abs(total-float64(recorded)-sumBalances(slots)) <= cent {
			return recorded
		}
	}
	return recorded
}

func sumBalances(slots []assetSlot) float64 {
	sum := 0.0
	for _, s := range slots {
		sum += s.balance
	}
	return sum
}

func cashFlowAmount(flows []SnapshotCashFlow, month int, inflCumulative float64, kind string) int64 {
	var sum int64
	for _, f := range flows {
		if !f.Enabled || f.Kind != kind {
			continue
		}
		if month < f.StartMonthOffset || month > f.EndMonthOffset {
			continue
		}
		apply := false
		switch f.Recurrence {
		case "once":
			apply = month == f.StartMonthOffset
		case "annual":
			apply = (month-f.StartMonthOffset)%12 == 0
		default:
			apply = true
		}
		if !apply {
			continue
		}
		amt := float64(f.AmountMinor)
		years := float64(month / 12)
		amt *= math.Pow(1+f.AnnualGrowthRate, years)
		if f.InflationLinked {
			amt *= inflCumulative
		}
		sum += int64(math.Round(amt))
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
}

func (y *yearAccumulator) accum(netSpend, income, tax, tx int64, wealth int64, dd float64, rebal bool) {
	y.income += income
	y.netSpend += netSpend
	y.tax += tax
	y.txCost += tx
	y.lastWealth = wealth
	y.lastDD = dd
	if dd > y.maxDD {
		y.maxDD = dd
	}
	if rebal {
		y.rebalanced = true
	}
}

func (y *yearAccumulator) finish(yearIdx, startAge int, weights map[string]float64) YearRecord {
	gain := y.lastWealth - y.start - y.income + y.netSpend + y.tax + y.txCost
	return YearRecord{
		Year: startAge + yearIdx, StartWealthMinor: y.start, IncomeMinor: y.income,
		SpendingMinor: y.netSpend, TaxMinor: y.tax, TransactionCost: y.txCost,
		InvestmentGainLoss: gain, EndWealthMinor: y.lastWealth,
		YearEndDrawdown: y.lastDD, MaxIntraYearDD: y.maxDD, Rebalanced: y.rebalanced,
		AssetWeights: weights,
	}
}

func formatSeed(seed int64) string {
	return strconv.FormatInt(seed, 10)
}
