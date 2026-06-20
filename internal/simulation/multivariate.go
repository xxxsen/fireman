package simulation

import "math"

// SampleMultivariateStudentT draws one month of jointly-distributed simple
// returns for every factor, sharing a single fat-tail scale across all factors
// so extreme months happen together rather than independently (td/061 §3.5.3):
//
//	z ~ N(0, I);  q ~ χ²(ν);  s = sqrt((ν-2)/q)
//	y = L (z * s);  log_return_i = μ_i + y_i;  simple_i = exp(log_return_i) - 1
//
// L is the lower-triangular Cholesky factor of the monthly covariance Σ_m (so it
// already includes per-factor volatility). For a single factor with σ>0 this
// consumes the RNG in the exact same order as SampleStudentT (one normal for z,
// then ν normals for the chi-square), so single-asset runs stay bit-compatible.
//
// The frozen per-factor simple-return truncation band is applied independently;
// the number of truncated factors is returned (td/063 R3).
func SampleMultivariateStudentT(
	rng *RNG, mu []float64, l [][]float64, df int, trunc TailTruncation,
) ([]float64, int) {
	n := len(mu)
	z := make([]float64, n)
	for i := 0; i < n; i++ {
		z[i] = rng.NormFloat64()
	}
	q := chiSquare(rng, df)
	s := math.Sqrt(float64(df-2) / q)

	out := make([]float64, n)
	truncations := 0
	for i := 0; i < n; i++ {
		y := 0.0
		for j := 0; j <= i; j++ {
			y += l[i][j] * z[j]
		}
		simple, clamped := trunc.clamp(math.Exp(mu[i]+s*y) - 1)
		if clamped {
			truncations++
		}
		out[i] = simple
	}
	return out, truncations
}
