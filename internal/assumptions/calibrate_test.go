package assumptions

import (
	"math"
	"testing"
)

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// expectedBlended replicates the doc §3.3 log-space formula independently.
func expectedBlended(hist, prior, shift float64, completeYears, priorStrength int) (float64, float64) {
	n := completeYears
	if n > priorStrength {
		n = priorStrength
	}
	w := float64(n) / float64(n+priorStrength)
	gBase := w*math.Log(1+hist) + (1-w)*math.Log(1+prior)
	gFwd := gBase + shift
	return w, math.Exp(gFwd) - 1
}

func TestCalibrateBlendedWeights(t *testing.T) {
	p := SystemDefaultProfile() // prior_strength_years = 20
	const histReturn = 0.169564 // the td/061 P50 equity historical CAGR
	priorReturn := 0.065        // equity/foreign/CNY system prior
	for _, years := range []int{1, 13, 20, 30} {
		t.Run(map[int]string{1: "1y", 13: "13y", 20: "20y", 30: "30y"}[years], func(t *testing.T) {
			res, err := p.CalibrateForwardReturn(CalibrationInput{
				Source:                          SourceBlendedPrior,
				AssetClass:                      "equity",
				Region:                          "foreign",
				ValuationCurrency:               "CNY",
				HistoricalAnnualGeometricReturn: histReturn,
				HistoricalAnnualVolatility:      0.175,
				CompleteYearCount:               years,
				Scenario:                        ScenarioBaseline,
			})
			if err != nil {
				t.Fatal(err)
			}
			wantW, wantR := expectedBlended(histReturn, priorReturn, 0, years, 20)
			if !almost(res.HistoricalWeight, wantW) {
				t.Fatalf("weight = %.6f want %.6f", res.HistoricalWeight, wantW)
			}
			if !almost(res.ForwardAnnualGeometricReturn, wantR) {
				t.Fatalf("forward = %.9f want %.9f", res.ForwardAnnualGeometricReturn, wantR)
			}
			// Monthly mu compounds back to the forward geometric return.
			if !almost(math.Exp(res.MonthlyMu*12)-1, res.ForwardAnnualGeometricReturn) {
				t.Fatalf("monthly mu does not compound to forward return")
			}
		})
	}
}

func TestCalibrateThirteenYearWeightMatchesDoc(t *testing.T) {
	p := SystemDefaultProfile()
	res, err := p.CalibrateForwardReturn(CalibrationInput{
		Source: SourceBlendedPrior, AssetClass: "equity", Region: "foreign", ValuationCurrency: "CNY",
		HistoricalAnnualGeometricReturn: 0.169564, CompleteYearCount: 13, Scenario: ScenarioBaseline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !almost(res.HistoricalWeight, 13.0/33.0) {
		t.Fatalf("weight = %.6f want 13/33", res.HistoricalWeight)
	}
	// Forward return must be materially below the raw 16.96% historical CAGR.
	if res.ForwardAnnualGeometricReturn >= 0.169564 {
		t.Fatalf("forward %.4f should be shrunk below historical", res.ForwardAnnualGeometricReturn)
	}
}

func TestCalibrateScenarioShiftLogSpace(t *testing.T) {
	p := SystemDefaultProfile()
	base, _ := p.CalibrateForwardReturn(CalibrationInput{
		Source: SourceBlendedPrior, AssetClass: "equity", Region: "domestic", ValuationCurrency: "CNY",
		HistoricalAnnualGeometricReturn: 0.08, CompleteYearCount: 10, Scenario: ScenarioBaseline,
	})
	opt, _ := p.CalibrateForwardReturn(CalibrationInput{
		Source: SourceBlendedPrior, AssetClass: "equity", Region: "domestic", ValuationCurrency: "CNY",
		HistoricalAnnualGeometricReturn: 0.08, CompleteYearCount: 10, Scenario: ScenarioOptimistic,
	})
	// +0.015 in log space multiplies the 12-month compounding center by exp(0.015).
	if !almost(opt.ForwardLogReturn-base.ForwardLogReturn, 0.015) {
		t.Fatalf("optimistic log shift = %.6f want 0.015", opt.ForwardLogReturn-base.ForwardLogReturn)
	}
}

func TestCalibrateCustomCompoundsExactly(t *testing.T) {
	p := SystemDefaultProfile()
	custom := 0.05
	res, err := p.CalibrateForwardReturn(CalibrationInput{
		Source: SourceCustom, AssetClass: "equity", Region: "domestic", ValuationCurrency: "CNY",
		HistoricalAnnualGeometricReturn: 0.20, CustomAnnualGeometricReturn: &custom, Scenario: ScenarioBaseline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !almost(res.ForwardAnnualGeometricReturn, custom) {
		t.Fatalf("custom forward = %.9f want %.9f", res.ForwardAnnualGeometricReturn, custom)
	}
	if !almost(math.Exp(res.MonthlyMu*12)-1, custom) {
		t.Fatal("custom monthly mu does not compound exactly")
	}
}

func TestCalibrateHistoricalCAGRSource(t *testing.T) {
	p := SystemDefaultProfile()
	res, err := p.CalibrateForwardReturn(CalibrationInput{
		Source: SourceHistoricalCAGR, AssetClass: "equity", Region: "foreign", ValuationCurrency: "CNY",
		HistoricalAnnualGeometricReturn: 0.169564, CompleteYearCount: 13, Scenario: ScenarioBaseline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !almost(res.ForwardAnnualGeometricReturn, 0.169564) {
		t.Fatalf("historical source must keep the CAGR, got %.6f", res.ForwardAnnualGeometricReturn)
	}
	if res.HistoricalWeight != 1 {
		t.Fatalf("historical weight = %.3f want 1", res.HistoricalWeight)
	}
}

func TestCalibrateErrors(t *testing.T) {
	p := SystemDefaultProfile()
	base := CalibrationInput{
		Source: SourceBlendedPrior, AssetClass: "equity", Region: "domestic", ValuationCurrency: "CNY",
		HistoricalAnnualGeometricReturn: 0.08, CompleteYearCount: 10, Scenario: ScenarioBaseline,
	}
	t.Run("missing prior currency mismatch", func(t *testing.T) {
		in := base
		in.ValuationCurrency = "USD" // no USD-denominated prior exists
		if _, err := p.CalibrateForwardReturn(in); err == nil {
			t.Fatal("expected error for missing prior / currency mismatch")
		}
	})
	t.Run("missing prior asset class", func(t *testing.T) {
		in := base
		in.AssetClass = "gold"
		if _, err := p.CalibrateForwardReturn(in); err == nil {
			t.Fatal("expected error for unmapped asset class under blended_prior")
		}
	})
	t.Run("nan historical", func(t *testing.T) {
		in := base
		in.HistoricalAnnualGeometricReturn = math.NaN()
		if _, err := p.CalibrateForwardReturn(in); err == nil {
			t.Fatal("expected error for NaN historical return")
		}
	})
	t.Run("return below -100%", func(t *testing.T) {
		in := base
		in.HistoricalAnnualGeometricReturn = -1.2
		if _, err := p.CalibrateForwardReturn(in); err == nil {
			t.Fatal("expected error for return <= -100%")
		}
	})
	t.Run("unknown scenario", func(t *testing.T) {
		in := base
		in.Scenario = "moonshot"
		if _, err := p.CalibrateForwardReturn(in); err == nil {
			t.Fatal("expected error for unknown scenario")
		}
	})
	t.Run("custom missing value", func(t *testing.T) {
		in := base
		in.Source = SourceCustom
		if _, err := p.CalibrateForwardReturn(in); err == nil {
			t.Fatal("expected error for custom without value")
		}
	})
}

func TestCalibrateVolatilityClipAndScenario(t *testing.T) {
	p := SystemDefaultProfile()
	// bond/domestic ceiling is 0.10; a 0.30 historical vol must clip to 0.10 then
	// be scaled by the conservative 1.15 multiplier.
	res, err := p.CalibrateForwardReturn(CalibrationInput{
		Source: SourceBlendedPrior, AssetClass: "bond", Region: "domestic", ValuationCurrency: "CNY",
		HistoricalAnnualGeometricReturn: 0.03, HistoricalAnnualVolatility: 0.30,
		CompleteYearCount: 10, Scenario: ScenarioConservative,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !almost(res.AnnualVolatilityUsed, 0.10*1.15) {
		t.Fatalf("vol used = %.4f want %.4f", res.AnnualVolatilityUsed, 0.10*1.15)
	}
	if !almost(res.MonthlyVolatility, res.AnnualVolatilityUsed/math.Sqrt(12)) {
		t.Fatal("monthly vol mismatch")
	}
}

func TestCalibrateCashUsesPriorWithZeroVol(t *testing.T) {
	p := SystemDefaultProfile()
	res, err := p.CalibrateForwardReturn(CalibrationInput{
		Source: SourceBlendedPrior, AssetClass: "cash", Region: "domestic", ValuationCurrency: "CNY",
		HistoricalAnnualGeometricReturn: 0, HistoricalAnnualVolatility: 0,
		CompleteYearCount: 0, Scenario: ScenarioBaseline,
	})
	if err != nil {
		t.Fatal(err)
	}
	// With zero complete years the weight is 0, so the forward return is the prior.
	if res.HistoricalWeight != 0 {
		t.Fatalf("cash weight = %.3f want 0", res.HistoricalWeight)
	}
	if !almost(res.ForwardAnnualGeometricReturn, 0.018) {
		t.Fatalf("cash forward = %.4f want 0.018", res.ForwardAnnualGeometricReturn)
	}
	if res.AnnualVolatilityUsed != 0 {
		t.Fatalf("cash vol = %.4f want 0", res.AnnualVolatilityUsed)
	}
}

func TestCalibrateFXBlend(t *testing.T) {
	p := SystemDefaultProfile()
	res, err := p.CalibrateFX(FXCalibrationInput{
		FromCurrency: "USD", BaseCurrency: "CNY",
		HistoricalAnnualGeometricReturn: 0.02, HistoricalAnnualVolatility: 0.06,
		CompleteYearCount: 10, Scenario: ScenarioBaseline,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantW, wantR := expectedBlended(0.02, 0.0, 0, 10, 20)
	if !almost(res.HistoricalWeight, wantW) || !almost(res.ForwardAnnualGeometricReturn, wantR) {
		t.Fatalf("fx blend w=%.6f r=%.9f want w=%.6f r=%.9f",
			res.HistoricalWeight, res.ForwardAnnualGeometricReturn, wantW, wantR)
	}
}

func TestCalibrateFXScenarioUsesFXShift(t *testing.T) {
	p := SystemDefaultProfile()
	// The system optimistic scenario has return_shift_log_fx = 0, so optimistic FX
	// must equal baseline FX (no mechanical currency view).
	base, _ := p.CalibrateFX(FXCalibrationInput{
		FromCurrency: "USD", BaseCurrency: "CNY", HistoricalAnnualGeometricReturn: 0.02,
		CompleteYearCount: 10, Scenario: ScenarioBaseline,
	})
	opt, _ := p.CalibrateFX(FXCalibrationInput{
		FromCurrency: "USD", BaseCurrency: "CNY", HistoricalAnnualGeometricReturn: 0.02,
		CompleteYearCount: 10, Scenario: ScenarioOptimistic,
	})
	if !almost(base.ForwardLogReturn, opt.ForwardLogReturn) {
		t.Fatal("FX should not move with the asset optimistic scenario shift")
	}
}
