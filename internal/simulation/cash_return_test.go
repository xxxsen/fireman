package simulation

import (
	"math"
	"testing"
)

// cashOnlySnapshot builds an all-cash, no-cash-flow plan over horizonMonths so the
// only thing moving the balance is the deterministic cash return (td/063 R1).
func cashOnlySnapshot(horizonMonths int, deterministic bool) *InputSnapshot {
	endAge := 30 + horizonMonths/12
	in := &InputSnapshot{
		EngineVersion: EngineVersion,
		BaseCurrency:  "CNY",
		Parameters: SnapshotParameters{
			CurrentAge: 30, RetirementAge: 55, EndAge: endAge,
			TotalAssetsMinor: 1_000_000_00, AnnualSavingsMinor: 0,
			AnnualSpendingMinor: 400_000_00, TerminalWealthFloorMinor: 0,
			InflationMode: "fixed_real", FixedInflationRate: 0.03,
			WithdrawalType: "fixed_real", WithdrawalRate: 0.04,
			WithdrawalFloorRatio: 0.7, WithdrawalCeilingRatio: 1.3,
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			SimulationRuns: 1, StudentTDf: 7, Seed: "42",
		},
		Assets: []SnapshotAsset{{
			HoldingID: "cash", InstrumentID: "cny_cash", SnapshotID: "s_cash",
			Currency: "CNY", AssetClass: "cash", Region: "domestic", IsCash: true,
			InitialMinor: 1_000_000_00, TargetWeight: 1.0,
			ModeledAnnualReturn: 0.018, AnnualVolatility: 0,
			ForwardAnnualGeometricReturn: 0.018, ForwardLogReturn: math.Log(1.018),
			SourceHash: "cash_hash",
		}},
		DeterministicCashReturn: deterministic,
	}
	return in
}

func TestForwardCashGrowsDeterministically(t *testing.T) {
	cases := []struct {
		name   string
		months int
		factor float64
	}{
		{"one year", 12, 1.018},
		{"ten years", 120, math.Pow(1.018, 10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := cashOnlySnapshot(tc.months, true)
			summary, _ := RunPath(in, 0, PathRunOpts{})
			want := int64(math.Round(1_000_000_00 * tc.factor))
			got := summary.TerminalWealthMinor
			// Allow minor-unit rounding from monthly compounding.
			if d := got - want; d < -100 || d > 100 {
				t.Fatalf("terminal cash wealth=%d want≈%d (diff=%d)", got, want, d)
			}
		})
	}
}

func TestLegacyCashStaysFlat(t *testing.T) {
	in := cashOnlySnapshot(120, false)
	summary, _ := RunPath(in, 0, PathRunOpts{})
	if summary.TerminalWealthMinor != 1_000_000_00 {
		t.Fatalf("legacy cash should stay flat at 0%%, got %d", summary.TerminalWealthMinor)
	}
}

// TestForwardCashSeedIndependent verifies the cash growth does not depend on the
// RNG seed or Student-t df (td/063 R1 acceptance #2).
func TestForwardCashSeedIndependent(t *testing.T) {
	a := cashOnlySnapshot(120, true)
	a.Parameters.Seed = "1"
	a.Parameters.StudentTDf = 5
	b := cashOnlySnapshot(120, true)
	b.Parameters.Seed = "987654321"
	b.Parameters.StudentTDf = 30
	sa, _ := RunPath(a, 0, PathRunOpts{})
	sb, _ := RunPath(b, 0, PathRunOpts{})
	if sa.TerminalWealthMinor != sb.TerminalWealthMinor {
		t.Fatalf("cash growth must be seed/df independent: %d vs %d",
			sa.TerminalWealthMinor, sb.TerminalWealthMinor)
	}
}
