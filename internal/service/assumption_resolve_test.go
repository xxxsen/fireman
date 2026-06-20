package service

import (
	"math"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
)

func sysResolved(mode, scenario string) resolvedAssumption {
	if scenario == "" {
		scenario = assumptions.ScenarioBaseline
	}
	return resolvedAssumption{
		Profile:  assumptions.SystemDefaultProfile(),
		Scenario: scenario,
		Mode:     mode,
	}
}

func TestCalibrateAssetHistoricalPreservesReturn(t *testing.T) {
	res := sysResolved(assumptions.SourceHistoricalCAGR, "")
	out, err := calibrateAsset(res, "ins_eq", "equity", "foreign", "CNY", 0.169564, 0.1755, 13, nil)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(out.ForwardAnnualGeometricReturn-0.169564) > 1e-12 {
		t.Fatalf("historical mode must preserve return, got %.6f", out.ForwardAnnualGeometricReturn)
	}
	if math.Abs(out.AnnualVolatilityUsed-0.1755) > 1e-12 {
		t.Fatalf("historical mode must preserve volatility, got %.6f", out.AnnualVolatilityUsed)
	}
}

func TestCalibrateAssetBlendedShrinksReturn(t *testing.T) {
	res := sysResolved(assumptions.SourceBlendedPrior, assumptions.ScenarioBaseline)
	out, err := calibrateAsset(res, "ins_eq", "equity", "foreign", "CNY", 0.169564, 0.40, 13, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.ForwardAnnualGeometricReturn >= 0.169564 {
		t.Fatalf("blended mode must shrink below historical, got %.6f", out.ForwardAnnualGeometricReturn)
	}
	if math.Abs(out.HistoricalWeight-13.0/33.0) > 1e-9 {
		t.Fatalf("expected weight 13/33, got %.6f", out.HistoricalWeight)
	}
	// 40% historical vol clips to the equity/foreign ceiling (0.40) then *1.0.
	if out.AnnualVolatilityUsed > 0.40+1e-9 {
		t.Fatalf("blended vol must respect ceiling, got %.6f", out.AnnualVolatilityUsed)
	}
}

func TestCalibrateAssetBlendedMissingPriorErrors(t *testing.T) {
	res := sysResolved(assumptions.SourceBlendedPrior, assumptions.ScenarioBaseline)
	if _, err := calibrateAsset(res, "ins_us", "equity", "foreign", "USD", 0.10, 0.18, 10, nil); err == nil {
		t.Fatal("expected error for unmapped USD prior under blended_prior")
	}
}

func TestCalibrateAssetCustomUsesPerInstrumentValue(t *testing.T) {
	res := sysResolved(assumptions.SourceCustom, assumptions.ScenarioBaseline)
	custom := map[string]float64{"ins_eq": 0.05}
	out, err := calibrateAsset(res, "ins_eq", "equity", "domestic", "CNY", 0.20, 0.18, 10, custom)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(out.ForwardAnnualGeometricReturn-0.05) > 1e-12 {
		t.Fatalf("custom must use per-instrument value, got %.6f", out.ForwardAnnualGeometricReturn)
	}
}
