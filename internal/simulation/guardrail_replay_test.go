package simulation

import (
	"math"
	"testing"
)

// guardrailReplayFixture is a frozen guardrail snapshot as it would have been
// persisted by the 3.0.0 engine. Replaying it must reproduce the annual-reset
// guardrail semantics its stored summary and path index were computed with.
func guardrailReplayFixture(engineVersion string) *InputSnapshot {
	return &InputSnapshot{
		EngineVersion: engineVersion,
		BaseCurrency:  "CNY",
		Parameters: SnapshotParameters{
			CurrentAge: 50, RetirementAge: 52, EndAge: 85,
			TotalAssetsMinor:    10_000_000_00,
			AnnualSavingsMinor:  200_000_00,
			AnnualSpendingMinor: 400_000_00,
			InflationMode:       "fixed", FixedInflationRate: 0.025,
			WithdrawalType: "guardrail", WithdrawalFloorRatio: 0.7, WithdrawalCeilingRatio: 1.3,
			WithdrawalTaxRate: 0.1, TaxableWithdrawalRatio: 0.5,
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			TransactionCostRate: 0.001,
			SimulationRuns:      8, StudentTDf: 7, Seed: "20260706",
		},
		Assets: []SnapshotAsset{
			{
				HoldingID: "h-eq", AssetKey: "eq", SnapshotID: "s1", Currency: "CNY",
				AssetClass: "equity", InitialMinor: 6_000_000_00, TargetWeight: 0.6,
				ModeledAnnualReturn: 0.07, AnnualVolatility: 0.18, SourceHash: "eq",
			},
			{
				HoldingID: "h-bond", AssetKey: "bond", SnapshotID: "s2", Currency: "CNY",
				AssetClass: "bond", InitialMinor: 3_000_000_00, TargetWeight: 0.3,
				ModeledAnnualReturn: 0.03, AnnualVolatility: 0.05, SourceHash: "bd",
			},
			{
				HoldingID: "h-cash", AssetKey: "cash", SnapshotID: "s3", Currency: "CNY",
				AssetClass: "cash", IsCash: true, InitialMinor: 1_000_000_00, TargetWeight: 0.1,
				SourceHash: "ca",
			},
		},
	}
}

// guardrailReplayGolden pins RunPath outputs captured by executing this exact
// fixture on the real 3.0.0 engine (pre-compounding-fix code), so replay
// equivalence is asserted against genuinely historical behavior rather than a
// reimplementation.
type guardrailReplayGolden struct {
	terminal     int64
	totalSpend   int64
	txCost       int64
	maxDD        float64
	realTerminal int64
}

var guardrailReplayGoldens = []guardrailReplayGolden{
	{terminal: 1463789676, totalSpend: 2100700872, txCost: 7675237, maxDD: 0.6602064415, realTerminal: 616798617},
	{terminal: 187670748, totalSpend: 1924157124, txCost: 4510902, maxDD: 0.8720164639, realTerminal: 79079023},
	{terminal: 1579975168, totalSpend: 2011232736, txCost: 5330859, maxDD: 0.5382764315, realTerminal: 665755821},
	{terminal: 598257578, totalSpend: 1990972536, txCost: 5185161, maxDD: 0.6808255689, realTerminal: 252088434},
}

func TestGuardrailReplayReproducesAnnualResetSemantics(t *testing.T) {
	in := guardrailReplayFixture("3.0.0")
	for pathNo, want := range guardrailReplayGoldens {
		s, _ := RunPath(in, pathNo, PathRunOpts{})
		if !s.Succeeded || s.FailureMonth != nil {
			t.Fatalf("path %d: golden run succeeded, replay did not (failureMonth=%v)", pathNo, s.FailureMonth)
		}
		if s.TerminalWealthMinor != want.terminal {
			t.Fatalf("path %d terminal = %d, want %d", pathNo, s.TerminalWealthMinor, want.terminal)
		}
		if s.TotalSpendingMinor != want.totalSpend {
			t.Fatalf("path %d total spending = %d, want %d", pathNo, s.TotalSpendingMinor, want.totalSpend)
		}
		if s.TransactionCostMinor != want.txCost {
			t.Fatalf("path %d tx cost = %d, want %d", pathNo, s.TransactionCostMinor, want.txCost)
		}
		if math.Abs(s.MaxDrawdown-want.maxDD) > 1e-9 {
			t.Fatalf("path %d max drawdown = %.10f, want %.10f", pathNo, s.MaxDrawdown, want.maxDD)
		}
		if s.RealTerminalWealthMinor != want.realTerminal {
			t.Fatalf("path %d real terminal = %d, want %d", pathNo, s.RealTerminalWealthMinor, want.realTerminal)
		}

		// Path regeneration (the path-detail page) must agree with the stored
		// summary of the historical run, not with current-version semantics.
		detail := RegeneratePathDetail(in, pathNo)
		var detailSpend, lastWealth int64
		for _, m := range detail.Monthly {
			detailSpend += m.SpendingMinor
			lastWealth = m.TotalWealthMinor
		}
		if detailSpend != want.totalSpend {
			t.Fatalf("path %d regenerated spending = %d, want %d", pathNo, detailSpend, want.totalSpend)
		}
		if lastWealth != want.terminal {
			t.Fatalf("path %d regenerated terminal = %d, want %d", pathNo, lastWealth, want.terminal)
		}
	}
}

func TestGuardrailReplayLegacyEngineVersionAlsoGated(t *testing.T) {
	if !GuardrailUsesLegacyAnnualReset("2.0.0") || !GuardrailUsesLegacyAnnualReset("3.0.0") {
		t.Fatal("2.0.0 and 3.0.0 snapshots must replay annual-reset guardrail semantics")
	}
	if GuardrailUsesLegacyAnnualReset("3.1.0") || GuardrailUsesLegacyAnnualReset(EngineVersion) {
		t.Fatal("3.1.0+ snapshots must use compounding guardrail semantics")
	}
}

// The same snapshot frozen at 3.1.0 must diverge from the 3.0.0 goldens:
// the compounding fix is real behavior change, and the gate must not leak
// legacy semantics into current-version runs.
func TestGuardrailCurrentVersionDivergesFromAnnualResetGolden(t *testing.T) {
	in := guardrailReplayFixture(EngineVersion)
	diverged := false
	for pathNo, want := range guardrailReplayGoldens {
		s, _ := RunPath(in, pathNo, PathRunOpts{})
		if s.TotalSpendingMinor != want.totalSpend || s.TerminalWealthMinor != want.terminal {
			diverged = true
			break
		}
	}
	if !diverged {
		t.Fatal("fixture never triggers a guardrail difference; strengthen the fixture")
	}
}
