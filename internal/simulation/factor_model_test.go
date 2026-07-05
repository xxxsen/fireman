package simulation

import (
	"math"
	"reflect"
	"testing"
)

func TestShrinkCorrelationDocExample(t *testing.T) {
	rho, lambda, priorOnly := ShrinkCorrelation(0.8, 36, true, 0.2, 36)
	if priorOnly {
		t.Fatal("did not expect prior-only with 36 common months")
	}
	if math.Abs(lambda-0.5) > 1e-12 {
		t.Fatalf("lambda = %.6f want 0.5", lambda)
	}
	if math.Abs(rho-0.5) > 1e-12 {
		t.Fatalf("rho_raw = %.6f want 0.5", rho)
	}
}

func TestShrinkCorrelationFallsBackToPrior(t *testing.T) {
	// Fewer than 24 common months must collapse to the prior with a warning.
	rho, lambda, priorOnly := ShrinkCorrelation(0.9, 12, true, 0.2, 36)
	if !priorOnly || rho != 0.2 || lambda != 1 {
		t.Fatalf("short overlap: got rho=%.3f lambda=%.3f priorOnly=%v", rho, lambda, priorOnly)
	}
	// Zero-variance (untrustworthy) historical also collapses to the prior.
	rho2, _, priorOnly2 := ShrinkCorrelation(0, 40, false, 0.3, 36)
	if !priorOnly2 || rho2 != 0.3 {
		t.Fatalf("zero variance: got rho=%.3f priorOnly=%v", rho2, priorOnly2)
	}
}

func TestProjectToPSDRepairsNonPSD(t *testing.T) {
	// Equal off-diagonal -0.6 gives eigenvalues {1-(-0.6)=1.6 (x2), 1+2(-0.6)=-0.2}.
	r := [][]float64{
		{1, -0.6, -0.6},
		{-0.6, 1, -0.6},
		{-0.6, -0.6, 1},
	}
	psd, minEig, maxRepair := projectToPSD(r)
	if minEig > 0 {
		t.Fatalf("expected a negative minimum eigenvalue, got %.6f", minEig)
	}
	if got := minEigenvalueSymmetric(psd); got < -1e-10 {
		t.Fatalf("repaired matrix not PSD, min eig %.3e", got)
	}
	for i := 0; i < 3; i++ {
		if math.Abs(psd[i][i]-1) > 1e-12 {
			t.Fatalf("diagonal not 1 at %d: %.6f", i, psd[i][i])
		}
		for j := 0; j < 3; j++ {
			if math.Abs(psd[i][j]-psd[j][i]) > 1e-12 {
				t.Fatalf("not symmetric at (%d,%d)", i, j)
			}
		}
	}
	if maxRepair <= PSDRepairWarnThreshold {
		t.Fatalf("expected significant repair (>%.2f), got %.4f", PSDRepairWarnThreshold, maxRepair)
	}
	// Deterministic: identical input must yield identical repaired matrix.
	psd2, _, _ := projectToPSD(r)
	if !reflect.DeepEqual(psd, psd2) {
		t.Fatal("PSD projection is not deterministic")
	}
}

func TestAssembleFactorModelPriorOnlyWarning(t *testing.T) {
	// Two factors with no common history: the pairwise correlation collapses
	// to the prior with priorOnly=true, and the assembled model must carry
	// the correlation_prior_only warning.
	a := FactorSpec{Key: "asset:equity:domestic", Mu: 0.005, MonthlySigma: 0.04, Months: map[string]float64{}}
	b := FactorSpec{Key: "asset:bond:domestic", Mu: 0.002, MonthlySigma: 0.02, Months: map[string]float64{}}
	rhoHist, m, histOK := PairwiseCorrelation(a, b)
	rho, _, priorOnly := ShrinkCorrelation(rhoHist, m, histOK, 0.15, 36)
	if !priorOnly {
		t.Fatal("expected prior-only correlation with no common months")
	}
	rRaw := [][]float64{{1, rho}, {rho, 1}}
	model, ok := AssembleFactorModel(
		[]string{a.Key, b.Key},
		[]float64{a.Mu, b.Mu},
		[]float64{a.MonthlySigma, b.MonthlySigma},
		rRaw,
		[]string{PairKey(a.Key, b.Key)},
	)
	if !ok {
		t.Fatal("expected a valid factor model")
	}
	if math.Abs(model.Audit.RPSD[0][1]-0.15) > 1e-9 {
		t.Fatalf("expected prior-only correlation 0.15, got %.6f", model.Audit.RPSD[0][1])
	}
	foundPriorOnly := false
	for _, w := range model.Audit.Warnings {
		if w == "correlation_prior_only" {
			foundPriorOnly = true
		}
	}
	if !foundPriorOnly {
		t.Fatalf("expected correlation_prior_only warning, got %v", model.Audit.Warnings)
	}
	if _, ok := cholesky(model.Sigma); !ok {
		t.Fatal("covariance must be Cholesky-decomposable")
	}
}
