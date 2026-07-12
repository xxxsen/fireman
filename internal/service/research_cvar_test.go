package service

import (
	"math"
	"slices"
	"testing"
)

func TestComputeEmpiricalCVaRFractionalTailGolden(t *testing.T) {
	losses := make([]float64, 25)
	losses[0], losses[1], losses[2] = 0.20, 0.10, 0.05
	for i := 3; i < len(losses); i++ {
		losses[i] = -0.01
	}
	original := slices.Clone(losses)
	got := computeTailLossStats(losses, TailRiskSpec{Confidence: 0.90, HorizonDays: 1})
	if got.TailCount != 3 || math.Abs(got.VaRLoss-0.05) > 1e-12 ||
		math.Abs(got.CVaRLoss-0.13) > 1e-12 || math.Abs(got.WorstLoss-0.20) > 1e-12 {
		t.Fatalf("unexpected golden result: %+v", got)
	}
	if !slices.Equal(losses, original) {
		t.Fatal("input losses were modified")
	}
}

func TestHoldingPeriodLossesCompoundsTwentyDays(t *testing.T) {
	returns := make([]float64, 20)
	for i := range returns {
		returns[i] = -0.01
	}
	losses, err := holdingPeriodLosses(returns, 20)
	if err != nil {
		t.Fatal(err)
	}
	want := 0.18209306240276923
	if len(losses) != 1 || math.Abs(losses[0]-want) > 1e-15 {
		t.Fatalf("got %v want %v", losses, want)
	}
}

func TestHoldingPeriodLossesOneDayMatchesReturns(t *testing.T) {
	returns := []float64{-0.03, 0, 0.02}
	losses, err := holdingPeriodLosses(returns, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0.03, 0, -0.02}
	if !slices.Equal(losses, want) {
		t.Fatalf("one-day losses = %v, want %v", losses, want)
	}
	if _, err := ComputeEmpiricalCVaR(make([]float64, 20), TailRiskSpec{0.95, 20}); err == nil {
		t.Fatal("single holding-period scenario must be rejected by the sample gate")
	}
}

func TestTailLossStatsPreservesNegativeTailAndIgnoresInputOrder(t *testing.T) {
	losses := []float64{-0.01, -0.02, -0.03, -0.04, -0.05, -0.06, -0.07, -0.08, -0.09, -0.10}
	forward := computeTailLossStats(losses, TailRiskSpec{Confidence: 0.90, HorizonDays: 1})
	slices.Reverse(losses)
	reversed := computeTailLossStats(losses, TailRiskSpec{Confidence: 0.90, HorizonDays: 1})
	if forward != reversed || forward.VaRLoss != -0.01 || forward.CVaRLoss != -0.01 {
		t.Fatalf("negative or reordered tail changed result: forward=%+v reversed=%+v", forward, reversed)
	}
}

func TestComputeEmpiricalCVaRBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		confidence float64
		minimum    int
	}{
		{"90", 0.90, 50}, {"95", 0.95, 100}, {"99", 0.99, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			returns := make([]float64, tc.minimum+1)
			if _, err := ComputeEmpiricalCVaR(returns[:tc.minimum-1], TailRiskSpec{tc.confidence, 1}); err == nil {
				t.Fatal("expected insufficient sample")
			}
			got, err := ComputeEmpiricalCVaR(returns[:tc.minimum], TailRiskSpec{tc.confidence, 1})
			if err != nil {
				t.Fatal(err)
			}
			if got.VaRLoss != 0 || got.CVaRLoss != 0 || got.WorstLoss != 0 {
				t.Fatalf("flat returns must have zero losses: %+v", got)
			}
			if _, err := ComputeEmpiricalCVaR(returns, TailRiskSpec{tc.confidence, 1}); err != nil {
				t.Fatalf("boundary+1 rejected: %v", err)
			}
		})
	}
}

func TestComputeEmpiricalCVaRRejectsInvalidInputs(t *testing.T) {
	valid := make([]float64, 100)
	for _, spec := range []TailRiskSpec{{0.94, 1}, {0.95, 2}} {
		if _, err := ComputeEmpiricalCVaR(valid, spec); err == nil {
			t.Fatalf("expected invalid spec %+v", spec)
		}
	}
	invalid := slices.Clone(valid)
	invalid[0] = math.NaN()
	if _, err := ComputeEmpiricalCVaR(invalid, TailRiskSpec{0.95, 1}); err == nil {
		t.Fatal("expected invalid return")
	}
	invalid[0] = math.Inf(1)
	if _, err := ComputeEmpiricalCVaR(invalid, TailRiskSpec{0.95, 1}); err == nil {
		t.Fatal("expected infinite return rejection")
	}
	invalid[0] = -1
	if _, err := ComputeEmpiricalCVaR(invalid, TailRiskSpec{0.95, 1}); err == nil {
		t.Fatal("expected -100% return rejection")
	}
}

func TestComputeEmpiricalCVaRIsDeterministicAndNotBelowVaR(t *testing.T) {
	returns := make([]float64, 520)
	for i := range returns {
		returns[i] = 0.001 + 0.02*math.Sin(float64(i)/11)
	}
	original := slices.Clone(returns)
	first, err := ComputeEmpiricalCVaR(returns, TailRiskSpec{Confidence: 0.99, HorizonDays: 20})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ComputeEmpiricalCVaR(returns, TailRiskSpec{Confidence: 0.99, HorizonDays: 20})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.CVaRLoss < first.VaRLoss || !slices.Equal(returns, original) {
		t.Fatalf("non-deterministic or invalid tail ordering: first=%+v second=%+v", first, second)
	}
}
