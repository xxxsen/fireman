package simulation

import (
	"math"
	"testing"
)

func TestEngine34VersionGates(t *testing.T) {
	for _, version := range []string{"1.0.0", "2.0.0", "3.0.0", "3.3.0", "", "invalid"} {
		if UsesNetRetirementSettlement(version) || UsesStationaryInflationInitialState(version) ||
			UsesZeroPaddedFailureSeries(version) || UsesMonthPrecisionFailureAge(version) {
			t.Fatalf("legacy version %q unexpectedly enables 3.4 semantics", version)
		}
	}
	for _, version := range []string{"3.4.0", "3.4.1", "3.5.0", "4.0.0"} {
		if !UsesNetRetirementSettlement(version) || !UsesStationaryInflationInitialState(version) ||
			!UsesZeroPaddedFailureSeries(version) || !UsesMonthPrecisionFailureAge(version) {
			t.Fatalf("version %q does not enable all 3.4 semantics", version)
		}
	}
}

func TestSettleRetirementMonthGolden(t *testing.T) {
	cases := []struct {
		name                                       string
		spending, income, net, gross, tax, surplus int64
	}{
		{name: "no income", spending: 10_000, net: 10_000, gross: 12_500, tax: 2_500},
		{name: "partial income", spending: 10_000, income: 6_000, net: 4_000, gross: 5_000, tax: 1_000},
		{name: "income equals spending", spending: 10_000, income: 10_000},
		{name: "income exceeds spending", spending: 10_000, income: 12_000, surplus: 2_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SettleRetirementMonth(tc.spending, tc.income, 0.20, 1)
			if got.PortfolioNetNeededMinor != tc.net || got.GrossWithdrawalMinor != tc.gross ||
				got.WithdrawalTaxMinor != tc.tax || got.StableIncomeSurplusMinor != tc.surplus {
				t.Fatalf("settlement = %+v", got)
			}
		})
	}
}

func TestFundPathAmountRecordsPartialFunding(t *testing.T) {
	in := &InputSnapshot{AggregateCashLiquidity: true}
	slots := []assetSlot{
		{isCash: true, balance: 20},
		{balance: 80},
	}
	got := fundPathAmount(in, slots, 0, 125, 100, 0.20, 1, 0)
	if got.Sufficient || got.GrossFundedMinor != 100 || got.PortfolioNetFundedMinor != 80 ||
		got.TaxFundedMinor != 20 || got.TransactionCostMinor != 0 {
		t.Fatalf("partial funding = %+v", got)
	}
	if totalWealth(slots) != 0 {
		t.Fatalf("insufficient withdrawal must exhaust portfolio, got %d", totalWealth(slots))
	}
}

func TestRandomInflationInitialStateIsVersioned(t *testing.T) {
	newState := NewInflationState("3.4.0", "random_ar1", 0, 0.03, 0.5, 0, NewRNG(1))
	legacyState := NewInflationState("3.3.0", "random_ar1", 0, 0.03, 0.5, 0, NewRNG(1))
	newAnnual := math.Pow(1+newState.MonthlyRate(0), 12) - 1
	legacyAnnual := math.Pow(1+legacyState.MonthlyRate(0), 12) - 1
	if math.Abs(newAnnual-0.03) > 1e-12 {
		t.Fatalf("3.4 first-year inflation = %.12f, want 0.03", newAnnual)
	}
	if math.Abs(legacyAnnual-0.015) > 1e-12 {
		t.Fatalf("3.3 first-year inflation = %.12f, want 0.015", legacyAnnual)
	}
}

func TestRetirementIncomeOffsetsSpendingBeforeTax(t *testing.T) {
	current := cashRetirementFixture(EngineVersion, 100_000, 12_000, 12_000)
	summary, detail := RunPath(current, 0, PathRunOpts{CollectDetail: true})
	if !summary.Succeeded || summary.TerminalWealthMinor != 100_000 ||
		summary.TotalSpendingMinor != 12_000 || summary.TransactionCostMinor != 0 {
		t.Fatalf("3.4 equal-income path = %+v", summary)
	}
	for _, month := range detail.Monthly {
		if month.SpendingMinor != 1_000 || month.IncomeMinor != 1_000 || month.TaxMinor != 0 {
			t.Fatalf("month %d ledger = %+v", month.MonthOffset, month)
		}
	}

	legacy := cashRetirementFixture("3.3.0", 100_000, 12_000, 12_000)
	legacySummary, _ := RunPath(legacy, 0, PathRunOpts{})
	if legacySummary.TerminalWealthMinor != 97_000 || legacySummary.TotalSpendingMinor != 12_000 {
		t.Fatalf("3.3 replay changed: %+v", legacySummary)
	}
}

func TestFailedPathRecordsFailureMonthAndPadsZero(t *testing.T) {
	current := cashRetirementFixture(EngineVersion, 350, 1_200, 0)
	current.Parameters.WithdrawalTaxRate = 0
	summary, detail := RunPath(current, 0, PathRunOpts{CollectDetail: true, CollectMonthlyWealth: true})
	if summary.FailureMonth == nil || *summary.FailureMonth != 3 || summary.TotalSpendingMinor != 350 {
		t.Fatalf("3.4 failure summary = %+v", summary)
	}
	if len(summary.MonthlyWealthMinor) != 12 {
		t.Fatalf("monthly wealth length = %d", len(summary.MonthlyWealthMinor))
	}
	wantPrefix := []int64{250, 150, 50, 0}
	for i, want := range wantPrefix {
		if summary.MonthlyWealthMinor[i] != want {
			t.Fatalf("month %d wealth = %d, want %d", i, summary.MonthlyWealthMinor[i], want)
		}
	}
	for month := 4; month < len(summary.MonthlyWealthMinor); month++ {
		if summary.MonthlyWealthMinor[month] != 0 {
			t.Fatalf("failed path month %d padded with %d", month, summary.MonthlyWealthMinor[month])
		}
	}
	if len(detail.Monthly) != 4 || len(detail.Yearly) != 1 {
		t.Fatalf("failure detail lengths monthly=%d yearly=%d", len(detail.Monthly), len(detail.Yearly))
	}
	failure := detail.Monthly[3]
	if failure.SpendingRequestedMinor != 100 || failure.SpendingMinor != 50 ||
		failure.UnfundedSpendingMinor != 50 || failure.TotalWealthMinor != 0 {
		t.Fatalf("failure month ledger = %+v", failure)
	}

	legacy := cashRetirementFixture("3.3.0", 350, 1_200, 0)
	legacy.Parameters.WithdrawalTaxRate = 0
	legacySummary, legacyDetail := RunPath(legacy, 0, PathRunOpts{CollectDetail: true, CollectMonthlyWealth: true})
	if legacySummary.TotalSpendingMinor != 400 || len(legacyDetail.Monthly) != 3 ||
		legacySummary.MonthlyWealthMinor[3] != 50 {
		t.Fatalf("3.3 failed-path replay changed: summary=%+v detail=%+v", legacySummary, legacyDetail)
	}
}

func TestFailureAgeAtMonthEnd(t *testing.T) {
	cases := []struct {
		month int
		want  float64
	}{{0, 40 + 1.0/12}, {5, 40.5}, {11, 41}, {12, 41 + 1.0/12}}
	for _, tc := range cases {
		if got := FailureAgeAtMonthEnd(40, tc.month); math.Abs(got-tc.want) > 1e-12 {
			t.Fatalf("month %d failure age = %.12f, want %.12f", tc.month, got, tc.want)
		}
	}
}

func cashRetirementFixture(version string, balance, annualSpending, annualIncome int64) *InputSnapshot {
	return &InputSnapshot{
		EngineVersion:          version,
		BaseCurrency:           "CNY",
		AggregateCashLiquidity: true,
		Parameters: SnapshotParameters{
			CurrentAge: 60, RetirementAge: 60, EndAge: 61,
			TotalAssetsMinor: balance, AnnualSpendingMinor: annualSpending,
			AnnualRetirementIncomeMinor: annualIncome,
			InflationMode:               "fixed_real", FixedInflationRate: 0,
			WithdrawalType: "fixed_real", WithdrawalTaxRate: 0.20,
			TaxableWithdrawalRatio: 1, RebalanceFrequency: "annual",
			RebalanceThreshold: 0.03, StudentTDf: 7, Seed: "1",
		},
		Assets: []SnapshotAsset{{
			HoldingID: "cash", AssetKey: "SYS|cash||CNY", Currency: "CNY",
			AssetClass: "cash", IsCash: true, InitialMinor: balance, TargetWeight: 1,
		}},
	}
}
