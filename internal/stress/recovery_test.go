package stress

import (
	"math"
	"testing"
)

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

func TestRecoveryP50BoundaryCases(t *testing.T) {
	cases := []struct {
		name       string
		recovered  int
		total      int
		wantWithin bool
	}{
		{name: "49 of 100", recovered: 49, total: 100, wantWithin: false},
		{name: "50 of 100", recovered: 50, total: 100, wantWithin: true},
		{name: "60 of 100", recovered: 60, total: 100, wantWithin: true},
		{name: "0 of 100", recovered: 0, total: 100, wantWithin: false},
		{name: "100 of 100", recovered: 100, total: 100, wantWithin: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			months := make([]float64, tc.total)
			for i := range months {
				months[i] = math.MaxFloat64
			}
			for i := 0; i < tc.recovered; i++ {
				months[i] = float64(i + 1)
			}
			rec, within := recoveryP50(months)
			if within != tc.wantWithin {
				t.Fatalf("within=%v want=%v rec=%v", within, tc.wantWithin, rec)
			}
			if !within && rec != nil {
				t.Fatalf("expected nil P50 when not within plan, got %v", *rec)
			}
			if within && rec == nil {
				t.Fatal("expected P50 when within plan")
			}
		})
	}
}

func TestRecoveryP50UsesAllPathsRank(t *testing.T) {
	months := make([]float64, 100)
	for i := range months {
		if i < 60 {
			months[i] = float64((i + 1) * 2)
		} else {
			months[i] = math.MaxFloat64
		}
	}
	rec, within := recoveryP50(months)
	if !within || rec == nil {
		t.Fatal("expected within-plan P50")
	}
	if *rec != 100 {
		t.Fatalf("expected rank-50 recovery 100 months, got %d", *rec)
	}
}

func TestRecoveryNotWithinPlan(t *testing.T) {
	series := []int64{100, 50, 55, 60}
	if recoveryMonth(series, 2, 100) >= 0 {
		t.Fatal("expected no recovery")
	}
}
