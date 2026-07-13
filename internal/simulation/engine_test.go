package simulation

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestRunCancellationDoesNotAggregatePartialPaths(t *testing.T) {
	in := testInputSnapshot()
	checks := 0
	result := Run(in, RunOptions{Runs: 20, CancelCheck: func() bool {
		checks++
		return checks > 5
	}})
	if !result.Canceled {
		t.Fatal("canceled run was not marked canceled")
	}
	if len(result.Paths) != 0 || result.SuccessCount != 0 || result.FailureCount != 0 ||
		len(result.QuantileSeries) != 0 || result.Summary.TerminalQuantiles != nil {
		t.Fatalf("canceled run returned partial aggregate: %#v", result)
	}
}

func TestSplitMix64Deterministic(t *testing.T) {
	a := SplitMix64(42)
	b := SplitMix64(42)
	if a != b {
		t.Fatalf("expected deterministic seed, got %d vs %d", a, b)
	}
	if SplitMix64(0) == SplitMix64(1) {
		t.Fatal("different inputs should differ")
	}
}

func TestSameSeedBitwiseReproducible(t *testing.T) {
	in := testInputSnapshot()
	in.Parameters.SimulationRuns = 50
	r1 := Run(in, RunOptions{Runs: 50})
	r2 := Run(in, RunOptions{Runs: 50})
	if !reflect.DeepEqual(r1, r2) {
		t.Fatal("same 3.2 snapshot and seed must reproduce the complete result")
	}
	if r1.SuccessCount != r2.SuccessCount {
		t.Fatalf("success count mismatch: %d vs %d", r1.SuccessCount, r2.SuccessCount)
	}
	for i := range r1.Paths {
		if r1.Paths[i].TerminalWealthMinor != r2.Paths[i].TerminalWealthMinor {
			t.Fatalf("path %d terminal wealth differs", i)
		}
		if r1.Paths[i].PathSeed != r2.Paths[i].PathSeed {
			t.Fatalf("path seed differs")
		}
	}
}

func TestStudentTMeanVariance(t *testing.T) {
	p := ParamsFromAnnual(0.08, 0.15)
	rng := NewRNG(12345)
	df := 7
	const n = 20000
	sum := 0.0
	sum2 := 0.0
	for i := 0; i < n; i++ {
		r, _ := SampleStudentT(rng, p, df, LegacyTailTruncation())
		sum += r
		sum2 += r * r
	}
	mean := sum / float64(n)
	variance := sum2/float64(n) - mean*mean
	expected := math.Exp(p.MonthlyMu) - 1
	if math.Abs(mean-expected) > 0.01 {
		t.Fatalf("monthly mean %f far from expected %f", mean, expected)
	}
	if variance <= 0 {
		t.Fatalf("expected positive variance, got %f", variance)
	}
}

func TestIndependentAssetFactors(t *testing.T) {
	p := ParamsFromAnnual(0.08, 0.15)
	const n = 5000
	x := make([]float64, n)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		rng1 := NewRNG(int64(i + 1))
		rng2 := NewRNG(int64(i + 100000))
		x[i], _ = SampleStudentT(rng1, p, 7, LegacyTailTruncation())
		y[i], _ = SampleStudentT(rng2, p, 7, LegacyTailTruncation())
	}
	corr := pearson(x, y)
	if math.Abs(corr) > 0.08 {
		t.Fatalf("expected near-zero correlation, got %f", corr)
	}
}

func TestSettlementOrderSavingsBeforeReturns(t *testing.T) {
	in := testInputSnapshot()
	in.Parameters.AnnualSavingsMinor = 120_000_00
	in.Parameters.AnnualSpendingMinor = 0
	in.Parameters.RetirementAge = in.Parameters.EndAge
	summary, _ := RunPath(in, 0, PathRunOpts{})
	if !summary.Succeeded {
		t.Fatal("expected success with only savings")
	}
	if summary.TerminalWealthMinor <= in.Assets[0].InitialMinor {
		t.Fatalf("wealth should grow from savings before returns, got %d", summary.TerminalWealthMinor)
	}
}

func TestQuantileAndWilson(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5}
	if got := Quantile(vals, 0); got != 1 {
		t.Fatalf("p00 min expected 1, got %f", got)
	}
	if got := Quantile(vals, 0.5); math.Abs(got-3) > 1e-9 {
		t.Fatalf("p50 expected 3, got %f", got)
	}
	low, high := WilsonInterval(800, 1000, 1.96)
	if low <= 0 || high >= 1 || low >= high {
		t.Fatalf("unexpected wilson interval: %f %f", low, high)
	}
}

func TestYearRecordIncludesAssetWeights(t *testing.T) {
	in := testInputSnapshot()
	detail := RegeneratePathDetail(in, 0)
	if len(detail.Yearly) == 0 {
		t.Fatal("expected yearly records")
	}
	weights := detail.Yearly[0].AssetWeights
	if len(weights) == 0 {
		t.Fatal("expected year-end asset weights")
	}
	if w, ok := weights["h1"]; !ok || w <= 0 {
		t.Fatalf("expected holding weight, got %+v", weights)
	}
}

func TestPathRegenerateMatchesSummary(t *testing.T) {
	in := testInputSnapshot()
	in.Parameters.SimulationRuns = 10
	result := Run(in, RunOptions{Runs: 10})
	for _, p := range result.Paths {
		detail := RegeneratePathDetail(in, p.PathNo)
		if detail.PathSeed != formatSeed(p.PathSeed) {
			t.Fatalf("path seed mismatch")
		}
		if detail.Succeeded != p.Succeeded {
			t.Fatalf("success flag mismatch for path %d", p.PathNo)
		}
		if len(detail.Monthly) == 0 {
			t.Fatalf("expected monthly detail")
		}
		last := detail.Monthly[len(detail.Monthly)-1].TotalWealthMinor
		if last != p.TerminalWealthMinor && p.Succeeded {
			t.Fatalf("terminal wealth mismatch: detail=%d summary=%d", last, p.TerminalWealthMinor)
		}
	}
}

func TestCollectDataWarningsUsesFrozenInstrumentName(t *testing.T) {
	in := testInputSnapshot()
	in.Assets[0].DataWarnings = []string{"仅有 1 个完整自然年度，收益与风险估计的不确定性较高"}
	in.Assets[0].InstrumentName = "沪深300ETF"
	in.Assets[0].InstrumentCode = "510300"
	result := Run(in, RunOptions{Runs: 10})
	for _, w := range result.Summary.ModelWarnings {
		if strings.Contains(w, "沪深300ETF") && strings.Contains(w, "510300") {
			return
		}
	}
	t.Fatalf("model warnings: %v", result.Summary.ModelWarnings)
}

func testInputSnapshot() *InputSnapshot {
	return &InputSnapshot{
		EngineVersion: EngineVersion,
		BaseCurrency:  "CNY",
		Parameters: SnapshotParameters{
			CurrentAge: 30, RetirementAge: 55, EndAge: 60,
			TotalAssetsMinor: 1_000_000_00, AnnualSavingsMinor: 100_000_00,
			AnnualSpendingMinor: 400_000_00, TerminalWealthFloorMinor: 0,
			InflationMode: "fixed", FixedInflationRate: 0.03,
			WithdrawalType: "fixed_real", WithdrawalRate: 0.04,
			WithdrawalFloorRatio: 0.7, WithdrawalCeilingRatio: 1.3,
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			SimulationRuns: 100, StudentTDf: 7, Seed: "42",
		},
		Assets: []SnapshotAsset{{
			HoldingID: "h1", AssetKey: "i1", SnapshotID: "s1",
			Currency: "CNY", AssetClass: "equity", IsCash: false,
			InitialMinor: 1_000_000_00, TargetWeight: 1.0,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15,
			SourceHash: "abc",
		}},
	}
}

func pearson(x, y []float64) float64 {
	n := float64(len(x))
	var sx, sy, sxx, syy, sxy float64
	for i := range x {
		sx += x[i]
		sy += y[i]
		sxx += x[i] * x[i]
		syy += y[i] * y[i]
		sxy += x[i] * y[i]
	}
	num := n*sxy - sx*sy
	den := math.Sqrt((n*sxx - sx*sx) * (n*syy - sy*sy))
	if den == 0 {
		return 0
	}
	return num / den
}
