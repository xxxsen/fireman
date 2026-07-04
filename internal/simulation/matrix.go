package simulation

import "math"

// eigenEpsilon bounds the smallest eigenvalue used in the PSD projection so the
// repaired matrix is strictly positive definite and Cholesky-decomposable.
const eigenEpsilon = 1e-8

func cloneMatrix(m [][]float64) [][]float64 {
	out := make([][]float64, len(m))
	for i := range m {
		out[i] = append([]float64(nil), m[i]...)
	}
	return out
}

func identityMatrix(n int) [][]float64 {
	out := make([][]float64, n)
	for i := range out {
		out[i] = make([]float64, n)
		out[i][i] = 1
	}
	return out
}

// jacobiEigenSymmetric returns eigenvalues and eigenvectors (columns) of a
// symmetric matrix using the cyclic Jacobi method. It is deterministic, so the
// same input always produces the same decomposition byte-for-byte.
func jacobiEigenSymmetric(input [][]float64) ([]float64, [][]float64) {
	n := len(input)
	a := cloneMatrix(input)
	v := identityMatrix(n)
	for sweep := 0; sweep < 100; sweep++ {
		if offDiagonalNorm(a) < 1e-18 {
			break
		}
		for p := 0; p < n-1; p++ {
			for q := p + 1; q < n; q++ {
				jacobiRotate(a, v, p, q)
			}
		}
	}
	values := make([]float64, n)
	for i := 0; i < n; i++ {
		values[i] = a[i][i]
	}
	return values, v
}

func offDiagonalNorm(a [][]float64) float64 {
	n := len(a)
	sum := 0.0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			sum += a[i][j] * a[i][j]
		}
	}
	return sum
}

func jacobiRotate(a, v [][]float64, p, q int) {
	if math.Abs(a[p][q]) < 1e-300 {
		return
	}
	n := len(a)
	theta := (a[q][q] - a[p][p]) / (2 * a[p][q])
	t := math.Copysign(1, theta) / (math.Abs(theta) + math.Sqrt(theta*theta+1))
	if theta == 0 {
		t = 1
	}
	c := 1 / math.Sqrt(t*t+1)
	s := t * c
	for i := 0; i < n; i++ {
		aip := a[i][p]
		aiq := a[i][q]
		a[i][p] = c*aip - s*aiq
		a[i][q] = s*aip + c*aiq
	}
	for i := 0; i < n; i++ {
		api := a[p][i]
		aqi := a[q][i]
		a[p][i] = c*api - s*aqi
		a[q][i] = s*api + c*aqi
	}
	for i := 0; i < n; i++ {
		vip := v[i][p]
		viq := v[i][q]
		v[i][p] = c*vip - s*viq
		v[i][q] = s*vip + c*viq
	}
}

// projectToPSD repairs a symmetric correlation matrix to be positive definite by
// flooring eigenvalues at eigenEpsilon and renormalising the diagonal back to 1.
// It returns the repaired matrix, the minimum eigenvalue of the
// input and the largest absolute element change.
func projectToPSD(r [][]float64) ([][]float64, float64, float64) {
	values, vectors := jacobiEigenSymmetric(r)
	minEig := math.Inf(1)
	for _, ev := range values {
		if ev < minEig {
			minEig = ev
		}
	}
	eig := reconstructFlooredEigen(values, vectors)
	psd, maxRepair := normalizeToCorrelation(eig, r)
	symmetrize(psd)
	return psd, minEig, maxRepair
}

// reconstructFlooredEigen rebuilds R_eig = Q diag(max(ev, eps)) Qᵀ.
func reconstructFlooredEigen(values []float64, vectors [][]float64) [][]float64 {
	n := len(values)
	eig := make([][]float64, n)
	for i := 0; i < n; i++ {
		eig[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			sum := 0.0
			for k := 0; k < n; k++ {
				lambda := values[k]
				if lambda < eigenEpsilon {
					lambda = eigenEpsilon
				}
				sum += vectors[i][k] * lambda * vectors[j][k]
			}
			eig[i][j] = sum
		}
	}
	return eig
}

// normalizeToCorrelation rescales R_eig back to unit diagonal and reports the
// largest absolute element change versus the original matrix.
func normalizeToCorrelation(eig, original [][]float64) ([][]float64, float64) {
	n := len(eig)
	psd := make([][]float64, n)
	maxRepair := 0.0
	for i := 0; i < n; i++ {
		psd[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			val := 1.0
			if i != j {
				denom := math.Sqrt(eig[i][i] * eig[j][j])
				if denom > 0 {
					val = eig[i][j] / denom
				} else {
					val = 0
				}
			}
			psd[i][j] = val
			if d := math.Abs(val - original[i][j]); d > maxRepair {
				maxRepair = d
			}
		}
	}
	return psd, maxRepair
}

func symmetrize(m [][]float64) {
	n := len(m)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			avg := (m[i][j] + m[j][i]) / 2
			m[i][j] = avg
			m[j][i] = avg
		}
		m[i][i] = 1
	}
}

// cholesky returns the lower-triangular L with L Lᵀ = a. It tolerates positive
// semi-definite inputs (zero pivots, e.g. a ρ=1 block) by zeroing the dependent
// column, so a perfectly correlated factor pair shares one shock instead of
// failing. A clearly negative pivot means the matrix is not PSD and ok=false.
func cholesky(a [][]float64) ([][]float64, bool) {
	const pivotTol = 1e-12
	n := len(a)
	l := make([][]float64, n)
	for i := range l {
		l[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			sum := a[i][j]
			for k := 0; k < j; k++ {
				sum -= l[i][k] * l[j][k]
			}
			if i == j {
				switch {
				case sum < -pivotTol:
					return nil, false
				case sum <= 0:
					l[i][j] = 0
				default:
					l[i][j] = math.Sqrt(sum)
				}
				continue
			}
			if l[j][j] == 0 {
				l[i][j] = 0
			} else {
				l[i][j] = sum / l[j][j]
			}
		}
	}
	return l, true
}

// minEigenvalueSymmetric is a helper for tests/audit.
func minEigenvalueSymmetric(m [][]float64) float64 {
	values, _ := jacobiEigenSymmetric(m)
	minEig := math.Inf(1)
	for _, ev := range values {
		if ev < minEig {
			minEig = ev
		}
	}
	return minEig
}
