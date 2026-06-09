package simulation

import (
	"math"
	"testing"

	"github.com/fireman/fireman/internal/domain"
)

func TestYearAccountingIdentityWithWithdrawalTax(t *testing.T) {
	in := testInputSnapshot()
	in.Parameters.RetirementAge = in.Parameters.CurrentAge
	in.Parameters.EndAge = in.Parameters.CurrentAge + 2
	in.Parameters.AnnualSavingsMinor = 0
	in.Parameters.AnnualSpendingMinor = 120_000_00
	in.Parameters.WithdrawalTaxRate = 0.20
	in.Parameters.TaxableWithdrawalRatio = 1.0

	detail := RegeneratePathDetail(in, 0)
	if len(detail.Yearly) == 0 {
		t.Fatal("expected yearly records")
	}

	for _, month := range detail.Monthly {
		if month.TaxMinor > 0 && month.SpendingMinor > 0 {
			gross := month.SpendingMinor + month.TaxMinor
			_, tax := GrossWithdrawal(month.SpendingMinor, in.Parameters.WithdrawalTaxRate, in.Parameters.TaxableWithdrawalRatio)
			if tax != month.TaxMinor {
				t.Fatalf("tax mismatch: month=%d tax=%d expected=%d", month.MonthOffset, month.TaxMinor, tax)
			}
			if gross <= month.SpendingMinor {
				t.Fatalf("expected gross withdrawal > net spending at month %d", month.MonthOffset)
			}
		}
	}

	for _, year := range detail.Yearly {
		expectedEnd := year.StartWealthMinor + year.IncomeMinor + year.InvestmentGainLoss -
			year.SpendingMinor - year.TaxMinor - year.TransactionCost
		if diff := int64(math.Abs(float64(expectedEnd - year.EndWealthMinor))); diff > 1 {
			t.Fatalf("year %d accounting identity failed: expected=%d actual=%d diff=%d",
				year.Year, expectedEnd, year.EndWealthMinor, diff)
		}
	}
}

func TestYearEndDrawdownLessThanMaxIntraYear(t *testing.T) {
	in := &InputSnapshot{
		EngineVersion: EngineVersion,
		BaseCurrency:  "CNY",
		Parameters: SnapshotParameters{
			CurrentAge: 55, RetirementAge: 55, EndAge: 57,
			TotalAssetsMinor: 1_000_000_00, AnnualSavingsMinor: 0,
			AnnualSpendingMinor: 0, TerminalWealthFloorMinor: 0,
			InflationMode: "fixed", FixedInflationRate: 0,
			WithdrawalType: "fixed_real", WithdrawalRate: 0,
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			SimulationRuns: 1, StudentTDf: 7, Seed: "1",
		},
		Assets: []SnapshotAsset{{
			HoldingID: "h1", InstrumentID: "i1", SnapshotID: "s1",
			Currency: "CNY", AssetClass: domain.AssetClassEquity, IsCash: false,
			InitialMinor: 1_000_000_00, TargetWeight: 1.0,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15, MaxDrawdown: 0.30,
			SourceHash: "eq",
		}},
	}
	in.Parameters.Seed = "777"

	detail := RegeneratePathDetail(in, 0)
	found := false
	for _, year := range detail.Yearly {
		if year.MaxIntraYearDD > year.YearEndDrawdown+1e-9 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected at least one year where year-end drawdown is below intra-year max")
	}
}
