package simulation

import (
	"testing"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/quickfire"
)

func TestQuickFireMatchesDeterministicCashSimulation(t *testing.T) {
	quick := quickfire.Input{
		BaseCurrency:                     "CNY",
		CurrentAge:                       40,
		PlannedFireAge:                   42,
		EndAge:                           46,
		CurrentAssetsMinor:               1_000_000,
		AnnualSavingsMinor:               120_000,
		AnnualSavingsGrowthRate:          0,
		AnnualSpendingMinor:              96_000,
		AnnualRetirementIncomeMinor:      24_000,
		AnnualRetirementIncomeGrowthRate: 0,
		AnnualReturnRate:                 0.04,
		InflationRate:                    0.02,
		TerminalWealthFloorMinor:         0,
	}
	quickResult, err := quickfire.Calculate(quick)
	if err != nil {
		t.Fatalf("quickfire: %v", err)
	}
	in := &InputSnapshot{
		EngineVersion:           EngineVersion,
		BaseCurrency:            "CNY",
		DeterministicCashReturn: true,
		AggregateCashLiquidity:  true,
		Parameters: SnapshotParameters{
			CurrentAge: quick.CurrentAge, RetirementAge: quick.PlannedFireAge, EndAge: quick.EndAge,
			TotalAssetsMinor: quick.CurrentAssetsMinor, AnnualSavingsMinor: quick.AnnualSavingsMinor,
			AnnualSavingsGrowthRate: quick.AnnualSavingsGrowthRate, AnnualSpendingMinor: quick.AnnualSpendingMinor,
			AnnualRetirementIncomeMinor:      quick.AnnualRetirementIncomeMinor,
			AnnualRetirementIncomeGrowthRate: quick.AnnualRetirementIncomeGrowthRate,
			InflationMode:                    "fixed_real", FixedInflationRate: quick.InflationRate,
			WithdrawalType: "fixed_real", RebalanceFrequency: "annual", StudentTDf: 7, Seed: "42",
		},
		Assets: []SnapshotAsset{{
			HoldingID: "cash", AssetKey: "cash", Currency: "CNY", AssetClass: domain.AssetClassCash,
			IsCash: true, InitialMinor: quick.CurrentAssetsMinor, TargetWeight: 1,
			ModeledAnnualReturn: quick.AnnualReturnRate, AnnualVolatility: 0,
		}},
	}
	detail := RegeneratePathDetail(in, 0)
	if !detail.Succeeded {
		t.Fatalf("simulation failed: %+v", detail)
	}
	if len(detail.Yearly) != len(quickResult.Years) {
		t.Fatalf("year rows = %d/%d", len(detail.Yearly), len(quickResult.Years))
	}
	for i := range detail.Yearly {
		got, want := detail.Yearly[i], quickResult.Years[i]
		if got.EndWealthMinor != want.EndWealthMinor || got.IncomeMinor != want.IncomeMinor || got.SpendingMinor != want.SpendingMinor {
			t.Fatalf("year %d mismatch: simulation=%+v quick=%+v", i, got, want)
		}
	}
}
