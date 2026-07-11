package simulation

import (
	"math"
	"testing"

	"github.com/fireman/fireman/internal/domain"
)

func TestWithdrawCashOnlyNoTransactionCost(t *testing.T) {
	slots := []assetSlot{
		{isCash: true, balance: 1000, targetWeight: 1.0},
	}
	ok, cost := withdrawAmount(slots, 100, 0.01)
	if !ok || cost != 0 {
		t.Fatalf("expected no cost, ok=%v cost=%d", ok, cost)
	}
	if math.Abs(slots[0].balance-900) > 0.01 {
		t.Fatalf("expected 900 balance, got %v", slots[0].balance)
	}
}

func TestWithdrawSellAssetsIncludesTransactionCost(t *testing.T) {
	slots := []assetSlot{
		{balance: 1000, targetWeight: 1.0},
	}
	ok, cost := withdrawAmount(slots, 99, 0.01)
	if !ok {
		t.Fatal("expected success")
	}
	if cost != 1 {
		t.Fatalf("expected cost 1, got %d", cost)
	}
	total := slots[0].balance
	if math.Abs(total-900) > 0.02 {
		t.Fatalf("expected ~900 remaining, got %v", total)
	}
}

func TestWithdrawUsesAllCashSlotsProportionallyWithoutCost(t *testing.T) {
	slots := []assetSlot{
		{isCash: true, balance: 600},
		{isCash: true, balance: 400},
		{balance: 1000},
	}
	ok, cost := withdrawAmount(slots, 100, 0.01)
	if !ok || cost != 0 {
		t.Fatalf("cash-pool withdrawal expected no cost, ok=%v cost=%d", ok, cost)
	}
	if math.Abs(slots[0].balance-540) > 1e-9 || math.Abs(slots[1].balance-360) > 1e-9 || slots[2].balance != 1000 {
		t.Fatalf("cash pool was not consumed proportionally: %+v", slots)
	}
}

func TestWithdrawCashShortfallChargesOnlyRiskAssetSale(t *testing.T) {
	slots := []assetSlot{
		{isCash: true, balance: 30},
		{isCash: true, balance: 10},
		{balance: 960},
	}
	ok, cost := withdrawAmount(slots, 99, 0.01)
	if !ok || cost != 1 {
		t.Fatalf("expected successful risk sale with one unit cost, ok=%v cost=%d", ok, cost)
	}
	if math.Abs(slots[0].balance) > 1e-9 || math.Abs(slots[1].balance) > 1e-9 || math.Abs(slots[2].balance-900.404040404) > 1e-6 {
		t.Fatalf("unexpected balances after mixed withdrawal: %+v", slots)
	}
	if diff := math.Abs(sumBalances(slots) - (1000 - 99 - float64(cost))); diff > 0.5 {
		t.Fatalf("accounting identity failed by %v", diff)
	}
}

func TestSavingsAllocateAcrossCashTargets(t *testing.T) {
	slots := []assetSlot{
		{isCash: true, targetWeight: 0.3},
		{isCash: true, targetWeight: 0.1},
		{targetWeight: 0.6, balance: 1000},
	}
	addCash(slots, 400)
	if math.Abs(slots[0].balance-300) > 1e-9 || math.Abs(slots[1].balance-100) > 1e-9 || slots[2].balance != 1000 {
		t.Fatalf("savings not allocated 75/25 across cash targets: %+v", slots)
	}
}

func TestRebalanceDeductsTransactionCost(t *testing.T) {
	slots := []assetSlot{
		{balance: 600, targetWeight: 0.5},
		{balance: 400, targetWeight: 0.5},
	}
	cost := rebalanceToTarget(slots, 0.01)
	if cost <= 0 {
		t.Fatalf("expected positive cost, got %d", cost)
	}
	total := sumBalances(slots)
	if math.Abs(total-1000+float64(cost)) > 1 {
		t.Fatalf("total %v should equal 1000-cost(%d)", total, cost)
	}
	w0 := slots[0].balance / total
	w1 := slots[1].balance / total
	if math.Abs(w0-0.5) > 0.001 || math.Abs(w1-0.5) > 0.001 {
		t.Fatalf("weights not 50/50: %v %v", w0, w1)
	}
}

func TestSameSeedTxCostChangesTerminalWealth(t *testing.T) {
	inZero := minimalTxCostInput()
	inZero.Parameters.TransactionCostRate = 0
	inZero.Parameters.RebalanceThreshold = 0.01
	inZero.Parameters.RebalanceFrequency = "monthly"

	inCost := minimalTxCostInput()
	inCost.Parameters.TransactionCostRate = 0.01
	inCost.Parameters.RebalanceThreshold = 0.01
	inCost.Parameters.RebalanceFrequency = "monthly"

	s0, _ := RunPath(inZero, 0, PathRunOpts{})
	s1, _ := RunPath(inCost, 0, PathRunOpts{})
	if s0.TerminalWealthMinor == s1.TerminalWealthMinor {
		t.Fatalf("expected different terminal wealth: zero=%d cost=%d", s0.TerminalWealthMinor, s1.TerminalWealthMinor)
	}
	if s1.TerminalWealthMinor >= s0.TerminalWealthMinor {
		t.Fatalf("tx cost should reduce terminal wealth: zero=%d cost=%d", s0.TerminalWealthMinor, s1.TerminalWealthMinor)
	}
}

func minimalTxCostInput() *InputSnapshot {
	return &InputSnapshot{
		EngineVersion: EngineVersion,
		BaseCurrency:  "CNY",
		Parameters: SnapshotParameters{
			CurrentAge: 55, RetirementAge: 55, EndAge: 60,
			TotalAssetsMinor: 1_000_000_00, AnnualSavingsMinor: 0,
			AnnualSpendingMinor: 100_000_00, TerminalWealthFloorMinor: 0,
			InflationMode: "fixed_real", FixedInflationRate: 0.03,
			WithdrawalType: "fixed_real", WithdrawalRate: 0.04,
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			SimulationRuns: 5, StudentTDf: 7, Seed: "42",
		},
		Assets: []SnapshotAsset{{
			HoldingID: "h1", AssetKey: "i1", SnapshotID: "s1",
			Currency: "CNY", AssetClass: domain.AssetClassEquity, IsCash: false,
			InitialMinor: 1_000_000_00, TargetWeight: 1.0,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15, MaxDrawdown: 0.30,
			SourceHash: "eq",
		}},
	}
}
