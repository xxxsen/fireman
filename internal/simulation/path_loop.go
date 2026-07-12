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

//nolint:funlen,gocognit,gocyclo,nestif // Version-gated ledgers intentionally retain legacy replay.
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
	useNetSettlement := UsesNetRetirementSettlement(in.EngineVersion)
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
			state.yearAcc.startCumInfl = infl.Cumulative
		}

		income := pathMonthIncomeAmount(p, month, retire)
		if month < retire || !useNetSettlement {
			addPathIncome(in, slots, cashIdx, income)
		}
		if useNetSettlement && month >= retire {
			withdraw.SetStableIncomeAnnual(pathAnnualRetirementIncome(p, month, retire))
		}
		requestedSpend := pathMonthSpendingRequest(
			month, retire, monthStart, infl, withdraw, monthShock, hasShock,
		)

		actualSpend, tax, txCost := requestedSpend, int64(0), int64(0)
		if useNetSettlement {
			settlement := RetirementSettlement{
				SpendingRequestedMinor:  requestedSpend,
				PortfolioNetNeededMinor: requestedSpend,
				GrossWithdrawalMinor:    requestedSpend,
			}
			if month >= retire {
				settlement = SettleRetirementMonth(
					requestedSpend, income, p.WithdrawalTaxRate, p.TaxableWithdrawalRatio,
				)
			}
			withdrawalTaxRate, taxableRatio := 0.0, 0.0
			if month >= retire {
				withdrawalTaxRate = p.WithdrawalTaxRate
				taxableRatio = p.TaxableWithdrawalRatio
			}
			funded := fundPathAmount(
				in, slots, cashIdx, settlement.GrossWithdrawalMinor,
				settlement.PortfolioNetNeededMinor, withdrawalTaxRate,
				taxableRatio, p.TransactionCostRate,
			)
			tax = funded.TaxFundedMinor
			txCost = funded.TransactionCostMinor
			stableIncomeSpent := int64(0)
			if month >= retire {
				stableIncomeSpent = min(income, requestedSpend)
				addPathIncome(in, slots, cashIdx, settlement.StableIncomeSurplusMinor)
			}
			actualSpend = stableIncomeSpent + funded.PortfolioNetFundedMinor
			state.summary.TotalSpendingMinor += actualSpend
			state.summary.TransactionCostMinor += txCost
			if !funded.Sufficient {
				infl.Advance(month)
				endWealth := totalWealth(slots)
				state.peak, state.maxDD = updatePeakDrawdown(endWealth, state.peak, state.maxDD)
				if opts.CollectMonthlyWealth {
					state.summary.MonthlyWealthMinor = append(state.summary.MonthlyWealthMinor, endWealth)
					state.summary.MonthlyCumInflation = append(
						state.summary.MonthlyCumInflation, infl.Cumulative,
					)
				}
				if opts.CollectDetail {
					state = appendPathDetail(
						state, month, horizon, p, endWealth, actualSpend, requestedSpend, income, tax, txCost,
						false, slots, infl.Cumulative, true,
					)
				}
				state.failed = true
				state.failMonth = month
				state.failReason = pathFailureReason(
					in, FailureInsufficientFunds, month, retire, horizon, infl.Cumulative,
				)
				break
			}
		} else {
			grossWithdrawal, legacyTax := requestedSpend, int64(0)
			if month >= retire {
				grossWithdrawal, legacyTax = GrossWithdrawal(
					requestedSpend, p.WithdrawalTaxRate, p.TaxableWithdrawalRatio,
				)
			}
			tax = legacyTax
			state.summary.TotalSpendingMinor += requestedSpend
			if grossWithdrawal > 0 {
				ok, cost := withdrawPathAmount(
					in, slots, cashIdx, float64(grossWithdrawal), p.TransactionCostRate,
				)
				txCost = cost
				state.summary.TransactionCostMinor += cost
				if !ok {
					state.failed = true
					state.failMonth = month
					state.failReason = pathFailureReason(
						in, FailureInsufficientFunds, month, retire, horizon, infl.Cumulative,
					)
					break
				}
			}
		}

		rebalanced := pathMonthRebalance(slots, month, p, &state.summary, &txCost)
		state.truncCount += applyPathReturns(in, slots, rng, monthShock, hasShock)
		applyCashReturns(in, slots, monthShock, hasShock)

		infl.Advance(month)
		endWealth := totalWealth(slots)
		state.peak, state.maxDD = updatePeakDrawdown(endWealth, state.peak, state.maxDD)

		if opts.CollectMonthlyWealth {
			state.summary.MonthlyWealthMinor = append(state.summary.MonthlyWealthMinor, endWealth)
			state.summary.MonthlyCumInflation = append(state.summary.MonthlyCumInflation, infl.Cumulative)
		}
		depleted := endWealth <= 0
		if opts.CollectDetail {
			state = appendPathDetail(state, month, horizon, p, endWealth, actualSpend, requestedSpend, income, tax, txCost,
				rebalanced, slots, infl.Cumulative, depleted && useNetSettlement)
		}

		if depleted {
			state.failed = true
			state.failMonth = month
			state.failReason = pathFailureReason(
				in, FailureWealthDepleted, month, retire, horizon, infl.Cumulative,
			)
			break
		}
	}
	return state
}

func pathMonthIncomeAmount(p SnapshotParameters, month, retire int) int64 {
	if month < retire {
		yearIdx := month / 12
		saving := float64(p.AnnualSavingsMinor) * math.Pow(1+p.AnnualSavingsGrowthRate, float64(yearIdx)) / 12
		return int64(math.Round(saving))
	}
	retiredYear := (month - retire) / 12
	retirementIncome := float64(p.AnnualRetirementIncomeMinor) *
		math.Pow(1+p.AnnualRetirementIncomeGrowthRate, float64(retiredYear)) / 12
	return int64(math.Round(retirementIncome))
}

func pathAnnualRetirementIncome(p SnapshotParameters, month, retire int) int64 {
	if month < retire {
		return 0
	}
	retiredYear := (month - retire) / 12
	return int64(math.Round(float64(p.AnnualRetirementIncomeMinor) *
		math.Pow(1+p.AnnualRetirementIncomeGrowthRate, float64(retiredYear))))
}

func addPathIncome(in *InputSnapshot, slots []assetSlot, cashIdx int, income int64) {
	if income > 0 {
		if in.AggregateCashLiquidity {
			addCash(slots, float64(income))
		} else {
			addLegacyCash(slots, cashIdx, float64(income))
		}
	}
}

// pathMonthIncome retains the legacy helper contract for focused tests and
// historical-path reasoning. New path execution separates calculation from
// deposit so retirement income can first offset spending.
func pathMonthIncome(
	in *InputSnapshot,
	p SnapshotParameters,
	month, retire int,
	slots []assetSlot,
	cashIdx int,
) int64 {
	income := pathMonthIncomeAmount(p, month, retire)
	addPathIncome(in, slots, cashIdx, income)
	return income
}

func withdrawPathAmount(
	in *InputSnapshot,
	slots []assetSlot,
	cashIdx int,
	amount, txRate float64,
) (bool, int64) {
	if in.AggregateCashLiquidity {
		return withdrawAmount(slots, amount, txRate)
	}
	return withdrawLegacyAmount(slots, cashIdx, amount, txRate)
}

func pathFailureReason(
	in *InputSnapshot,
	factReason string,
	month, retire, horizon int,
	cumulativeInflation float64,
) string {
	if UsesFactBasedFailureStates(in.EngineVersion) {
		return factReason
	}
	return legacyFailureReason(month, retire, horizon, cumulativeInflation)
}

func pathMonthSpendingRequest(
	month, retire int,
	monthStart int64,
	infl *InflationState,
	withdraw *WithdrawalPlanner,
	monthShock MonthShock,
	hasShock bool,
) int64 {
	if month >= retire {
		isAnniv := month > retire && (month-retire)%12 == 0
		net := withdraw.MonthlySpending(month, retire, monthStart, infl.Cumulative, isAnniv)
		if hasShock {
			if monthShock.SpendingMultiplier > 0 && monthShock.SpendingMultiplier != 1 {
				net = int64(math.Round(float64(net) * monthShock.SpendingMultiplier))
			}
			net += monthShock.ExtraSpendingMinor
		}
		return max(net, 0)
	}
	netSpend := int64(0)
	if hasShock {
		netSpend += monthShock.ExtraSpendingMinor
	}
	return max(netSpend, 0)
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
	monthShock MonthShock,
	hasShock bool,
) int {
	if in.RandomFactorModel == FactorModelMultivariate && in.FactorModel != nil {
		return applyPathReturnsJoint(in, slots, rng, monthShock, hasShock)
	}
	truncCount := 0
	for i := range slots {
		truncCount += applySlotReturn(in, slots, i, rng, monthShock, hasShock)
	}
	return truncCount
}

// applyPathReturnsJoint draws one jointly-distributed month for every factor and
// composes asset-local with shared FX returns. All factors share
// one fat-tail scale so extreme months co-occur instead of being independent.
func applyPathReturnsJoint(
	in *InputSnapshot,
	slots []assetSlot,
	rng *RNG,
	monthShock MonthShock,
	hasShock bool,
) int {
	mu := in.FactorModel.Mu
	if hasShock {
		mu = adjustedFactorMu(in, monthShock)
	}
	factorReturns, trunc := SampleMultivariateStudentT(
		rng, mu, in.FactorModel.L, in.EffectiveDf(), in.TailTruncationBounds(),
	)
	for i := range slots {
		applySlotJointReturn(in, slots, i, factorReturns, monthShock, hasShock)
	}
	return trunc
}

func applySlotJointReturn(
	in *InputSnapshot,
	slots []assetSlot,
	i int,
	factorReturns []float64,
	monthShock MonthShock,
	hasShock bool,
) {
	if slots[i].isCash || i >= len(in.AssetFactorRefs) {
		return
	}
	ref := in.AssetFactorRefs[i]
	if ref.AssetFactorIndex < 0 || ref.AssetFactorIndex >= len(factorReturns) {
		return
	}
	local := factorReturns[ref.AssetFactorIndex]
	var assetShock AssetShock
	if hasShock {
		assetShock = monthShock.Assets[i]
	}
	if assetShock.ReturnMul != 0 {
		local = (1+local)*(1+assetShock.ReturnMul) - 1
	}
	ret := local
	if ref.FXFactorIndex >= 0 && ref.FXFactorIndex < len(factorReturns) {
		fx := factorReturns[ref.FXFactorIndex]
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

// adjustedFactorMu copies the frozen factor drifts and applies per-asset annual
// DriftDelta overlays (stress/sensitivity) in the same way the independent path
// does, keeping volatility (and therefore L) unchanged.
func adjustedFactorMu(in *InputSnapshot, monthShock MonthShock) []float64 {
	floor := in.TailTruncationBounds().Floor
	mu := append([]float64(nil), in.FactorModel.Mu...)
	for i := range in.Assets {
		shock, ok := monthShock.Assets[i]
		if !ok || shock.DriftDelta == 0 || i >= len(in.AssetFactorRefs) {
			continue
		}
		ref := in.AssetFactorRefs[i]
		if ref.AssetFactorIndex < 0 || ref.AssetFactorIndex >= len(mu) {
			continue
		}
		annual := in.Assets[i].ModeledAnnualReturn + shock.DriftDelta
		if annual < floor {
			annual = floor
		}
		mu[ref.AssetFactorIndex] = math.Log(1+annual) / 12
	}
	return mu
}

func applySlotReturn(
	in *InputSnapshot,
	slots []assetSlot,
	i int,
	rng *RNG,
	monthShock MonthShock,
	hasShock bool,
) int {
	if slots[i].isCash {
		return 0
	}
	trunc := in.TailTruncationBounds()
	df := in.EffectiveDf()
	params := slots[i].returnParams
	var assetShock AssetShock
	if hasShock {
		assetShock = monthShock.Assets[i]
		if assetShock.DriftDelta != 0 {
			annual := in.Assets[i].ModeledAnnualReturn + assetShock.DriftDelta
			if annual < trunc.Floor {
				annual = trunc.Floor
			}
			params = ParamsFromAnnual(annual, in.Assets[i].AnnualVolatility)
		}
	}
	local, tr := SampleStudentT(rng, params, df, trunc)
	truncCount := 0
	if tr {
		truncCount++
	}
	if assetShock.ReturnMul != 0 {
		local = (1+local)*(1+assetShock.ReturnMul) - 1
	}
	ret := local
	if slots[i].useFX {
		fx, tr2 := SampleStudentT(rng, slots[i].fxParams, df, trunc)
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

// applyCashReturns grows every cash slot by its frozen deterministic monthly
// return r_m = exp(ln(1+forward_annual)/12) - 1. Cash is intentionally non-random
// and FX-free: it is excluded from the Student-t draw, the correlation matrix and
// the FX factor. Only forward (3.0.0) inputs set
// DeterministicCashReturn; legacy 2.x snapshots keep cash at 0% so old runs replay
// byte-for-byte. A cash-specific stress shock composes on the deterministic return
// using the same AssetShock semantics as risk assets.
func applyCashReturns(in *InputSnapshot, slots []assetSlot, monthShock MonthShock, hasShock bool) {
	if !in.DeterministicCashReturn {
		return
	}
	floor := in.TailTruncationBounds().Floor
	for i := range slots {
		if !slots[i].isCash {
			continue
		}
		params := slots[i].returnParams
		var assetShock AssetShock
		if hasShock {
			assetShock = monthShock.Assets[i]
			if assetShock.DriftDelta != 0 && i < len(in.Assets) {
				annual := in.Assets[i].ModeledAnnualReturn + assetShock.DriftDelta
				if annual < floor {
					annual = floor
				}
				params = ParamsFromAnnual(annual, 0)
			}
		}
		r := math.Exp(params.MonthlyMu) - 1
		if assetShock.ReturnMul != 0 {
			r = (1+r)*(1+assetShock.ReturnMul) - 1
		}
		slots[i].balance *= (1 + r)
		if slots[i].balance < 0 {
			slots[i].balance = 0
		}
	}
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
	endWealth, netSpend, spendingRequested, income, tax, txCost int64,
	rebalanced bool,
	slots []assetSlot,
	cumInfl float64,
	forceYearEnd bool,
) pathSimState {
	mr := MonthRecord{
		MonthOffset: month, TotalWealthMinor: endWealth, SpendingMinor: netSpend,
		SpendingRequestedMinor: spendingRequested,
		UnfundedSpendingMinor:  max(spendingRequested-netSpend, 0),
		IncomeMinor:            income, TaxMinor: tax, TransactionCost: txCost,
		Rebalanced: rebalanced, CumInflation: cumInfl,
		RealTotalWealthMinor: deflate(endWealth, cumInfl),
	}
	if state.peak > 0 {
		mr.Drawdown = 1 - float64(endWealth)/float64(state.peak)
	}
	state.detail.Monthly = append(state.detail.Monthly, mr)
	state.yearAcc.accum(netSpend, income, tax, txCost, endWealth, mr.Drawdown, rebalanced, cumInfl)
	if month%12 == 11 || month == horizon-1 || forceYearEnd {
		state.detail.Yearly = append(state.detail.Yearly, state.yearAcc.finish(month/12, p.CurrentAge, slotWeights(slots)))
		state.yearAcc = yearAccumulator{start: endWealth, startCumInfl: cumInfl}
	}
	return state
}

func finalizePathSummary(
	summary *PathSummary,
	slots []assetSlot,
	state pathSimState,
	floor int64,
	factBasedFailures bool,
) {
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
		summary.FailureReason = terminalFailureReason(summary.TerminalWealthMinor, factBasedFailures)
	}
}

func terminalFailureReason(terminalWealth int64, factBased bool) string {
	if factBased {
		if terminalWealth <= 0 {
			return FailureWealthDepleted
		}
		return FailureTerminalFloor
	}
	if terminalWealth <= 0 {
		return FailureOther
	}
	return FailureLongevity
}
