package simulation

import (
	"math"
	"testing"
)

// 12 months of unperturbed compounding of the monthly log drift
// must reproduce the forward annual geometric return exactly.
func TestParamsFromAnnualCompoundsToGeometricReturn(t *testing.T) {
	for _, r := range []float64{0, 0.03, 0.065, 0.169564, -0.05} {
		p := ParamsFromAnnual(r, 0.0)
		got := math.Exp(p.MonthlyMu*12) - 1
		if math.Abs(got-r) > 1e-12 {
			t.Fatalf("annual=%g: 12-month compound=%g, want %g", r, got, r)
		}
		// With zero volatility, SampleStudentT must return the deterministic drift.
		simple, truncated := SampleStudentT(nil, p, 7, LegacyTailTruncation())
		if truncated {
			t.Fatalf("annual=%g: unexpected truncation with zero sigma", r)
		}
		if math.Abs(simple-(math.Exp(p.MonthlyMu)-1)) > 1e-12 {
			t.Fatalf("annual=%g: zero-sigma sample mismatch", r)
		}
	}
}
