package investmentpath

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type strategyKind int

var errAccountInvariant = errors.New("investment path account invariant violated")

const (
	kindIncomeDCA strategyKind = iota
	kindCashBaseline
	kindLumpSum
	kindPhaseIn
	kindStatic
	kindThreshold
)

type strategySpec struct {
	key       string
	kind      strategyKind
	months    int
	target    float64
	threshold float64
}

type scheduledBudget struct {
	date         time.Time
	contribution int64
	budget       int64
	reason       string
}

type cashFlow struct {
	date   time.Time
	amount int64
}

type accountState struct {
	units                  float64
	cash                   int64
	issuedUnits            float64
	cumulativeContribution int64
	fees                   int64
	turnover               float64
	pending                int64
	tradeCount             int
	deploymentComplete     string
}

// Run validates the input, computes the primary path once and all rolling
// window summaries, then derives stable cross-window aggregates.
func Run(ctx context.Context, input Input, progress ProgressFunc) (*Result, error) {
	in, resolved, err := ValidateAndResolve(input)
	if err != nil {
		return nil, err
	}
	total := len(resolved.WindowStarts) * len(resolved.StrategyKeys)
	done := 0
	result := &Result{Resolved: resolved}
	for _, start := range resolved.WindowStarts {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		for _, spec := range specs(in) {
			if err := checkContext(ctx); err != nil {
				return nil, err
			}
			detail := start == resolved.PrimaryStart
			window, points, trades, err := runWindow(in, start, spec, detail)
			if err != nil {
				return nil, fmt.Errorf("run %s at %s: %w", spec.key, start, err)
			}
			result.Windows = append(result.Windows, window)
			if detail {
				result.Primary = append(result.Primary, window)
				result.Points = append(result.Points, points...)
				result.Trades = append(result.Trades, trades...)
			}
			done++
			if progress != nil {
				if err := progress(done, total); err != nil {
					return nil, err
				}
			}
		}
	}
	applyLumpSumDeltas(result.Windows)
	applyLumpSumDeltas(result.Primary)
	result.Aggregates = aggregate(in, result.Windows)
	if in.TransactionCostRate == 0 {
		result.Warnings = append(result.Warnings, "transaction_cost_is_zero")
	}
	if len(resolved.WindowStarts) < 24 {
		result.Warnings = append(result.Warnings, "rolling_window_count_below_24")
	}
	return result, nil
}

//nolint:funlen,gocognit,gocyclo,nestif,lll // The ordered daily ledger intentionally keeps all five event phases visible together.
func runWindow(in Input, startText string, spec strategySpec, detail bool) (WindowResult, []Point, []Trade, error) {
	start, _ := time.Parse(DateLayout, startText)
	endExclusive := start.AddDate(0, in.HorizonMonths, 0)
	lastDate := endExclusive.AddDate(0, 0, -1)
	type priceObservation struct {
		value    float64
		tradable bool
	}
	priceByDate := make(map[string]priceObservation, len(in.Prices))
	for _, p := range in.Prices {
		priceByDate[p.Date] = priceObservation{value: p.Value, tradable: p.Tradable}
	}
	schedule := buildSchedule(in, start, spec)
	state := accountState{}
	var points []Point
	var trades []Trade
	var flows []cashFlow
	var lastPrice float64
	peakNAV := 1.0
	peakDate := startText
	var maxDrawdown float64
	var drawdownStart, drawdownEnd string
	underwaterStart := time.Time{}
	longestUnderwater := 0
	belowStart := time.Time{}
	longestBelow := 0
	var firstRecovery string
	var maxDeficit int64
	var maxDeficitRatio float64
	var cashWeightSum float64
	var cashWeightDays int

	for date := start; !date.After(lastDate); date = date.AddDate(0, 0, 1) {
		dateText := date.Format(DateLayout)
		observation, hasObservation := priceByDate[dateText]
		if hasObservation {
			lastPrice = observation.value
		}
		price := lastPrice
		isReal := hasObservation && observation.tradable
		assetBefore := valueOf(state.units, lastPrice)
		valueBeforeFlow := assetBefore + state.cash
		unitNAVBefore := unitNAV(valueBeforeFlow, state.issuedUnits)
		if unitNAVBefore <= 0 {
			unitNAVBefore = 1
		}
		for i := range schedule {
			item := &schedule[i]
			if !item.date.Equal(date) {
				continue
			}
			if item.contribution > 0 {
				state.cash += item.contribution
				state.cumulativeContribution += item.contribution
				state.issuedUnits += float64(item.contribution) / unitNAVBefore
				flows = append(flows, cashFlow{date: date, amount: -item.contribution})
			}
			state.pending += item.budget
			if spec.kind == kindCashBaseline {
				state.pending = 0
			}
		}
		if isReal && state.pending > 0 && spec.kind != kindCashBaseline {
			gross := state.pending
			state.pending = 0
			var trade Trade
			var err error
			if state.tradeCount == 0 && (spec.kind == kindStatic || spec.kind == kindThreshold) {
				trade, err = executeTargetBuild(&state, price, spec.target, in.TransactionCostRate, dateText)
			} else {
				trade, err = executeBuy(&state, price, gross, in.TransactionCostRate, dateText, scheduledReason(state.tradeCount))
			}
			if err != nil {
				return WindowResult{}, nil, nil, err
			}
			trades = append(trades, trade)
		}
		if isReal && spec.kind == kindThreshold && state.issuedUnits > 0 {
			trade, ok, err := rebalance(&state, price, spec, in.TransactionCostRate, dateText)
			if err != nil {
				return WindowResult{}, nil, nil, err
			}
			if ok {
				trades = append(trades, trade)
			}
		}
		if state.pending == 0 && state.deploymentComplete == "" && hasNoFutureInternalBudget(schedule, date) && spec.kind != kindIncomeDCA {
			state.deploymentComplete = dateText
		}
		assetValue := valueOf(state.units, lastPrice)
		accountValue := assetValue + state.cash
		nav := unitNAV(accountValue, state.issuedUnits)
		if nav > peakNAV {
			peakNAV, peakDate = nav, dateText
		}
		drawdown := 0.0
		if peakNAV > 0 {
			drawdown = nav/peakNAV - 1
		}
		if drawdown < maxDrawdown {
			maxDrawdown, drawdownStart, drawdownEnd = drawdown, peakDate, dateText
		}
		if drawdown < -1e-12 {
			if underwaterStart.IsZero() {
				underwaterStart = date
			}
			longestUnderwater = maxInt(longestUnderwater, int(date.Sub(underwaterStart).Hours()/24)+1)
		} else {
			underwaterStart = time.Time{}
		}
		gap := accountValue - state.cumulativeContribution
		if gap < 0 {
			deficit := -gap
			if deficit > maxDeficit {
				maxDeficit = deficit
			}
			if state.cumulativeContribution > 0 {
				maxDeficitRatio = math.Max(maxDeficitRatio, float64(deficit)/float64(state.cumulativeContribution))
			}
			if belowStart.IsZero() {
				belowStart = date
			}
			longestBelow = maxInt(longestBelow, int(date.Sub(belowStart).Hours()/24)+1)
		} else if !belowStart.IsZero() {
			if firstRecovery == "" {
				firstRecovery = dateText
			}
			belowStart = time.Time{}
		}
		if accountValue > 0 {
			cashWeightSum += float64(state.cash) / float64(accountValue)
			cashWeightDays++
		}
		if accountValue < 0 || accountValue != assetValue+state.cash {
			return WindowResult{}, nil, nil, errAccountInvariant
		}
		if detail {
			points = append(points, Point{
				StrategyKey: spec.key, ValuationDate: dateText, AccountValueMinor: accountValue,
				AssetValueMinor: assetValue, CashValueMinor: state.cash,
				CumulativeContributionMinor: state.cumulativeContribution, UnitNAV: nav, Drawdown: drawdown,
			})
		}
	}
	terminal := valueOf(state.units, lastPrice) + state.cash
	flows = append(flows, cashFlow{date: lastDate, amount: terminal})
	xirr, xirrReason := solveXIRR(flows)
	firstNAV, lastNAV := 1.0, unitNAV(terminal, state.issuedUnits)
	twr := 0.0
	if firstNAV > 0 {
		twr = lastNAV/firstNAV - 1
	}
	years := math.Max(lastDate.Sub(start).Hours()/24/365, 1.0/365)
	annualized := math.Pow(math.Max(0, 1+twr), 1/years) - 1
	averageCash := 0.0
	if cashWeightDays > 0 {
		averageCash = cashWeightSum / float64(cashWeightDays)
	}
	for i := range trades {
		trades[i].StrategyKey = spec.key
		trades[i].SequenceNo = i + 1
	}
	return WindowResult{
		StrategyKey: spec.key, WindowStart: startText, WindowEnd: lastDate.Format(DateLayout),
		TotalContributionMinor: state.cumulativeContribution, TerminalValueMinor: terminal,
		ProfitMinor: terminal - state.cumulativeContribution, XIRR: xirr, XIRRReason: xirrReason,
		TWRTotal: twr, TWRAnnualized: annualized, MaxDrawdown: maxDrawdown,
		MaxDrawdownStart: drawdownStart, MaxDrawdownEnd: drawdownEnd,
		LongestUnderwaterDays: longestUnderwater, MaxPrincipalDeficitMinor: maxDeficit,
		MaxPrincipalDeficitRatio: maxDeficitRatio, LongestBelowPrincipalDays: longestBelow,
		FirstRecoveryAbovePrincipalDate: firstRecovery, AverageCashWeight: averageCash,
		TotalTransactionCostMinor: state.fees, TradeCount: state.tradeCount, Turnover: state.turnover,
		DeploymentCompleteDate: state.deploymentComplete,
	}, points, trades, nil
}

//nolint:lll // Schedule rows keep contribution and purchase budget adjacent for auditability.
func buildSchedule(in Input, start time.Time, spec strategySpec) []scheduledBudget {
	if in.Mode == ModeIncomeDCA {
		out := []scheduledBudget{}
		if in.IncomeDCA.InitialInvestmentMinor > 0 {
			out = append(out, scheduledBudget{date: start, contribution: in.IncomeDCA.InitialInvestmentMinor, budget: in.IncomeDCA.InitialInvestmentMinor, reason: "initial"})
		}
		for i := 0; i < in.HorizonMonths; i++ {
			out = append(out, scheduledBudget{date: start.AddDate(0, i, 0), contribution: in.IncomeDCA.MonthlyContributionMinor, budget: in.IncomeDCA.MonthlyContributionMinor, reason: "scheduled"})
		}
		return out
	}
	capital := in.ExistingCapital.InitialCapitalMinor
	out := []scheduledBudget{{date: start, contribution: capital, budget: capital, reason: "initial"}}
	switch spec.kind {
	case kindIncomeDCA, kindCashBaseline:
		return out
	case kindLumpSum:
		return out
	case kindPhaseIn:
		return phaseSchedule(capital, spec.months, start)
	case kindStatic, kindThreshold:
		out[0].budget = int64(math.Round(float64(capital) * spec.target))
		return out
	default:
		return out
	}
}

func phaseSchedule(capital int64, months int, start time.Time) []scheduledBudget {
	out := make([]scheduledBudget, 0, months)
	base := capital / int64(months)
	for i := 0; i < months; i++ {
		amount := base
		if i == months-1 {
			amount += capital % int64(months)
		}
		item := scheduledBudget{date: start.AddDate(0, i, 0), budget: amount, reason: "scheduled"}
		if i == 0 {
			item.contribution = capital
			item.reason = "initial"
		}
		out = append(out, item)
	}
	return out
}

func executeBuy(state *accountState, price float64, gross int64, costRate float64, date, reason string) (Trade, error) {
	if gross <= 0 || gross > state.cash {
		return Trade{}, fmt.Errorf("%w: invalid gross budget", ErrTradeBudgetTooSmall)
	}
	fee := int64(math.Round(float64(gross) * costRate))
	net := gross - fee
	if net <= 0 {
		return Trade{}, ErrTradeBudgetTooSmall
	}
	before := valueOf(state.units, price)
	state.units += float64(net) / price
	after := valueOf(state.units, price)
	assetDelta := after - before
	state.cash -= gross
	state.fees += fee
	state.tradeCount++
	state.turnover += float64(gross) / math.Max(1, float64(before+state.cash+gross))
	return Trade{
		TradeDate: date, Side: "buy", Reason: reason, GrossTradeMinor: gross, FeeMinor: fee,
		AssetValueDeltaMinor: assetDelta, CashDeltaMinor: -gross,
	}, nil
}

func executeTargetBuild(state *accountState, price, target, costRate float64, date string) (Trade, error) {
	value := valueOf(state.units, price) + state.cash
	turnover := target
	fee := int64(math.Round(float64(value) * turnover * costRate))
	if value-fee <= 0 {
		return Trade{}, ErrTradeBudgetTooSmall
	}
	targetAsset := int64(math.Round(float64(value-fee) * target))
	if targetAsset <= 0 {
		return Trade{}, ErrTradeBudgetTooSmall
	}
	oldAsset, oldCash := valueOf(state.units, price), state.cash
	state.units = float64(targetAsset) / price
	state.cash = value - fee - targetAsset
	state.fees += fee
	state.tradeCount++
	state.turnover += turnover
	return Trade{
		TradeDate: date, Side: "buy", Reason: "initial", GrossTradeMinor: targetAsset + fee,
		FeeMinor: fee, AssetValueDeltaMinor: targetAsset - oldAsset, CashDeltaMinor: state.cash - oldCash,
	}, nil
}

//nolint:lll // Rebalance arguments mirror the formula inputs.
func rebalance(state *accountState, price float64, spec strategySpec, costRate float64, date string) (Trade, bool, error) {
	asset := valueOf(state.units, price)
	value := asset + state.cash
	if value <= 0 {
		return Trade{}, false, nil
	}
	weight := float64(asset) / float64(value)
	if math.Abs(weight-spec.target)+1e-12 < spec.threshold {
		return Trade{}, false, nil
	}
	turnover := .5 * (math.Abs(weight-spec.target) + math.Abs((1-weight)-(1-spec.target)))
	fee := int64(math.Round(float64(value) * turnover * costRate))
	if value-fee <= 0 {
		return Trade{}, false, ErrTradeBudgetTooSmall
	}
	afterValue := value - fee
	targetAsset := int64(math.Round(float64(afterValue) * spec.target))
	targetCash := afterValue - targetAsset
	assetDelta := targetAsset - asset
	cashDelta := targetCash - state.cash
	state.units = float64(targetAsset) / price
	state.cash = targetCash
	state.fees += fee
	state.tradeCount++
	state.turnover += turnover
	side := "buy"
	if assetDelta < 0 {
		side = "sell"
	}
	return Trade{
		TradeDate: date, Side: side, Reason: "threshold", GrossTradeMinor: abs64(assetDelta), FeeMinor: fee,
		AssetValueDeltaMinor: assetDelta, CashDeltaMinor: cashDelta,
	}, true, nil
}

//nolint:lll // Strategy construction is intentionally declarative.
func specs(in Input) []strategySpec {
	if in.Mode == ModeIncomeDCA {
		return []strategySpec{{key: StrategyIncomeDCA, kind: kindIncomeDCA}, {key: StrategyIncomeCashBaseline, kind: kindCashBaseline}}
	}
	out := []strategySpec{{key: StrategyLumpSum, kind: kindLumpSum}}
	for _, months := range in.ExistingCapital.PhaseInMonths {
		out = append(out, strategySpec{key: fmt.Sprintf("phase_in_%dm", months), kind: kindPhaseIn, months: months})
	}
	if t := in.ExistingCapital.Threshold; t != nil && t.Enabled {
		out = append(out,
			strategySpec{key: weightKey("static", t.TargetAssetWeight, 0), kind: kindStatic, target: t.TargetAssetWeight},
			strategySpec{key: weightKey("threshold", t.TargetAssetWeight, t.RebalanceThreshold), kind: kindThreshold, target: t.TargetAssetWeight, threshold: t.RebalanceThreshold},
		)
	}
	return out
}

func scheduledReason(tradeCount int) string {
	if tradeCount == 0 {
		return "initial"
	}
	return "scheduled"
}

func hasNoFutureInternalBudget(schedule []scheduledBudget, date time.Time) bool {
	for _, item := range schedule {
		if item.budget > 0 && item.date.After(date) {
			return false
		}
	}
	return true
}

func valueOf(units, price float64) int64 {
	if units == 0 || price == 0 {
		return 0
	}
	return int64(math.Round(units * price))
}

func unitNAV(value int64, units float64) float64 {
	if units <= 0 {
		return 1
	}
	return float64(value) / units
}

func applyLumpSumDeltas(windows []WindowResult) {
	baseline := make(map[string]WindowResult)
	for _, w := range windows {
		if w.StrategyKey == StrategyLumpSum {
			baseline[w.WindowStart] = w
		}
	}
	for i := range windows {
		b, ok := baseline[windows[i].WindowStart]
		if !ok || windows[i].StrategyKey == StrategyLumpSum {
			continue
		}
		delta := windows[i].TerminalValueMinor - b.TerminalValueMinor
		dd := windows[i].MaxDrawdown - b.MaxDrawdown
		windows[i].TerminalDeltaVsLumpSumMinor = &delta
		windows[i].DrawdownDeltaVsLumpSum = &dd
	}
}

//nolint:gocognit,gocyclo,nestif,lll // Aggregation keeps stable ordering, quantiles and paired comparisons in one pass.
func aggregate(in Input, windows []WindowResult) []StrategyAggregate {
	byStrategy := map[string][]WindowResult{}
	for _, window := range windows {
		byStrategy[window.StrategyKey] = append(byStrategy[window.StrategyKey], window)
	}
	order := strategyKeys(in)
	out := make([]StrategyAggregate, 0, len(order))
	for _, key := range order {
		rows := byStrategy[key]
		sort.Slice(rows, func(i, j int) bool { return rows[i].WindowStart < rows[j].WindowStart })
		xirrs := []float64{}
		for _, row := range rows {
			if row.XIRR != nil {
				xirrs = append(xirrs, *row.XIRR)
			}
		}
		a := StrategyAggregate{
			StrategyKey: key, WindowCount: len(rows), TerminalValue: quantiles(mapFloat(rows, func(w WindowResult) float64 { return float64(w.TerminalValueMinor) })),
			Profit: quantiles(mapFloat(rows, func(w WindowResult) float64 { return float64(w.ProfitMinor) })), XIRR: quantiles(xirrs), XIRRCount: len(xirrs),
			TWRAnnualized:  quantiles(mapFloat(rows, func(w WindowResult) float64 { return w.TWRAnnualized })),
			MaxDrawdown:    quantiles(mapFloat(rows, func(w WindowResult) float64 { return w.MaxDrawdown })),
			UnderwaterDays: quantiles(mapFloat(rows, func(w WindowResult) float64 { return float64(w.LongestUnderwaterDays) })),
		}
		if len(rows) > 0 {
			best, worst := rows[0], rows[0]
			for _, row := range rows[1:] {
				if row.TerminalValueMinor > best.TerminalValueMinor {
					best = row
				}
				if row.TerminalValueMinor < worst.TerminalValueMinor {
					worst = row
				}
			}
			a.BestStart, a.WorstStart = best.WindowStart, worst.WindowStart
		}
		baseline := StrategyLumpSum
		if strings.HasPrefix(key, "threshold_") {
			parts := strings.Split(key, "_")
			if len(parts) >= 3 {
				baseline = "static_" + parts[1]
			}
		}
		if key != StrategyLumpSum && in.Mode == ModeExistingCapital {
			a.BaselineKey = baseline
			baseByStart := map[string]WindowResult{}
			for _, row := range byStrategy[baseline] {
				baseByStart[row.WindowStart] = row
			}
			for _, row := range rows {
				b, ok := baseByStart[row.WindowStart]
				if !ok {
					continue
				}
				a.PairedWindowCount++
				if row.TerminalValueMinor > b.TerminalValueMinor {
					a.HigherTerminalCount++
				}
				if row.MaxDrawdown > b.MaxDrawdown && row.TerminalValueMinor < b.TerminalValueMinor {
					a.TradeoffCount++
				}
			}
			if a.PairedWindowCount > 0 {
				a.HigherTerminalRatio = float64(a.HigherTerminalCount) / float64(a.PairedWindowCount)
			}
		}
		out = append(out, a)
	}
	return out
}

func mapFloat(rows []WindowResult, fn func(WindowResult) float64) []float64 {
	out := make([]float64, len(rows))
	for i, row := range rows {
		out[i] = fn(row)
	}
	return out
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
