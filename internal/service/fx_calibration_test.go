package service

import (
	"math"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/simulation"
)

// The forward FX calibration must reach production via
// applyFXCalibration. These cover the three currency combinations the doc calls
// out: a native-currency (USD) holding gets a forward, prior-blended FX drift;
// historical_cagr keeps the raw historical FX drift; and a holding whose
// currency has no FX prior is blocked rather than silently using history.

func TestApplyFXCalibrationBlendedUsesProfilePrior(t *testing.T) {
	res := sysResolved(assumptions.SourceBlendedPrior, assumptions.ScenarioBaseline)
	sa := simulation.SnapshotAsset{FXModeledReturn: 0.20, FXAnnualVolatility: 0.18}
	const hist, histVol = 0.20, 0.18
	fx := marketdata.SnapshotMetrics{CompleteYearCount: 12}

	if err := applyFXCalibration(&sa, res, "USD", "CNY", hist, histVol, fx); err != nil {
		t.Fatalf("blended USD->CNY should calibrate, got %v", err)
	}
	// The system USD FX prior is 0.0 geometric, so blending pulls the 20%
	// historical drift down toward the prior: forward must differ from history.
	if math.Abs(sa.FXModeledReturn-hist) < 1e-9 {
		t.Fatalf("forward FX drift should differ from historical %.4f, got %.6f", hist, sa.FXModeledReturn)
	}
	if sa.FXReturnSource == "" || sa.FXReturnSource == assumptions.SourceHistoricalCAGR {
		t.Fatalf("forward FX source should be calibrated, got %q", sa.FXReturnSource)
	}
	if sa.FXReturnScenario != assumptions.ScenarioBaseline {
		t.Fatalf("FX scenario not frozen, got %q", sa.FXReturnScenario)
	}
}

func TestApplyFXCalibrationHistoricalKeepsHistory(t *testing.T) {
	res := sysResolved(assumptions.SourceHistoricalCAGR, "")
	sa := simulation.SnapshotAsset{FXModeledReturn: 0.20, FXAnnualVolatility: 0.18}
	fx := marketdata.SnapshotMetrics{CompleteYearCount: 12}

	if err := applyFXCalibration(&sa, res, "USD", "CNY", 0.20, 0.18, fx); err != nil {
		t.Fatalf("historical_cagr should not error, got %v", err)
	}
	if math.Abs(sa.FXModeledReturn-0.20) > 1e-12 {
		t.Fatalf("historical mode must keep the historical FX drift, got %.6f", sa.FXModeledReturn)
	}
}

func TestApplyFXCalibrationMissingPriorBlocks(t *testing.T) {
	res := sysResolved(assumptions.SourceBlendedPrior, assumptions.ScenarioBaseline)
	sa := simulation.SnapshotAsset{}
	fx := marketdata.SnapshotMetrics{CompleteYearCount: 12}

	// JPY has no FX prior in the system profile: a forward run must be blocked.
	if err := applyFXCalibration(&sa, res, "JPY", "CNY", 0.05, 0.10, fx); err == nil {
		t.Fatal("missing FX prior under blended_prior must error, got nil")
	}
}
