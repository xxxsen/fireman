package service

import (
	"math"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/simulation"
)

func TestPortfolioReturnAndVolUsesCovariance(t *testing.T) {
	monthlySigma := []float64{0.10 / math.Sqrt(12), 0.20 / math.Sqrt(12)}
	model, ok := simulation.AssembleFactorModel(
		[]string{"a", "b"}, []float64{0, 0}, monthlySigma,
		[][]float64{{1, 0}, {0, 1}}, nil,
	)
	if !ok {
		t.Fatal("assemble factor model")
	}
	in := simulation.InputSnapshot{
		BaseCurrency: "CNY", FactorModel: &model,
		Assets: []simulation.SnapshotAsset{
			{TargetWeight: 0.5, ModeledAnnualReturn: 0.04},
			{TargetWeight: 0.5, ModeledAnnualReturn: 0.08},
		},
		AssetFactorRefs: []simulation.FactorRef{
			{AssetFactorIndex: 0, FXFactorIndex: -1},
			{AssetFactorIndex: 1, FXFactorIndex: -1},
		},
	}
	forward, vol, err := portfolioReturnAndVol(in)
	if err != nil {
		t.Fatal(err)
	}
	wantVol := math.Sqrt(0.5*0.5*0.10*0.10 + 0.5*0.5*0.20*0.20)
	if math.Abs(forward-0.06) > 1e-12 || math.Abs(vol-wantVol) > 1e-12 {
		t.Fatalf("forward/vol = %.12f/%.12f, want 0.06/%.12f", forward, vol, wantVol)
	}
	if math.Abs(vol-0.15) < 1e-6 {
		t.Fatal("portfolio volatility regressed to weighted average of asset volatilities")
	}
}

func TestPortfolioReturnAndVolComposesFXAndSharedExposure(t *testing.T) {
	model := simulation.FactorModel{
		Factors: []string{"a", "b", "fx"},
		Sigma: [][]float64{
			{0, 0, 0}, {0, 0, 0}, {0, 0, 0.01 / 12},
		},
	}
	in := simulation.InputSnapshot{
		BaseCurrency: "CNY", FactorModel: &model,
		Assets: []simulation.SnapshotAsset{
			{TargetWeight: 0.4, Currency: "USD", FXSnapshotID: "fx", ModeledAnnualReturn: 0.10, FXModeledReturn: 0.05},
			{TargetWeight: 0.6, Currency: "USD", FXSnapshotID: "fx", ModeledAnnualReturn: 0.10, FXModeledReturn: 0.05},
		},
		AssetFactorRefs: []simulation.FactorRef{
			{AssetFactorIndex: 0, FXFactorIndex: 2},
			{AssetFactorIndex: 1, FXFactorIndex: 2},
		},
	}
	forward, vol, err := portfolioReturnAndVol(in)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(forward-0.155) > 1e-12 {
		t.Fatalf("base-currency forward return = %.12f, want 0.155", forward)
	}
	if math.Abs(vol-0.10) > 1e-12 {
		t.Fatalf("shared FX exposure volatility = %.12f, want 0.10", vol)
	}
}

func TestDeriveScenarioSnapshotPreservesOverrideAndCorrelation(t *testing.T) {
	profile := assumptions.SystemDefaultProfile()
	baseParams := simulation.ParamsFromAnnual(0.08, 0.20)
	model, ok := simulation.AssembleFactorModel(
		[]string{"equity#h1"}, []float64{baseParams.MonthlyMu},
		[]float64{baseParams.MonthlySigma}, [][]float64{{1}}, nil,
	)
	if !ok {
		t.Fatal("assemble base factor model")
	}
	override := 0.123
	base := simulation.InputSnapshot{
		EngineVersion: simulation.EngineVersion, BaseCurrency: "CNY",
		ScenarioComparisonReady: true, ScenarioComparisonFactorModel: &model,
		AssetFactorRefs: []simulation.FactorRef{{AssetFactorIndex: 0, FXFactorIndex: -1}},
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "h1", AssetKey: "asset_1", AssetClass: "equity", Region: "domestic",
			Currency: "CNY", TargetWeight: 1, CompleteYearCount: 10,
			HistoricalAnnualGeometricReturn: 0.08, HistoricalAnnualVolatility: 0.20,
			OverrideForwardReturn: &override, OverrideReason: "frozen view",
		}},
	}
	derived, err := deriveScenarioSnapshot(base, profile, assumptions.ScenarioOptimistic)
	if err != nil {
		t.Fatal(err)
	}
	asset := derived.Assets[0]
	if asset.ModeledAnnualReturn != override || asset.ReturnAssumptionSource != "plan_override" ||
		asset.ReturnAssumptionScenario != assumptions.ScenarioOptimistic {
		t.Fatalf("derived override audit = %+v", asset)
	}
	if derived.FactorModel == nil || derived.FactorModel.Audit.RPSD[0][0] != 1 {
		t.Fatalf("frozen correlation not retained: %+v", derived.FactorModel)
	}
}
