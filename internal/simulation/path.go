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
	pathSeed := int64(SplitMix64(uint64(in.RootSeed()) + uint64(pathNo)))
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
	withdraw := NewWithdrawalPlanner(p.WithdrawalType, p.AnnualSpendingMinor, p.WithdrawalRate, p.WithdrawalFloorRatio, p.WithdrawalCeilingRatio)

	var detail *PathDetail
	if opts.CollectDetail {
		detail = &PathDetail{PathNo: pathNo, PathSeed: formatSeed(pathSeed)}
	}

	summary := PathSummary{PathNo: pathNo, PathSeed: pathSeed}
	peak := int64(math.Round(total))
	maxDD := 0.0
	truncCount := 0
	failed := false
	var failMonth int
	var failReason string
	yearAcc := yearAccumulator{}

	for month := 0; month < horizon; month++ {
		if month == retire {
			withdraw.InitAtRetirement(totalWealth(slots))
		}

		monthShock, hasShock := opts.Shocks[month]
		if hasShock && monthShock.InflationAnnual != nil {
			infl.SetOverrideAnnual(monthShock.InflationAnnual)
		} else {
			infl.ClearOverrideAnnual()
		}

		monthStart := totalWealth(slots)
		if opts.CollectDetail && month%12 == 0 {
			yearAcc.start = monthStart
		}

		// 2. savings and cash-flow income
		income := int64(0)
		if month < retire {
			yearIdx := month / 12
			saving := float64(p.AnnualSavingsMinor) * math.Pow(1+p.AnnualSavingsGrowthRate, float64(yearIdx)) / 12
			income += int64(math.Round(saving))
		}
		income += cashFlowAmount(in.CashFlows, month, infl.Cumulative, "income")
		if income > 0 {
			addCash(slots, cashIdx, float64(income))
		}

		// 3-4. spending, tax, withdrawal
		spend := int64(0)
		tax := int64(0)
		if month >= retire {
			isAnniv := month > retire && (month-retire)%12 == 0
			net := withdraw.MonthlySpending(month, retire, monthStart, infl.Cumulative, isAnniv)
			net += cashFlowAmount(in.CashFlows, month, infl.Cumulative, "expense")
			if hasShock {
				if monthShock.SpendingMultiplier > 0 && monthShock.SpendingMultiplier != 1 {
					net = int64(math.Round(float64(net) * monthShock.SpendingMultiplier))
				}
				net += monthShock.ExtraSpendingMinor
			}
			gross, t := GrossWithdrawal(net, p.WithdrawalTaxRate, p.TaxableWithdrawalRatio)
			spend = gross
			tax = t
			summary.TotalSpendingMinor += net
		} else {
			spend = cashFlowAmount(in.CashFlows, month, infl.Cumulative, "expense")
			if hasShock {
				spend += monthShock.ExtraSpendingMinor
			}
		}

		txCost := int64(0)
		if spend > 0 {
			ok, cost := withdrawAmount(slots, cashIdx, float64(spend), p.TransactionCostRate)
			txCost = cost
			summary.TransactionCostMinor += cost
			if !ok {
				failed = true
				failMonth = month
				failReason = classifyFailure(month, retire, horizon, infl.Cumulative)
				break
			}
		}

		// 6. rebalance
		rebalanced := false
		if month > 0 && shouldRebalance(month, p.RebalanceFrequency) {
			if needsRebalance(slots, p.RebalanceThreshold) {
				cost := rebalanceToTarget(slots, p.TransactionCostRate)
				txCost += cost
				summary.TransactionCostMinor += cost
				rebalanced = true
			}
		}

		// 7. returns
		for i := range slots {
			if slots[i].isCash {
				continue
			}
			params := slots[i].returnParams
			var assetShock AssetShock
			if hasShock {
				assetShock = monthShock.Assets[i]
				if assetShock.DriftDelta != 0 {
					annual := in.Assets[i].ModeledAnnualReturn + assetShock.DriftDelta
					if annual < ReturnFloor {
						annual = ReturnFloor
					}
					params = ParamsFromAnnual(annual, in.Assets[i].AnnualVolatility)
				}
			}
			local, tr := SampleStudentT(rng, params, p.StudentTDf)
			if tr {
				truncCount++
			}
			if assetShock.ReturnMul != 0 {
				local = (1+local)*(1+assetShock.ReturnMul) - 1
			}
			ret := local
			if slots[i].useFX {
				fx, tr2 := SampleStudentT(rng, slots[i].fxParams, p.StudentTDf)
				if tr2 {
					truncCount++
				}
				if assetShock.FXReturnMul != 0 {
					fx = (1+fx)*(1+assetShock.FXReturnMul) - 1
				}
				ret = CompositeBaseReturn(local, fx)
			}
			slots[i].balance *= (1 + ret)
			if slots[i].balance < 0 {
				slots[i].balance = 0
			}
		}

		infl.Advance(month)
		endWealth := totalWealth(slots)
		if endWealth > peak {
			peak = endWealth
		}
		if peak > 0 {
			dd := 1 - float64(endWealth)/float64(peak)
			if dd > maxDD {
				maxDD = dd
			}
		}

		if opts.CollectMonthlyWealth {
			summary.MonthlyWealthMinor = append(summary.MonthlyWealthMinor, endWealth)
		}

		if opts.CollectDetail {
			mr := MonthRecord{
				MonthOffset: month, TotalWealthMinor: endWealth, SpendingMinor: spend,
				IncomeMinor: income, TaxMinor: tax, TransactionCost: txCost,
				Rebalanced: rebalanced,
			}
			if peak > 0 {
				mr.Drawdown = 1 - float64(endWealth)/float64(peak)
			}
			detail.Monthly = append(detail.Monthly, mr)
			yearAcc.accum(spend, income, tax, txCost, endWealth, mr.Drawdown, rebalanced)
			if month%12 == 11 || month == horizon-1 {
				detail.Yearly = append(detail.Yearly, yearAcc.finish(month/12, p.CurrentAge, slotWeights(slots)))
				yearAcc = yearAccumulator{start: endWealth}
			}
		}

		if endWealth <= 0 {
			failed = true
			failMonth = month
			failReason = classifyFailure(month, retire, horizon, infl.Cumulative)
			break
		}
	}

	summary.TerminalWealthMinor = totalWealth(slots)
	summary.MaxDrawdown = maxDD
	summary.TruncationCount = truncCount

	if failed {
		summary.Succeeded = false
		summary.FailureMonth = &failMonth
		summary.FailureReason = failReason
	} else {
		summary.Succeeded = summary.TerminalWealthMinor > 0 &&
			summary.TerminalWealthMinor >= p.TerminalWealthFloorMinor
		if !summary.Succeeded {
			failReason = FailureLongevity
			if summary.TerminalWealthMinor <= 0 {
				failReason = FailureOther
			}
		}
	}

	if opts.CollectMonthlyWealth {
		summary.MonthlyWealthMinor = padMonthlyWealth(summary.MonthlyWealthMinor, horizon)
	}

	if opts.CollectDetail {
		detail.Succeeded = summary.Succeeded
		detail.FailureReason = failReason
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
	var tradeVolume float64

	if cashIdx >= 0 && slots[cashIdx].balance >= remaining {
		slots[cashIdx].balance -= remaining
		tradeVolume = remaining
		cost := int64(math.Round(tradeVolume * txRate))
		if cost > 0 {
			deductProRata(slots, float64(cost))
		}
		return true, cost
	}

	if cashIdx >= 0 {
		remaining -= slots[cashIdx].balance
		tradeVolume += slots[cashIdx].balance
		slots[cashIdx].balance = 0
	}

	total := 0.0
	for _, s := range slots {
		total += s.balance
	}
	if total < remaining {
		return false, 0
	}
	for i := range slots {
		if slots[i].balance <= 0 {
			continue
		}
		share := remaining * (slots[i].balance / total)
		slots[i].balance -= share
		tradeVolume += share
	}
	cost := int64(math.Round(tradeVolume * txRate))
	if cost > 0 {
		deductProRata(slots, float64(cost))
	}
	return true, cost
}

func deductProRata(slots []assetSlot, amount float64) {
	total := 0.0
	for _, s := range slots {
		total += s.balance
	}
	if total <= amount {
		for i := range slots {
			slots[i].balance = 0
		}
		return
	}
	for i := range slots {
		slots[i].balance -= amount * (slots[i].balance / total)
	}
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
	total := 0.0
	for _, s := range slots {
		total += s.balance
	}
	var tradeVolume float64
	for i := range slots {
		target := total * slots[i].targetWeight
		diff := target - slots[i].balance
		tradeVolume += math.Abs(diff)
		slots[i].balance = target
	}
	return int64(math.Round(tradeVolume * txRate))
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
	start              int64
	income, spend, tax int64
	txCost             int64
	lastWealth         int64
	maxDD              float64
	rebalanced         bool
}

func (y *yearAccumulator) accum(spend, income, tax, tx int64, wealth int64, dd float64, rebal bool) {
	y.income += income
	y.spend += spend
	y.tax += tax
	y.txCost += tx
	y.lastWealth = wealth
	if dd > y.maxDD {
		y.maxDD = dd
	}
	if rebal {
		y.rebalanced = true
	}
}

func (y *yearAccumulator) finish(yearIdx, startAge int, weights map[string]float64) YearRecord {
	gain := y.lastWealth - y.start - y.income + y.spend + y.tax + y.txCost
	return YearRecord{
		Year: startAge + yearIdx, StartWealthMinor: y.start, IncomeMinor: y.income,
		SpendingMinor: y.spend, TaxMinor: y.tax, TransactionCost: y.txCost,
		InvestmentGainLoss: gain, EndWealthMinor: y.lastWealth,
		YearEndDrawdown: y.maxDD, MaxIntraYearDD: y.maxDD, Rebalanced: y.rebalanced,
		AssetWeights: weights,
	}
}

func formatSeed(seed int64) string {
	return strconv.FormatInt(seed, 10)
}
