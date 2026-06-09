package stress

import (
	"testing"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

func TestBuiltinScenarioCount(t *testing.T) {
	if len(BuiltinScenarios()) != 7 {
		t.Fatalf("expected 7 built-in scenarios, got %d", len(BuiltinScenarios()))
	}
}

func TestStressShockOnlyInWindow(t *testing.T) {
	in := testStressInput()
	in.Parameters.SimulationRuns = 20
	baseline := simulation.Run(in, simulation.RunOptions{Runs: 20})
	sched := CompileSchedule(ScenarioEarlyRetirementCrash, in)
	stressed := Run(in, RunOptions{Runs: 20})

	if len(sched) == 0 {
		t.Fatal("expected non-empty schedule")
	}
	if stressed.BaselineSuccessProbability != float64(baseline.SuccessCount)/20 {
		t.Fatalf("baseline mismatch: stress=%f sim=%f", stressed.BaselineSuccessProbability, float64(baseline.SuccessCount)/20)
	}
	foundWorse := false
	for _, sc := range stressed.Scenarios {
		if sc.ScenarioID == ScenarioEarlyRetirementCrash && sc.SuccessProbability <= stressed.BaselineSuccessProbability {
			foundWorse = true
		}
	}
	if !foundWorse {
		t.Fatal("early retirement crash should not improve success vs baseline")
	}
}

func TestHistoricalMaxDrawdownUsesPerFundShock(t *testing.T) {
	in := testStressInput()
	in.Assets = append(in.Assets, simulation.SnapshotAsset{
		HoldingID: "h2", InstrumentID: "i2", SnapshotID: "s2",
		Currency: "CNY", AssetClass: domain.AssetClassBond, IsCash: false,
		InitialMinor: 0, TargetWeight: 0,
		ModeledAnnualReturn: 0.04, AnnualVolatility: 0.05, MaxDrawdown: 0.10,
		SourceHash: "bond",
	})
	sched := CompileSchedule(ScenarioHistoricalMaxDrawdown, in)
	start := shockStartMonth(in)
	ms := sched[start]
	if len(ms.Assets) != 2 {
		t.Fatalf("expected per-fund shocks, got %d", len(ms.Assets))
	}
}

func testStressInput() *simulation.InputSnapshot {
	return &simulation.InputSnapshot{
		EngineVersion: simulation.EngineVersion,
		BaseCurrency:  "CNY",
		Parameters: simulation.SnapshotParameters{
			CurrentAge: 55, RetirementAge: 55, EndAge: 65,
			TotalAssetsMinor: 5_000_000_00, AnnualSavingsMinor: 0,
			AnnualSpendingMinor: 200_000_00, TerminalWealthFloorMinor: 0,
			InflationMode: "fixed", FixedInflationRate: 0.03,
			WithdrawalType: "fixed_real", WithdrawalRate: 0.04,
			WithdrawalFloorRatio: 0.7, WithdrawalCeilingRatio: 1.3,
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			SimulationRuns: 100, StudentTDf: 7, Seed: "42",
		},
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "h1", InstrumentID: "i1", SnapshotID: "s1",
			Currency: "CNY", AssetClass: domain.AssetClassEquity, IsCash: false,
			InitialMinor: 5_000_000_00, TargetWeight: 1.0,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15, MaxDrawdown: 0.30,
			SourceHash: "eq",
		}},
	}
}
