package simulation

import (
	"math"
	"testing"
)

// guardrailPlanner returns a planner retired at wealth 1,000,000 with annual
// spending 40,000, i.e. InitialRate = 4%. Guardrails trigger outside
// [0.8, 1.2] x 4% = [3.2%, 4.8%]; floor/ceiling are 0.7 / 1.3 x inflBase.
func guardrailPlanner() WithdrawalPlanner {
	w := NewWithdrawalPlanner("guardrail", 40000, 0, 0.7, 1.3)
	w.InitAtRetirement(1_000_000)
	return w
}

func realSpending(w *WithdrawalPlanner, infl float64) float64 {
	return w.ProposedAnnual / infl
}

func TestGuardrailCutsCompoundAcrossYearsUntilFloor(t *testing.T) {
	w := guardrailPlanner()

	// First retirement month: spending starts at the inflation baseline.
	got := w.MonthlySpending(0, 0, 1_000_000, 1.0, false)
	if got != int64(math.Round(40000.0/12)) {
		t.Fatalf("first month spending = %d, want %d", got, int64(math.Round(40000.0/12)))
	}

	// Five consecutive anniversaries with depressed wealth so that
	// currentRate > 1.2*InitialRate: real spending must compound x0.9 per
	// year (40000 -> 36000 -> 32400 -> 29160) and then stop at the floor
	// 0.7 x 40000 = 28000.
	want := []float64{36000, 32400, 29160, 28000, 28000}
	infl := 1.0
	for year, wantReal := range want {
		infl *= 1.02
		w.MonthlySpending(12*(year+1), 0, 400_000, infl, true)
		if gotReal := realSpending(&w, infl); math.Abs(gotReal-wantReal) > 1e-6 {
			t.Fatalf("year %d real spending = %.4f, want %.4f", year+1, gotReal, wantReal)
		}
	}
}

func TestGuardrailRaisesCompoundAcrossYearsUntilCeiling(t *testing.T) {
	w := guardrailPlanner()
	w.MonthlySpending(0, 0, 1_000_000, 1.0, false)

	// Five consecutive anniversaries with inflated wealth so that
	// currentRate < 0.8*InitialRate: real spending compounds x1.1 per year
	// (40000 -> 44000 -> 48400) and then stops at the ceiling
	// 1.3 x 40000 = 52000.
	want := []float64{44000, 48400, 52000, 52000, 52000}
	infl := 1.0
	for year, wantReal := range want {
		infl *= 1.02
		w.MonthlySpending(12*(year+1), 0, 10_000_000, infl, true)
		if gotReal := realSpending(&w, infl); math.Abs(gotReal-wantReal) > 1e-6 {
			t.Fatalf("year %d real spending = %.4f, want %.4f", year+1, gotReal, wantReal)
		}
	}
}

func TestGuardrailWithoutTriggersMatchesInflationAdjustment(t *testing.T) {
	w := guardrailPlanner()
	fixed := NewWithdrawalPlanner("fixed_real", 40000, 0, 0, 0)
	fixed.InitAtRetirement(1_000_000)

	w.MonthlySpending(0, 0, 1_000_000, 1.0, false)

	// Wealth tracks inflation exactly, so currentRate stays at InitialRate
	// and no guardrail fires: spending must equal the pure
	// inflation-adjusted baseline (identical to fixed_real).
	infl := 1.0
	for year := 1; year <= 5; year++ {
		infl *= 1.03
		wealth := int64(math.Round(1_000_000 * infl))
		got := w.MonthlySpending(12*year, 0, wealth, infl, true)
		wantMonthly := fixed.MonthlySpending(12*year, 0, wealth, infl, true)
		if got != wantMonthly {
			t.Fatalf("year %d monthly spending = %d, want fixed_real %d", year, got, wantMonthly)
		}
		if gotReal := realSpending(&w, infl); math.Abs(gotReal-40000) > 1e-6 {
			t.Fatalf("year %d real spending = %.4f, want 40000", year, gotReal)
		}
	}
}

func TestGuardrailZeroYearStartWealthSkipsRateCheckButStillClamps(t *testing.T) {
	w := guardrailPlanner()
	w.MonthlySpending(0, 0, 1_000_000, 1.0, false)

	// Anniversary with zero month-start wealth: the rate check is skipped and
	// the proposal is only the inflation-adjusted previous year clamped to
	// floor/ceiling, i.e. unchanged in real terms.
	w.MonthlySpending(12, 0, 0, 1.05, true)
	if gotReal := realSpending(&w, 1.05); math.Abs(gotReal-40000) > 1e-6 {
		t.Fatalf("real spending = %.4f, want 40000", gotReal)
	}
}
