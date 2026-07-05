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

func TestStressDeterministicWithFixedSeed(t *testing.T) {
	in := testStressInput()
	in.Parameters.SimulationRuns = 200
	in.Parameters.Seed = "12345"

	r1 := Run(in, RunOptions{Runs: 200})
	r2 := Run(in, RunOptions{Runs: 200})

	if r1.BaselineSuccessProbability != r2.BaselineSuccessProbability {
		t.Fatalf("baseline mismatch: %f vs %f", r1.BaselineSuccessProbability, r2.BaselineSuccessProbability)
	}
	if len(r1.Scenarios) != len(r2.Scenarios) {
		t.Fatalf("scenario count mismatch: %d vs %d", len(r1.Scenarios), len(r2.Scenarios))
	}
	for i := range r1.Scenarios {
		s1, s2 := r1.Scenarios[i], r2.Scenarios[i]
		if s1.ScenarioID != s2.ScenarioID {
			t.Fatalf("scenario id mismatch at %d", i)
		}
		if s1.SuccessProbability != s2.SuccessProbability {
			t.Fatalf("%s success probability mismatch: %f vs %f", s1.ScenarioID, s1.SuccessProbability, s2.SuccessProbability)
		}
		if s1.TerminalP50Minor != s2.TerminalP50Minor {
			t.Fatalf("%s terminal P50 mismatch: %d vs %d", s1.ScenarioID, s1.TerminalP50Minor, s2.TerminalP50Minor)
		}
		if s1.RecoveryNotWithinPlan != s2.RecoveryNotWithinPlan {
			t.Fatalf("%s recovery within-plan mismatch", s1.ScenarioID)
		}
		if (s1.RecoveryMonthP50 == nil) != (s2.RecoveryMonthP50 == nil) {
			t.Fatalf("%s recovery P50 nil mismatch", s1.ScenarioID)
		}
		if s1.RecoveryMonthP50 != nil && s2.RecoveryMonthP50 != nil && *s1.RecoveryMonthP50 != *s2.RecoveryMonthP50 {
			t.Fatalf("%s recovery P50 mismatch: %d vs %d", s1.ScenarioID, *s1.RecoveryMonthP50, *s2.RecoveryMonthP50)
		}
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
		t.Fatalf("baseline mismatch: stress=%f sim=%f", stressed.BaselineSuccessProbability,
			float64(baseline.SuccessCount)/20)
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
		HoldingID: "h2", AssetKey: "i2", SnapshotID: "s2",
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
			HoldingID: "h1", AssetKey: "i1", SnapshotID: "s1",
			Currency: "CNY", AssetClass: domain.AssetClassEquity, IsCash: false,
			InitialMinor: 5_000_000_00, TargetWeight: 1.0,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15, MaxDrawdown: 0.30,
			SourceHash: "eq",
		}},
	}
}
