package investmentpath

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func weekdayPrices(t *testing.T, start, end string, value func(time.Time) float64) []PricePoint {
	t.Helper()
	lo, err := time.Parse(DateLayout, start)
	if err != nil {
		t.Fatal(err)
	}
	hi, err := time.Parse(DateLayout, end)
	if err != nil {
		t.Fatal(err)
	}
	var out []PricePoint
	for d := lo; !d.After(hi); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		out = append(out, PricePoint{Date: d.Format(DateLayout), Value: value(d)})
	}
	return out
}

func baseInput(t *testing.T) Input {
	t.Helper()
	return Input{
		Mode: ModeIncomeDCA, EvaluationStart: "2020-01-01", EvaluationEnd: "2022-12-31",
		HorizonMonths: 12, PrimaryStart: "2022-01-15", MonthlyDay: 15,
		IncomeDCA: &IncomeDCAConfig{InitialInvestmentMinor: 50, MonthlyContributionMinor: 100},
		Prices:    weekdayPrices(t, "2019-12-01", "2024-01-10", func(time.Time) float64 { return 10 }),
	}
}

func TestIncomeDCAUsesSameExternalFlowsAsCashBaseline(t *testing.T) {
	result, err := Run(context.Background(), baseInput(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Primary) != 2 {
		t.Fatalf("primary strategies = %d", len(result.Primary))
	}
	byKey := map[string]WindowResult{}
	for _, row := range result.Primary {
		byKey[row.StrategyKey] = row
	}
	dca, cash := byKey[StrategyIncomeDCA], byKey[StrategyIncomeCashBaseline]
	if dca.TotalContributionMinor != 1250 || cash.TotalContributionMinor != dca.TotalContributionMinor {
		t.Fatalf("contributions dca=%d cash=%d", dca.TotalContributionMinor, cash.TotalContributionMinor)
	}
	if dca.TerminalValueMinor != 1250 || cash.TerminalValueMinor != 1250 {
		t.Fatalf("terminal dca=%d cash=%d", dca.TerminalValueMinor, cash.TerminalValueMinor)
	}
	if dca.XIRR == nil || math.Abs(*dca.XIRR) > 1e-8 || cash.XIRR == nil || math.Abs(*cash.XIRR) > 1e-8 {
		t.Fatalf("unexpected XIRR dca=%v cash=%v", dca.XIRR, cash.XIRR)
	}
	if cash.TradeCount != 0 || cash.TotalTransactionCostMinor != 0 {
		t.Fatalf("cash baseline traded: %+v", cash)
	}
	for _, point := range result.Points {
		if point.AccountValueMinor != point.AssetValueMinor+point.CashValueMinor {
			t.Fatalf("account invariant failed on %s", point.ValuationDate)
		}
	}
}

func TestPlannedNonTradingDayQueuesPurchase(t *testing.T) {
	result, err := Run(context.Background(), baseInput(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, trade := range result.Trades {
		if trade.StrategyKey != StrategyIncomeDCA {
			continue
		}
		if trade.SequenceNo == 1 && trade.TradeDate != "2022-01-17" {
			t.Fatalf("first trade date = %s, want 2022-01-17", trade.TradeDate)
		}
		if trade.TradeDate == "2022-01-15" {
			t.Fatal("trade was created on a non-trading day")
		}
	}
}

func TestExistingCapitalPhaseBudgetsAndInitialFees(t *testing.T) {
	in := baseInput(t)
	in.Mode = ModeExistingCapital
	in.IncomeDCA = nil
	in.ExistingCapital = &ExistingCapitalConfig{InitialCapitalMinor: 10_000, PhaseInMonths: []int{3}}
	in.TransactionCostRate = .01
	result, err := Run(context.Background(), in, nil)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]WindowResult{}
	for _, row := range result.Primary {
		byKey[row.StrategyKey] = row
	}
	if byKey[StrategyLumpSum].TotalTransactionCostMinor != 100 {
		t.Fatalf("lump fee = %d", byKey[StrategyLumpSum].TotalTransactionCostMinor)
	}
	if byKey["phase_in_3m"].TradeCount != 3 || byKey["phase_in_3m"].TotalContributionMinor != 10_000 {
		t.Fatalf("phase result = %+v", byKey["phase_in_3m"])
	}
	gross := int64(0)
	for _, trade := range result.Trades {
		if trade.StrategyKey == "phase_in_3m" {
			gross += trade.GrossTradeMinor
		}
	}
	if gross != 10_000 {
		t.Fatalf("phase gross budgets = %d", gross)
	}
}

func TestThresholdUsesInclusiveBoundary(t *testing.T) {
	state := accountState{units: 5, cash: 500}
	spec := strategySpec{target: .5, threshold: .1}
	trade, ok, err := rebalance(&state, 150, spec, 0, "2024-01-02")
	if err != nil {
		t.Fatal(err)
	}
	// 750/(750+500)=0.6: the deviation is exactly 0.1.
	if !ok || trade.Reason != "threshold" {
		t.Fatalf("inclusive threshold did not trigger: ok=%v trade=%+v", ok, trade)
	}
}

func TestValidateRejectsMixedModeAndDuplicatePrices(t *testing.T) {
	in := baseInput(t)
	in.ExistingCapital = &ExistingCapitalConfig{InitialCapitalMinor: 100}
	if _, _, err := ValidateAndResolve(in); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mixed union error = %v", err)
	}
	in = baseInput(t)
	in.Prices = append(in.Prices, in.Prices[0])
	if _, _, err := ValidateAndResolve(in); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate price error = %v", err)
	}
}

func TestValidateExcludesWindowWhoseOpeningHistoryGapExceedsTolerance(t *testing.T) {
	in := baseInput(t)
	in.EvaluationStart = "2019-01-01"
	in.EvaluationEnd = "2020-12-31"
	in.PrimaryStart = ""
	in.MaxTailGapDays = 10

	_, resolved, err := ValidateAndResolve(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.WindowStarts) == 0 || resolved.WindowStarts[0] != "2019-12-15" {
		t.Fatalf("window starts = %v, want first eligible start 2019-12-15", resolved.WindowStarts)
	}
	for _, start := range resolved.WindowStarts {
		if start < "2019-12-01" {
			t.Fatalf("pre-history window was accepted: %s", start)
		}
	}
}

func TestXIRRKnownAnnualReturnAndNoRoot(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	rate, reason := solveXIRR([]cashFlow{{date: start, amount: -1000}, {date: end, amount: 1100}})
	if reason != "" || rate == nil || math.Abs(*rate-.1) > .001 {
		t.Fatalf("rate=%v reason=%q", rate, reason)
	}
	if rate, reason = solveXIRR([]cashFlow{{date: start, amount: -1000}}); rate != nil || reason == "" {
		t.Fatalf("expected no root, got rate=%v reason=%q", rate, reason)
	}
}

func TestQuantilesUseLinearInterpolation(t *testing.T) {
	q := quantiles([]float64{0, 10})
	if q.P10 != 1 || q.P50 != 5 || q.P90 != 9 {
		t.Fatalf("quantiles = %+v", q)
	}
}
