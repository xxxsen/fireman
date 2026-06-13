package simulation

import "math"

type pathSimState struct {
	summary    PathSummary
	detail     *PathDetail
	peak       int64
	maxDD      float64
	truncCount int
	failed     bool
	failMonth  int
	failReason string
	yearAcc    yearAccumulator
}

func runPathMonths(
	in *InputSnapshot,
	slots []assetSlot,
	cashIdx int,
	horizon, retire int,
	infl *InflationState,
	withdraw *WithdrawalPlanner,
	rng *RNG,
	opts PathRunOpts,
	state pathSimState,
) pathSimState {
	p := in.Parameters
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
			state.yearAcc.start = monthStart
		}

		income := pathMonthIncome(in, p, month, retire, infl, slots, cashIdx)
		netSpend, tax, grossWithdrawal := pathMonthSpending(
			in, p, month, retire, monthStart, infl, withdraw, monthShock, hasShock, &state.summary,
		)

		txCost := int64(0)
		if grossWithdrawal > 0 {
			ok, cost := withdrawAmount(slots, cashIdx, float64(grossWithdrawal), p.TransactionCostRate)
			txCost = cost
			state.summary.TransactionCostMinor += cost
			if !ok {
				state.failed = true
				state.failMonth = month
				state.failReason = classifyFailure(month, retire, horizon, infl.Cumulative)
				break
			}
		}

		rebalanced := pathMonthRebalance(slots, month, p, &state.summary, &txCost)
		state.truncCount += applyPathReturns(in, slots, rng, p, monthShock, hasShock)

		infl.Advance(month)
		endWealth := totalWealth(slots)
		state.peak, state.maxDD = updatePeakDrawdown(endWealth, state.peak, state.maxDD)

		if opts.CollectMonthlyWealth {
			state.summary.MonthlyWealthMinor = append(state.summary.MonthlyWealthMinor, endWealth)
		}
		if opts.CollectDetail {
			state = appendPathDetail(state, month, horizon, p, endWealth, netSpend, income, tax, txCost, rebalanced, slots)
		}

		if endWealth <= 0 {
			state.failed = true
			state.failMonth = month
			state.failReason = classifyFailure(month, retire, horizon, infl.Cumulative)
			break
		}
	}
	return state
}

func pathMonthIncome(
	in *InputSnapshot,
	p SnapshotParameters,
	month, retire int,
	infl *InflationState,
	slots []assetSlot,
	cashIdx int,
) int64 {
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
	return income
}

func pathMonthSpending(
	in *InputSnapshot,
	p SnapshotParameters,
	month, retire int,
	monthStart int64,
	infl *InflationState,
	withdraw *WithdrawalPlanner,
	monthShock MonthShock,
	hasShock bool,
	summary *PathSummary,
) (int64, int64, int64) {
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
		summary.TotalSpendingMinor += net
		return net, t, gross
	}
	netSpend := cashFlowAmount(in.CashFlows, month, infl.Cumulative, "expense")
	if hasShock {
		netSpend += monthShock.ExtraSpendingMinor
	}
	return netSpend, 0, netSpend
}

func pathMonthRebalance(slots []assetSlot, month int, p SnapshotParameters, summary *PathSummary, txCost *int64) bool {
	if month <= 0 || !shouldRebalance(month, p.RebalanceFrequency) || !needsRebalance(slots, p.RebalanceThreshold) {
		return false
	}
	cost := rebalanceToTarget(slots, p.TransactionCostRate)
	*txCost += cost
	summary.TransactionCostMinor += cost
	return true
}

func applyPathReturns(
	in *InputSnapshot,
	slots []assetSlot,
	rng *RNG,
	p SnapshotParameters,
	monthShock MonthShock,
	hasShock bool,
) int {
	truncCount := 0
	for i := range slots {
		truncCount += applySlotReturn(in, slots, i, rng, p, monthShock, hasShock)
	}
	return truncCount
}

func applySlotReturn(
	in *InputSnapshot,
	slots []assetSlot,
	i int,
	rng *RNG,
	p SnapshotParameters,
	monthShock MonthShock,
	hasShock bool,
) int {
	if slots[i].isCash {
		return 0
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
	truncCount := 0
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
	return truncCount
}

func updatePeakDrawdown(endWealth, peak int64, maxDD float64) (int64, float64) {
	if endWealth > peak {
		peak = endWealth
	}
	if peak > 0 {
		dd := 1 - float64(endWealth)/float64(peak)
		if dd > maxDD {
			maxDD = dd
		}
	}
	return peak, maxDD
}

func appendPathDetail(
	state pathSimState,
	month, horizon int,
	p SnapshotParameters,
	endWealth, netSpend, income, tax, txCost int64,
	rebalanced bool,
	slots []assetSlot,
) pathSimState {
	mr := MonthRecord{
		MonthOffset: month, TotalWealthMinor: endWealth, SpendingMinor: netSpend,
		IncomeMinor: income, TaxMinor: tax, TransactionCost: txCost,
		Rebalanced: rebalanced,
	}
	if state.peak > 0 {
		mr.Drawdown = 1 - float64(endWealth)/float64(state.peak)
	}
	state.detail.Monthly = append(state.detail.Monthly, mr)
	state.yearAcc.accum(netSpend, income, tax, txCost, endWealth, mr.Drawdown, rebalanced)
	if month%12 == 11 || month == horizon-1 {
		state.detail.Yearly = append(state.detail.Yearly, state.yearAcc.finish(month/12, p.CurrentAge, slotWeights(slots)))
		state.yearAcc = yearAccumulator{start: endWealth}
	}
	return state
}

func finalizePathSummary(summary *PathSummary, slots []assetSlot, state pathSimState, floor int64) {
	summary.TerminalWealthMinor = totalWealth(slots)
	summary.MaxDrawdown = state.maxDD
	summary.TruncationCount = state.truncCount
	if state.failed {
		summary.Succeeded = false
		summary.FailureMonth = &state.failMonth
		summary.FailureReason = state.failReason
		return
	}
	summary.Succeeded = summary.TerminalWealthMinor > 0 && summary.TerminalWealthMinor >= floor
	if !summary.Succeeded {
		summary.FailureReason = FailureLongevity
		if summary.TerminalWealthMinor <= 0 {
			summary.FailureReason = FailureOther
		}
	}
}
