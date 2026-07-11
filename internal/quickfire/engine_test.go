package quickfire

import (
	"encoding/json"
	"errors"
	"testing"
)

func quickInput() Input {
	return Input{
		BaseCurrency:                     "CNY",
		CurrentAge:                       40,
		PlannedFireAge:                   40,
		EndAge:                           50,
		CurrentAssetsMinor:               1_201,
		AnnualSavingsMinor:               0,
		AnnualSavingsGrowthRate:          0,
		AnnualSpendingMinor:              120,
		AnnualRetirementIncomeMinor:      0,
		AnnualRetirementIncomeGrowthRate: 0,
		AnnualReturnRate:                 0,
		InflationRate:                    0,
		TerminalWealthFloorMinor:         0,
	}
}

func TestCalculateAnnualCompounding(t *testing.T) {
	in := quickInput()
	in.CurrentAge = 35
	in.PlannedFireAge = 36
	in.EndAge = 37
	in.CurrentAssetsMinor = 10_000
	in.AnnualSpendingMinor = 12
	in.AnnualRetirementIncomeMinor = 12
	in.AnnualReturnRate = 0.10
	result, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectedAssetsAtFireMinor != 11_000 {
		t.Fatalf("assets after one annual return = %d, want 11000", result.ProjectedAssetsAtFireMinor)
	}
}

func TestCalculateNegativeAnnualReturn(t *testing.T) {
	in := quickInput()
	in.CurrentAge = 35
	in.PlannedFireAge = 36
	in.EndAge = 37
	in.CurrentAssetsMinor = 10_000
	in.AnnualSpendingMinor = 12
	in.AnnualRetirementIncomeMinor = 12
	in.AnnualReturnRate = -0.10
	result, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectedAssetsAtFireMinor != 9_000 {
		t.Fatalf("assets after one annual return = %d, want 9000", result.ProjectedAssetsAtFireMinor)
	}
}

func TestCalculateZeroRateDepletion(t *testing.T) {
	in := quickInput()
	in.EndAge = 51
	in.CurrentAssetsMinor = 1_200
	result, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.OutcomeStatus != OutcomeWealthDepleted || result.SustainableThroughEndAge {
		t.Fatalf("unexpected status: %+v", result)
	}
	if result.SupportMonthsAfterFire != 120 {
		t.Fatalf("support months = %d, want 120", result.SupportMonthsAfterFire)
	}
	if result.DepletionMonthOffset == nil || *result.DepletionMonthOffset != 119 {
		t.Fatalf("depletion offset = %v, want 119", result.DepletionMonthOffset)
	}
	if result.DepletionAgeYears == nil || *result.DepletionAgeYears != 50 ||
		result.DepletionAgeMonths == nil || *result.DepletionAgeMonths != 0 {
		t.Fatalf("depletion age = %v/%v, want 50/0", result.DepletionAgeYears, result.DepletionAgeMonths)
	}
}

func TestCalculateStableIncomeAndRequiredCapital(t *testing.T) {
	in := quickInput()
	in.EndAge = 60
	in.CurrentAssetsMinor = 1_201
	in.AnnualRetirementIncomeMinor = 60
	result, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequiredAssetsAtFireMinor != 1_201 {
		t.Fatalf("required capital = %d, want 1201", result.RequiredAssetsAtFireMinor)
	}
	if result.OutcomeStatus != OutcomeSustainable || result.TerminalWealthMinor != 1 {
		t.Fatalf("stable-income result = %+v", result)
	}
}

func TestCalculateAccumulation(t *testing.T) {
	in := quickInput()
	in.CurrentAge = 40
	in.PlannedFireAge = 45
	in.EndAge = 46
	in.CurrentAssetsMinor = 100
	in.AnnualSavingsMinor = 12
	in.AnnualSpendingMinor = 12
	in.AnnualRetirementIncomeMinor = 12
	result, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectedAssetsAtFireMinor != 160 {
		t.Fatalf("assets at fire = %d, want 160", result.ProjectedAssetsAtFireMinor)
	}
	if len(result.Years) < 5 {
		t.Fatalf("year rows = %d, want at least 5", len(result.Years))
	}
	if result.Years[0].Phase != "accumulation" {
		t.Fatalf("first phase = %q", result.Years[0].Phase)
	}
}

func TestRetirementCashFlowInflationTiming(t *testing.T) {
	in := quickInput()
	in.AnnualSpendingMinor = 12_000
	in.InflationRate = 0.12
	monthlyInflation := annualToMonthly(in.InflationRate)
	first, err := retirementCashFlow(in, 0, 0, monthlyInflation)
	if err != nil {
		t.Fatal(err)
	}
	thirteenth, err := retirementCashFlow(in, 12, 0, monthlyInflation)
	if err != nil {
		t.Fatal(err)
	}
	if first.spending != 1_000 || thirteenth.spending != 1_120 {
		t.Fatalf("spending = %d/%d, want 1000/1120", first.spending, thirteenth.spending)
	}
}

func TestRequiredAssetsForwardParity(t *testing.T) {
	cases := []struct {
		name             string
		annualReturn     float64
		inflation        float64
		retirementIncome int64
		floor            int64
	}{
		{name: "positive return", annualReturn: 0.04, inflation: 0.02},
		{name: "negative return", annualReturn: -0.10, inflation: 0.03, retirementIncome: 60},
		{name: "terminal floor", annualReturn: 0.02, inflation: 0, floor: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := quickInput()
			in.EndAge = 46
			in.AnnualReturnRate = tc.annualReturn
			in.InflationRate = tc.inflation
			in.AnnualRetirementIncomeMinor = tc.retirementIncome
			in.TerminalWealthFloorMinor = tc.floor
			monthlyReturn := annualToMonthly(in.AnnualReturnRate)
			monthlyInflation := annualToMonthly(in.InflationRate)
			required, err := requiredAssets(in, 0, 72, monthlyReturn, monthlyInflation)
			if err != nil {
				t.Fatal(err)
			}
			state, err := project(in, 0, 72, required, monthlyReturn, monthlyInflation, false)
			if err != nil {
				t.Fatal(err)
			}
			if state.status != OutcomeSustainable {
				t.Fatalf("required capital did not sustain: %+v", state)
			}
			want := maxInt64(tc.floor, 1)
			if got, err := roundMinor(state.terminal); err != nil || got < want || got > want+1 {
				t.Fatalf("terminal = %d err=%v, want [%d,%d]", got, err, want, want+1)
			}
		})
	}
}

func TestCalculateEarliestFireMonth(t *testing.T) {
	in := quickInput()
	in.EndAge = 43
	in.CurrentAssetsMinor = 241
	in.AnnualSavingsMinor = 120
	result, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.EarliestFireMonthOffset == nil || *result.EarliestFireMonthOffset != 6 {
		t.Fatalf("earliest month = %v, want 6", result.EarliestFireMonthOffset)
	}
}

func TestCalculateFinalMonthZeroIsDepleted(t *testing.T) {
	in := quickInput()
	in.CurrentAssetsMinor = 1_200
	result, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	if result.OutcomeStatus != OutcomeWealthDepleted || result.SustainableThroughEndAge {
		t.Fatalf("final-month zero must deplete: %+v", result)
	}
}

func TestCalculateYearLedgerAndDeterminism(t *testing.T) {
	in := quickInput()
	in.PlannedFireAge = 42
	in.EndAge = 48
	in.AnnualSavingsMinor = 120
	in.AnnualSavingsGrowthRate = 0.1
	in.AnnualRetirementIncomeMinor = 60
	in.AnnualRetirementIncomeGrowthRate = 0.05
	in.AnnualReturnRate = -0.1
	result, err := Calculate(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, year := range result.Years {
		if year.EndWealthMinor != year.StartWealthMinor+year.IncomeMinor-year.SpendingMinor+year.InvestmentGainMinor {
			t.Fatalf("ledger mismatch: %+v", year)
		}
	}
	first, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		replay, err := Calculate(in)
		if err != nil {
			t.Fatal(err)
		}
		second, err := json.Marshal(replay)
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Fatal("deterministic result changed")
		}
	}
}

func TestInputValidation(t *testing.T) {
	in := quickInput()
	in.AnnualReturnRate = -0.991
	err := in.Validate()
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["annual_return_rate"] == "" {
		t.Fatalf("expected annual return validation, got %v", err)
	}
}
