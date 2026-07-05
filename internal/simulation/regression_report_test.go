package simulation

import (
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
)

// TestForwardReturnRegressionReport builds three frozen
// assumption variants for the documented 90/10 case that differ ONLY in the
// forward geometric return (cash-flows, seed, volatility and rebalancing held
// constant) and emit a regression report. Acceptance only requires direction and
// reproducibility, never a target success rate.
//
// Documented case: ¥4,000,000 initial, retire at 35, 50y/600m horizon, 3% fixed
// inflation, ¥120,000 fixed real spending, 90% foreign equity (historical CAGR
// 16.9564%, vol 17.5502%, 13 complete years) + 10% CNY cash.
func TestForwardReturnRegressionReport(t *testing.T) {
	const (
		histCAGR   = 0.169564
		histVol    = 0.175502
		sampleYrs  = 13
		runs       = 1000
		equityWt   = 0.90
		cashWt     = 0.10
		totalMinor = 4_000_000_00
	)
	profile := assumptions.SystemDefaultProfile()

	calibrate := func(source, scenario string) float64 {
		res, err := profile.CalibrateForwardReturn(assumptions.CalibrationInput{
			Source:                          source,
			AssetClass:                      "equity",
			Region:                          "foreign",
			ValuationCurrency:               "CNY",
			HistoricalAnnualGeometricReturn: histCAGR,
			HistoricalAnnualVolatility:      histVol,
			CompleteYearCount:               sampleYrs,
			Scenario:                        scenario,
		})
		if err != nil {
			t.Fatalf("calibrate %s/%s: %v", source, scenario, err)
		}
		return res.ForwardAnnualGeometricReturn
	}

	histFwd := calibrate(assumptions.SourceHistoricalCAGR, assumptions.ScenarioBaseline)
	baseFwd := calibrate(assumptions.SourceBlendedPrior, assumptions.ScenarioBaseline)
	consFwd := calibrate(assumptions.SourceBlendedPrior, assumptions.ScenarioConservative)

	// Calibration direction: shrinking 16.96% toward the 6.5% prior must lower the
	// forward return, and the conservative log shift must lower it further.
	if baseFwd >= histFwd {
		t.Fatalf("expected blended baseline %.4f < historical %.4f", baseFwd, histFwd)
	}
	if consFwd >= baseFwd {
		t.Fatalf("expected conservative %.4f < baseline %.4f", consFwd, baseFwd)
	}

	build := func(fwd float64) *InputSnapshot {
		return &InputSnapshot{
			EngineVersion:     EngineVersion,
			BaseCurrency:      "CNY",
			RandomFactorModel: FactorModelIndependent,
			Parameters: SnapshotParameters{
				CurrentAge: 35, RetirementAge: 35, EndAge: 85,
				TotalAssetsMinor:    totalMinor,
				AnnualSavingsMinor:  0,
				AnnualSpendingMinor: 120_000_00,
				InflationMode:       "fixed", FixedInflationRate: 0.03,
				WithdrawalType: "fixed_real", WithdrawalRate: 0.03,
				WithdrawalFloorRatio: 0.7, WithdrawalCeilingRatio: 1.3,
				RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
				SimulationRuns: runs, StudentTDf: profile.StudentTDf, Seed: "42",
			},
			Assets: []SnapshotAsset{
				{
					HoldingID: "h1", AssetKey: "i1", SnapshotID: "s1",
					Currency: "CNY", AssetClass: "equity", Region: "foreign", IsCash: false,
					InitialMinor: int64(totalMinor * equityWt), TargetWeight: equityWt,
					// Only the forward return varies across variants; volatility is fixed.
					ModeledAnnualReturn: fwd, AnnualVolatility: histVol,
					CompleteYearCount: sampleYrs, SourceHash: "equity",
				},
				{
					HoldingID: "h2", AssetKey: "i2", SnapshotID: "s2",
					Currency: "CNY", AssetClass: "cash", IsCash: true,
					InitialMinor: int64(totalMinor * cashWt), TargetWeight: cashWt,
					ModeledAnnualReturn: 0, AnnualVolatility: 0, SourceHash: "cash",
				},
			},
		}
	}

	type variant struct {
		label string
		fwd   float64
	}
	variants := []variant{
		{"historical_cagr", histFwd},
		{"blended_prior/baseline", baseFwd},
		{"blended_prior/conservative", consFwd},
	}

	p50 := map[string]int64{}
	for _, v := range variants {
		snap := build(v.fwd)
		res := Run(snap, RunOptions{Runs: runs})

		nominalP50 := res.Summary.TerminalQuantiles["p50"]
		realP50 := res.Summary.RealTerminalQuantiles["p50"]
		p05 := int64(0)
		if n := len(res.QuantileSeries); n > 0 {
			p05 = res.QuantileSeries[n-1].P05Minor
		}
		successRate := float64(res.SuccessCount) / float64(runs)
		ddP95 := res.Summary.MaxDrawdownQuantiles["p95"]

		t.Logf("[%s] fwd=%.4f%% | terminal P05=%d P50=%d P95=%d | success=%.2f%% | maxDD P95=%.2f%% | real P50=%d",
			v.label, v.fwd*100, p05, nominalP50, res.Summary.TerminalQuantiles["p95"],
			successRate*100, ddP95*100, realP50)

		// Real purchasing power must be strictly below nominal under positive inflation.
		if realP50 >= nominalP50 {
			t.Fatalf("[%s] real P50 %d should be below nominal P50 %d", v.label, realP50, nominalP50)
		}
		p50[v.label] = nominalP50
	}

	// Return-shrinkage direction on the headline P50 terminal wealth.
	if p50["blended_prior/baseline"] >= p50["historical_cagr"] {
		t.Fatalf("expected blended baseline P50 %d < historical P50 %d",
			p50["blended_prior/baseline"], p50["historical_cagr"])
	}
	if p50["blended_prior/conservative"] >= p50["blended_prior/baseline"] {
		t.Fatalf("expected conservative P50 %d < baseline P50 %d",
			p50["blended_prior/conservative"], p50["blended_prior/baseline"])
	}

	// Reproducibility: identical snapshot, seed and version are bitwise stable.
	r1 := Run(build(baseFwd), RunOptions{Runs: runs})
	r2 := Run(build(baseFwd), RunOptions{Runs: runs})
	if r1.SuccessCount != r2.SuccessCount {
		t.Fatalf("non-reproducible success count: %d vs %d", r1.SuccessCount, r2.SuccessCount)
	}
	for _, k := range []string{"p00", "p25", "p50", "p75", "p95"} {
		if r1.Summary.TerminalQuantiles[k] != r2.Summary.TerminalQuantiles[k] {
			t.Fatalf("non-reproducible terminal quantile %s: %d vs %d",
				k, r1.Summary.TerminalQuantiles[k], r2.Summary.TerminalQuantiles[k])
		}
	}
}
