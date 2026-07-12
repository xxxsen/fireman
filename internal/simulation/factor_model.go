package simulation

import (
	"math"
	"sort"
)

// MinCommonMonths is the minimum number of common observations required before a
// pairwise historical correlation is trusted; below it the prior is used and a
// correlation_prior_only warning is recorded.
const MinCommonMonths = 24

// PSDRepairWarnThreshold is the maximum off-diagonal repair magnitude before a
// high-priority model warning is required.
const PSDRepairWarnThreshold = 0.05

// FactorSpec is one risk factor (asset or FX) entering the joint model. Months
// maps a YYYY-MM key to that month's log return; only complete-year continuous
// months should be present.
type FactorSpec struct {
	Key          string
	Mu           float64 // monthly log drift (forward)
	MonthlySigma float64 // scenario-adjusted monthly log volatility
	Months       map[string]float64
}

// FactorAudit records how the joint correlation/covariance was built so a run can
// explain whether it mainly relies on priors and how large the PSD repair was.
type FactorAudit struct {
	Factors        []string           `json:"factors"`
	PairMonths     map[string]int     `json:"pair_months"`
	Lambda         map[string]float64 `json:"lambda"`
	RRaw           [][]float64        `json:"r_raw"`
	RPSD           [][]float64        `json:"r_psd"`
	MinEigenvalue  float64            `json:"min_eigenvalue"`
	MaxRepairDelta float64            `json:"max_repair_delta"`
	PriorOnlyPairs []string           `json:"prior_only_pairs,omitempty"`
	Warnings       []string           `json:"warnings,omitempty"`
}

// FactorModel is the frozen joint risk model for a run.
type FactorModel struct {
	Factors      []string    `json:"factors"`
	Mu           []float64   `json:"mu"`
	MonthlySigma []float64   `json:"monthly_sigma"`
	Sigma        [][]float64 `json:"sigma"`
	L            [][]float64 `json:"l"`
	Audit        FactorAudit `json:"audit"`
}

func pairKey(a, b string) string {
	if a <= b {
		return a + "|" + b
	}
	return b + "|" + a
}

// PairwiseCorrelation computes the Pearson correlation of two factors over their
// common months. It returns (rho, commonMonths, ok); ok is false when there is
// no overlap or either series has zero variance on the overlap.
func PairwiseCorrelation(a, b FactorSpec) (float64, int, bool) {
	keys := make([]string, 0, len(a.Months))
	for k := range a.Months {
		if _, ok := b.Months[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	m := len(keys)
	if m == 0 {
		return 0, 0, false
	}
	var sumA, sumB float64
	for _, k := range keys {
		sumA += a.Months[k]
		sumB += b.Months[k]
	}
	meanA := sumA / float64(m)
	meanB := sumB / float64(m)
	var cov, varA, varB float64
	for _, k := range keys {
		da := a.Months[k] - meanA
		db := b.Months[k] - meanB
		cov += da * db
		varA += da * da
		varB += db * db
	}
	if varA <= 0 || varB <= 0 {
		return 0, m, false
	}
	return cov / math.Sqrt(varA*varB), m, true
}

// CorrelationPSDResult reports whether a candidate correlation matrix is PSD and
// how large the deterministic repair would be.
type CorrelationPSDResult struct {
	MinEigenvalue  float64
	MaxRepairDelta float64
	Repaired       bool
}

// CheckCorrelationPSD validates a symmetric correlation matrix and returns the
// minimum eigenvalue and the largest off-diagonal element the PSD projection
// would move. Callers reject a profile when MaxRepairDelta exceeds the warn
// threshold so a published profile never silently relies on a heavy repair.
func CheckCorrelationPSD(rRaw [][]float64) CorrelationPSDResult {
	_, minEig, maxRepair := projectToPSD(rRaw)
	return CorrelationPSDResult{
		MinEigenvalue:  minEig,
		MaxRepairDelta: maxRepair,
		Repaired:       maxRepair > 1e-12,
	}
}

// AssembleFactorModel builds the frozen joint model from a caller-supplied raw
// correlation matrix without per-pair audit detail (a simplified signature kept
// for tests and callers that have no pair-month/λ bookkeeping). It is a thin
// delegate to AssembleFactorModelDetailed.
func AssembleFactorModel(
	keys []string, mu, sigma []float64, rRaw [][]float64, priorOnlyPairs []string,
) (FactorModel, bool) {
	return AssembleFactorModelDetailed(keys, mu, sigma, rRaw, nil, nil, priorOnlyPairs)
}

// AssembleFactorModelDetailed builds the frozen model from a caller-computed raw
// correlation matrix plus the per-pair audit the caller derived from the frozen
// monthly history (pair months, shrinkage λ and the prior-only pair list). It is
// used when same-type asset pairs are forced to ρ=1 and cross-type pairs carry
// shrunk historical correlations, which the generic buildRawCorrelation path
// cannot express.
func AssembleFactorModelDetailed(
	keys []string, mu, sigma []float64, rRaw [][]float64,
	pairMonths map[string]int, lambda map[string]float64, priorOnlyPairs []string,
) (FactorModel, bool) {
	if pairMonths == nil {
		pairMonths = map[string]int{}
	}
	if lambda == nil {
		lambda = map[string]float64{}
	}
	audit := FactorAudit{
		Factors:        keys,
		PairMonths:     pairMonths,
		Lambda:         lambda,
		PriorOnlyPairs: priorOnlyPairs,
		RRaw:           cloneMatrix(rRaw),
	}
	if len(priorOnlyPairs) > 0 {
		audit.Warnings = append(audit.Warnings, "correlation_prior_only")
	}
	psd, minEig, maxRepair := projectToPSD(rRaw)
	audit.RPSD = psd
	audit.MinEigenvalue = minEig
	audit.MaxRepairDelta = maxRepair
	if maxRepair > PSDRepairWarnThreshold {
		audit.Warnings = append(audit.Warnings, "correlation_psd_repair_significant")
	}
	cov := covarianceFromCorrelation(psd, sigma)
	l, ok := cholesky(cov)
	if !ok {
		return FactorModel{}, false
	}
	return FactorModel{
		Factors: keys, Mu: mu, MonthlySigma: sigma, Sigma: cov, L: l, Audit: audit,
	}, true
}

// RebuildFactorModelWithFrozenCorrelation replaces drift and volatility while
// preserving the exact factor order and PSD correlation audit of a frozen run.
// Scenario comparison uses it so no mutable market history is reread.
func RebuildFactorModelWithFrozenCorrelation(
	base FactorModel, mu, sigma []float64,
) (FactorModel, bool) {
	if len(mu) != len(base.Factors) || len(sigma) != len(base.Factors) ||
		len(base.Audit.RPSD) != len(base.Factors) {
		return FactorModel{}, false
	}
	cov := covarianceFromCorrelation(base.Audit.RPSD, sigma)
	l, ok := cholesky(cov)
	if !ok {
		return FactorModel{}, false
	}
	return FactorModel{
		Factors: append([]string(nil), base.Factors...),
		Mu:      append([]float64(nil), mu...), MonthlySigma: append([]float64(nil), sigma...),
		Sigma: cov, L: l, Audit: base.Audit,
	}, true
}

// PairKey returns the canonical sorted "a|b" identifier for a factor pair so the
// service and engine record audit entries under the same key.
func PairKey(a, b string) string { return pairKey(a, b) }

func covarianceFromCorrelation(r [][]float64, sigma []float64) [][]float64 {
	n := len(r)
	cov := make([][]float64, n)
	for i := 0; i < n; i++ {
		cov[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			cov[i][j] = r[i][j] * sigma[i] * sigma[j]
		}
	}
	return cov
}

// ShrinkCorrelation blends the historical correlation toward the prior using the
// strength-month weight λ = strength / (m + strength). When m < MinCommonMonths
// or the historical estimate is untrustworthy, it falls back to the prior and
// reports priorOnly = true.
func ShrinkCorrelation(
	rhoHist float64, m int, histOK bool, rhoPrior float64, strengthMonths int,
) (float64, float64, bool) {
	if !histOK || m < MinCommonMonths {
		return rhoPrior, 1, true
	}
	lambda := float64(strengthMonths) / float64(m+strengthMonths)
	rho := (1-lambda)*rhoHist + lambda*rhoPrior
	if rho > 1 {
		rho = 1
	}
	if rho < -1 {
		rho = -1
	}
	return rho, lambda, false
}
