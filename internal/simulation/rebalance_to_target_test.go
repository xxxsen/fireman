package simulation

import (
	"math"
	"math/rand"
	"testing"

	"github.com/fireman/fireman/internal/domain"
)

// rebalanceToTargetLegacyLoop is a verbatim copy of the pre-D4 50-iteration
// implementation. It exists only so the fuzz test below can prove the closed
// form is bit-for-bit equivalent; do not use it in production code.
func rebalanceToTargetLegacyLoop(slots []assetSlot, txRate float64) int64 {
	const cent = 0.005
	var recorded int64
	for iter := 0; iter < 50; iter++ {
		total := 0.0
		for _, s := range slots {
			total += s.balance
		}
		var tradeVolume float64
		targets := make([]float64, len(slots))
		for i := range slots {
			targets[i] = total * slots[i].targetWeight
			tradeVolume += math.Abs(targets[i] - slots[i].balance)
		}
		cost := int64(math.Round(tradeVolume * txRate))
		if cost == 0 {
			for i := range slots {
				slots[i].balance = targets[i]
			}
			return recorded
		}
		newTotal := total - float64(cost)
		if newTotal < 0 {
			newTotal = 0
		}
		recorded += cost
		for i := range slots {
			slots[i].balance = newTotal * slots[i].targetWeight
		}
		if math.Abs(total-float64(recorded)-sumBalances(slots)) <= cent {
			return recorded
		}
	}
	return recorded
}

func cloneSlots(slots []assetSlot) []assetSlot {
	out := make([]assetSlot, len(slots))
	copy(out, slots)
	return out
}

// Randomized inputs (weights summing to 1, including degenerate cases such as
// zero balances and cost exceeding total) must produce identical cost and
// identical balances between the legacy loop and the closed form.
func TestRebalanceToTargetMatchesLegacyLoop(t *testing.T) {
	rng := rand.New(rand.NewSource(20260705))
	for c := 0; c < 5000; c++ {
		n := 1 + rng.Intn(6)
		slots := make([]assetSlot, n)
		weights := make([]float64, n)
		var wSum float64
		for i := range weights {
			weights[i] = rng.Float64()
			wSum += weights[i]
		}
		for i := range slots {
			slots[i].targetWeight = weights[i] / wSum
			switch rng.Intn(10) {
			case 0:
				slots[i].balance = 0
			case 1:
				slots[i].balance = rng.Float64() // tiny balance so cost can exceed total
			default:
				slots[i].balance = rng.Float64() * 5_000_000
			}
		}
		txRate := 0.0
		switch rng.Intn(5) {
		case 0:
			// keep zero rate
		case 1:
			txRate = 10 + rng.Float64()*50 // absurd rate to force the newTotal clamp
		default:
			txRate = rng.Float64() * 0.05
		}

		legacySlots := cloneSlots(slots)
		newSlots := cloneSlots(slots)
		legacyCost := rebalanceToTargetLegacyLoop(legacySlots, txRate)
		newCost := rebalanceToTarget(newSlots, txRate)

		if legacyCost != newCost {
			t.Fatalf("case %d: cost mismatch legacy=%d new=%d slots=%+v txRate=%v", c, legacyCost, newCost, slots, txRate)
		}
		for i := range slots {
			if legacySlots[i].balance != newSlots[i].balance {
				t.Fatalf("case %d slot %d: balance mismatch legacy=%v new=%v (input=%+v txRate=%v)",
					c, i, legacySlots[i].balance, newSlots[i].balance, slots, txRate)
			}
		}
	}
}

// Acceptance properties of the closed-form rebalance: cost is non-negative, the post-cost
// total equals the pre-cost total minus cost (within half a cent), and a zero
// rate rebalances exactly onto total*weight with zero cost.
func TestRebalanceToTargetProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(96))
	for c := 0; c < 2000; c++ {
		n := 1 + rng.Intn(5)
		slots := make([]assetSlot, n)
		var wSum float64
		weights := make([]float64, n)
		for i := range weights {
			weights[i] = 0.05 + rng.Float64()
			wSum += weights[i]
		}
		for i := range slots {
			slots[i].targetWeight = weights[i] / wSum
			slots[i].balance = rng.Float64() * 3_000_000
		}
		total := sumBalances(slots)
		txRate := rng.Float64() * 0.03

		cost := rebalanceToTarget(slots, txRate)
		if cost < 0 {
			t.Fatalf("case %d: negative cost %d", c, cost)
		}
		if got := sumBalances(slots); math.Abs(got-(total-float64(cost))) > 0.005 {
			t.Fatalf("case %d: sum after rebalance %v != total %v - cost %d", c, got, total, cost)
		}
	}

	slots := []assetSlot{
		{balance: 700, targetWeight: 0.25},
		{balance: 100, targetWeight: 0.35},
		{balance: 200, targetWeight: 0.40},
	}
	cost := rebalanceToTarget(slots, 0)
	if cost != 0 {
		t.Fatalf("zero rate should cost 0, got %d", cost)
	}
	for i, want := range []float64{1000 * 0.25, 1000 * 0.35, 1000 * 0.40} {
		if slots[i].balance != want {
			t.Fatalf("slot %d: want exact %v, got %v", i, want, slots[i].balance)
		}
	}
}

// Golden regression for the 3.3.0 engine semantics. The closed-form rebalance
// and the 3.4.0 version gates must preserve these historical fixed-seed paths
// exactly when an old snapshot is replayed.
func TestRebalanceToTargetGoldenPaths(t *testing.T) {
	type golden struct {
		seed          int
		terminal      int64
		txCost        int64
		succeeded     bool
		failureMonth  int // -1 when nil
		maxDD         float64
		totalSpending int64
		realTerminal  int64
	}
	cases := []golden{
		{seed: 0, terminal: 152448677, txCost: 15013928, succeeded: true, failureMonth: -1, maxDD: 0.7479120211, totalSpending: 438466818, realTerminal: 82229382},
		{seed: 1, terminal: 0, txCost: 9498603, succeeded: false, failureMonth: 284, maxDD: 0.9948108756, totalSpending: 404271567, realTerminal: 0},
		{seed: 7, terminal: 0, txCost: 11243682, succeeded: false, failureMonth: 297, maxDD: 0.9956547928, totalSpending: 433846240, realTerminal: 0},
		{seed: 42, terminal: 391436927, txCost: 15976634, succeeded: true, failureMonth: -1, maxDD: 0.2745939521, totalSpending: 438466818, realTerminal: 211137395},
	}
	for _, g := range cases {
		s, err := RunPath(goldenRebalanceInput(), g.seed, PathRunOpts{})
		if err != nil {
			t.Fatalf("seed %d: %v", g.seed, err)
		}
		fm := -1
		if s.FailureMonth != nil {
			fm = *s.FailureMonth
		}
		if s.TerminalWealthMinor != g.terminal || s.TransactionCostMinor != g.txCost ||
			s.Succeeded != g.succeeded || fm != g.failureMonth ||
			s.TotalSpendingMinor != g.totalSpending || s.RealTerminalWealthMinor != g.realTerminal {
			t.Fatalf("seed %d drifted from legacy golden: got terminal=%d txcost=%d succeeded=%v failureMonth=%d spending=%d realTerminal=%d, want %+v",
				g.seed, s.TerminalWealthMinor, s.TransactionCostMinor, s.Succeeded, fm, s.TotalSpendingMinor, s.RealTerminalWealthMinor, g)
		}
		if math.Abs(s.MaxDrawdown-g.maxDD) > 5e-11 {
			t.Fatalf("seed %d: max drawdown drifted: got %.12f want %.10f", g.seed, s.MaxDrawdown, g.maxDD)
		}
	}
}

func goldenRebalanceInput() *InputSnapshot {
	return &InputSnapshot{
		EngineVersion:          "3.3.0",
		BaseCurrency:           "CNY",
		AggregateCashLiquidity: true,
		Parameters: SnapshotParameters{
			CurrentAge: 50, RetirementAge: 55, EndAge: 75,
			TotalAssetsMinor: 2_000_000_00, AnnualSavingsMinor: 100_000_00,
			AnnualSpendingMinor: 150_000_00, TerminalWealthFloorMinor: 0,
			InflationMode: "fixed_real", FixedInflationRate: 0.025,
			WithdrawalType: "fixed_real", WithdrawalRate: 0.04,
			RebalanceFrequency: "monthly", RebalanceThreshold: 0.01,
			TransactionCostRate: 0.008,
			SimulationRuns:      5, StudentTDf: 7, Seed: "42",
		},
		Assets: []SnapshotAsset{
			{
				HoldingID: "h1", AssetKey: "eq", SnapshotID: "s1",
				Currency: "CNY", AssetClass: domain.AssetClassEquity, IsCash: false,
				InitialMinor: 1_200_000_00, TargetWeight: 0.6,
				ModeledAnnualReturn: 0.07, AnnualVolatility: 0.18, MaxDrawdown: 0.35,
				SourceHash: "eq",
			},
			{
				HoldingID: "h2", AssetKey: "bond", SnapshotID: "s2",
				Currency: "CNY", AssetClass: domain.AssetClassBond, IsCash: false,
				InitialMinor: 600_000_00, TargetWeight: 0.3,
				ModeledAnnualReturn: 0.03, AnnualVolatility: 0.05, MaxDrawdown: 0.10,
				SourceHash: "bond",
			},
			{
				HoldingID: "h3", AssetKey: "cash", SnapshotID: "s3",
				Currency: "CNY", AssetClass: domain.AssetClassCash, IsCash: true,
				InitialMinor: 200_000_00, TargetWeight: 0.1,
				ModeledAnnualReturn: 0.015, AnnualVolatility: 0.0, MaxDrawdown: 0.0,
				SourceHash: "cash",
			},
		},
	}
}
