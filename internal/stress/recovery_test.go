package stress

import "testing"

func TestRecoveryTargetUsesPreShockWealth(t *testing.T) {
	series := []int64{1000, 950, 800, 1050}
	target := series[0]
	rec := recoveryMonth(series, 3, target)
	if rec != 0 {
		t.Fatalf("expected immediate recovery at month 3, got %d", rec)
	}
}

func TestRecoveryTargetAtShockStartZero(t *testing.T) {
	initial := int64(2_000_000_00)
	series := []int64{1_800_000_00, 1_900_000_00}
	rec := recoveryMonth(series, 1, initial)
	if rec >= 0 {
		t.Fatalf("expected no recovery within short series, got %d", rec)
	}
}

func TestRecoveryMonthZeroWhenImmediate(t *testing.T) {
	series := []int64{100, 90, 100}
	if recoveryMonth(series, 2, 100) != 0 {
		t.Fatal("expected 0 month recovery")
	}
}

func TestRecoveryNotWithinPlan(t *testing.T) {
	series := []int64{100, 50, 55, 60}
	if recoveryMonth(series, 2, 100) >= 0 {
		t.Fatal("expected no recovery")
	}
}
