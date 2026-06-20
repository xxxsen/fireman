package simulation

import (
	"math"
	"testing"
)

// td/061 §5.C.2 / §6.1.9: a single factor with σ>0 must consume the RNG in the
// same order and produce the same draws as the legacy SampleStudentT.
func TestMultivariateSingleFactorMatchesLegacy(t *testing.T) {
	p := ParamsFromAnnual(0.07, 0.15)
	legacy := NewRNG(12345)
	joint := NewRNG(12345)
	l := [][]float64{{p.MonthlySigma}}
	mu := []float64{p.MonthlyMu}
	for i := 0; i < 1000; i++ {
		want, _ := SampleStudentT(legacy, p, 7, LegacyTailTruncation())
		got, _ := SampleMultivariateStudentT(joint, mu, l, 7, LegacyTailTruncation())
		if math.Abs(got[0]-want) > 1e-12 {
			t.Fatalf("draw %d: joint %.12f != legacy %.12f", i, got[0], want)
		}
	}
}

// td/061 §6.1.9: 300k draws should reproduce Σ within statistical tolerance.
func TestMultivariateSampleCovarianceMatchesSigma(t *testing.T) {
	sigma := []float64{0.04, 0.05}
	r := [][]float64{{1, 0.5}, {0.5, 1}}
	cov := covarianceFromCorrelation(r, sigma)
	l, ok := cholesky(cov)
	if !ok {
		t.Fatal("cholesky failed")
	}
	mu := []float64{0, 0}
	rng := NewRNG(99)
	const n = 300000
	var s11, s22, s12 float64
	for i := 0; i < n; i++ {
		out, _ := SampleMultivariateStudentT(rng, mu, l, 7, LegacyTailTruncation())
		// Reconstruct the log returns (no truncation at this small sigma).
		a := math.Log(1 + out[0])
		b := math.Log(1 + out[1])
		s11 += a * a
		s22 += b * b
		s12 += a * b
	}
	s11 /= n
	s22 /= n
	s12 /= n
	rel := func(got, want float64) float64 { return math.Abs(got-want) / want }
	if rel(s11, cov[0][0]) > 0.08 {
		t.Fatalf("var0 = %.6e want %.6e", s11, cov[0][0])
	}
	if rel(s22, cov[1][1]) > 0.08 {
		t.Fatalf("var1 = %.6e want %.6e", s22, cov[1][1])
	}
	if rel(s12, cov[0][1]) > 0.10 {
		t.Fatalf("cov01 = %.6e want %.6e", s12, cov[0][1])
	}
}

// td/061 §6.1.9: ρ=1 equal assets get no diversification; ρ=0 does.
func TestMultivariateDiversificationByCorrelation(t *testing.T) {
	sigma := []float64{0.04, 0.04}
	mu := []float64{0, 0}
	avgVar := func(rho float64) float64 {
		cov := covarianceFromCorrelation([][]float64{{1, rho}, {rho, 1}}, sigma)
		l, _ := cholesky(cov)
		rng := NewRNG(7)
		const n = 200000
		var sum float64
		for i := 0; i < n; i++ {
			out, _ := SampleMultivariateStudentT(rng, mu, l, 7, LegacyTailTruncation())
			avg := (math.Log(1+out[0]) + math.Log(1+out[1])) / 2
			sum += avg * avg
		}
		return sum / n
	}
	single := sigma[0] * sigma[0]
	corr1 := avgVar(1)
	corr0 := avgVar(0)
	if math.Abs(corr1-single)/single > 0.08 {
		t.Fatalf("rho=1 average variance %.6e should match single-asset %.6e", corr1, single)
	}
	if corr0 >= single*0.85 {
		t.Fatalf("rho=0 average variance %.6e should show diversification below %.6e", corr0, single)
	}
}
