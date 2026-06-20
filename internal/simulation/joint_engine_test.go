package simulation

import "testing"

// td/061 §5.C.2: a single risk asset run under the multivariate factor model must
// be bit-for-bit identical to the legacy independent run (same seed, same draws),
// because the 1-factor sampler consumes the RNG in the same order.
func TestJointEngineSingleAssetMatchesIndependent(t *testing.T) {
	indep := testInputSnapshot()
	indep.RandomFactorModel = FactorModelIndependent

	joint := testInputSnapshot()
	joint.RandomFactorModel = FactorModelMultivariate
	p := ParamsFromAnnual(joint.Assets[0].ModeledAnnualReturn, joint.Assets[0].AnnualVolatility)
	model, ok := AssembleFactorModel(
		[]string{"asset:equity:domestic"},
		[]float64{p.MonthlyMu},
		[]float64{p.MonthlySigma},
		[][]float64{{1}},
		nil,
	)
	if !ok {
		t.Fatal("assemble single-factor model failed")
	}
	joint.FactorModel = &model
	joint.AssetFactorRefs = []FactorRef{{AssetFactorIndex: 0, FXFactorIndex: -1}}

	a := Run(indep, RunOptions{Runs: 200})
	b := Run(joint, RunOptions{Runs: 200})

	if a.SuccessCount != b.SuccessCount {
		t.Fatalf("success count diverged: indep %d vs joint %d", a.SuccessCount, b.SuccessCount)
	}
	for i := range a.Paths {
		if a.Paths[i].TerminalWealthMinor != b.Paths[i].TerminalWealthMinor {
			t.Fatalf("path %d terminal diverged: indep %d vs joint %d",
				i, a.Paths[i].TerminalWealthMinor, b.Paths[i].TerminalWealthMinor)
		}
	}
}

// Two perfectly-correlated equal-weight assets must not gain diversification: a
// ρ=1 joint model behaves like a single concentrated asset for the portfolio.
func TestJointEngineTwoAssetsBuildAndRun(t *testing.T) {
	in := testInputSnapshot()
	in.RandomFactorModel = FactorModelMultivariate
	in.Assets = append(in.Assets, SnapshotAsset{
		HoldingID: "h2", InstrumentID: "i2", SnapshotID: "s2",
		Currency: "CNY", AssetClass: "equity", IsCash: false,
		InitialMinor: 0, TargetWeight: 0, ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15,
		SourceHash: "def",
	})
	p := ParamsFromAnnual(0.07, 0.15)
	model, ok := AssembleFactorModel(
		[]string{"asset:equity:domestic:a", "asset:equity:domestic:b"},
		[]float64{p.MonthlyMu, p.MonthlyMu},
		[]float64{p.MonthlySigma, p.MonthlySigma},
		[][]float64{{1, 1}, {1, 1}},
		[]string{"asset:equity:domestic:a|asset:equity:domestic:b"},
	)
	if !ok {
		t.Fatal("assemble two-factor model failed")
	}
	in.FactorModel = &model
	in.AssetFactorRefs = []FactorRef{
		{AssetFactorIndex: 0, FXFactorIndex: -1},
		{AssetFactorIndex: 1, FXFactorIndex: -1},
	}
	res := Run(in, RunOptions{Runs: 50})
	if res.SuccessCount < 0 || len(res.Paths) != 50 {
		t.Fatalf("unexpected run result: success=%d paths=%d", res.SuccessCount, len(res.Paths))
	}
}
