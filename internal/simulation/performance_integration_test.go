//go:build integration

package simulation

import (
	"testing"
	"time"
)

func TestMonteCarloPerformanceBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped in short mode")
	}
	in := &InputSnapshot{
		EngineVersion: EngineVersion,
		PlanID:        "perf_plan",
		BaseCurrency:  "CNY",
		Parameters: SnapshotParameters{
			CurrentAge: 30, RetirementAge: 55, EndAge: 90,
			TotalAssetsMinor:    1_000_000_00,
			AnnualSpendingMinor: 400_000_00,
			AnnualSavingsMinor:  200_000_00,
			SimulationRuns:      10_000,
			StudentTDf:          7,
			Seed:                "perf-seed",
			InflationMode:       "fixed_real",
			FixedInflationRate:  0.03,
			WithdrawalType:      "fixed_real",
			WithdrawalRate:      0.04,
			RebalanceFrequency:  "annual",
			RebalanceThreshold:  0.03,
		},
		Assets: []SnapshotAsset{{
			HoldingID: "h1", SnapshotID: "s1", SourceHash: "fixture",
			InitialMinor: 1_000_000_00, TargetWeight: 1,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15,
		}},
	}
	start := time.Now()
	res := Run(in, RunOptions{Runs: 10_000})
	elapsed := time.Since(start)
	if res.SuccessCount+res.FailureCount != 10_000 {
		t.Fatalf("expected 10000 paths, got success=%d failure=%d", res.SuccessCount, res.FailureCount)
	}
	// Target 10s on four cores; allow 15s headroom for CI and WSL.
	if elapsed > 15*time.Second {
		t.Fatalf("10000-run simulation took %v, budget is 15s (design target 10s)", elapsed)
	}
	t.Logf("10000-run simulation completed in %v", elapsed)
}
