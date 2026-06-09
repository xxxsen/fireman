package sensitivity

import (
	"testing"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

func TestDefaultPerturbationCount(t *testing.T) {
	pts := DefaultPerturbations()
	if len(pts) != 35 {
		t.Fatalf("expected 35 default points (7x5), got %d", len(pts))
	}
}

func TestCommonRandomNumbersBaselinePoint(t *testing.T) {
	in := testSensitivityInput()
	in.Parameters.SimulationRuns = 200
	report, err := Run(in, RunOptions{Runs: 200})
	if err != nil {
		t.Fatal(err)
	}
	var baseline PointResult
	for _, p := range report.Points {
		if p.ParameterID == ParamInitialAssets && p.Delta == 0 {
			baseline = p
			break
		}
	}
	if baseline.SuccessProbability != report.BaselineSuccessProbability {
		t.Fatalf("baseline point should match report baseline: %f vs %f",
			baseline.SuccessProbability, report.BaselineSuccessProbability)
	}
}

func TestEquityWeightPerturbationPreservesCash(t *testing.T) {
	in := testSensitivityInput()
	in.Assets = append(in.Assets,
		simulation.SnapshotAsset{
			HoldingID: "bond", AssetClass: domain.AssetClassBond,
			InitialMinor: 300_000_00, TargetWeight: 0.3,
			ModeledAnnualReturn: 0.04, AnnualVolatility: 0.05, SourceHash: "b",
		},
		simulation.SnapshotAsset{
			HoldingID: "cash", AssetClass: domain.AssetClassCash, IsCash: true,
			InitialMinor: 100_000_00, TargetWeight: 0.1,
			ModeledAnnualReturn: 0, AnnualVolatility: 0, SourceHash: "c",
		},
	)
	in.Assets[0].TargetWeight = 0.6
	in.Assets[0].InitialMinor = 600_000_00

	out, err := ApplyPerturbation(in, PerturbationPoint{
		ParameterID: ParamEquityWeight, Delta: 0.10, DeltaUnit: "pp",
	})
	if err != nil {
		t.Fatal(err)
	}
	var eq, bond, cash float64
	for _, a := range out.Assets {
		switch a.AssetClass {
		case domain.AssetClassEquity:
			eq += a.TargetWeight
		case domain.AssetClassBond:
			bond += a.TargetWeight
		case domain.AssetClassCash:
			cash += a.TargetWeight
		}
	}
	if cash < 0.099 || cash > 0.101 {
		t.Fatalf("cash weight should stay ~0.1, got %f", cash)
	}
	if eq+bond+cash < 0.999 || eq+bond+cash > 1.001 {
		t.Fatalf("weights should sum to 1, got %f", eq+bond+cash)
	}
}

func TestHeatmapShape(t *testing.T) {
	in := testSensitivityInput()
	in.Parameters.SimulationRuns = 50
	report, err := Run(in, RunOptions{Runs: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Heatmap) != 5 {
		t.Fatalf("expected 5 heatmap rows, got %d", len(report.Heatmap))
	}
	if len(report.Heatmap[0]) != 5 {
		t.Fatalf("expected 5 heatmap cols, got %d", len(report.Heatmap[0]))
	}
	if len(report.Tornado) == 0 {
		t.Fatal("expected tornado data")
	}
}

func testSensitivityInput() *simulation.InputSnapshot {
	return &simulation.InputSnapshot{
		EngineVersion: simulation.EngineVersion,
		BaseCurrency:  "CNY",
		Parameters: simulation.SnapshotParameters{
			CurrentAge: 40, RetirementAge: 55, EndAge: 90,
			TotalAssetsMinor: 1_000_000_00, AnnualSavingsMinor: 100_000_00,
			AnnualSpendingMinor: 400_000_00, TerminalWealthFloorMinor: 0,
			InflationMode: "fixed", FixedInflationRate: 0.03,
			WithdrawalType: "fixed_real", WithdrawalRate: 0.04,
			WithdrawalFloorRatio: 0.7, WithdrawalCeilingRatio: 1.3,
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			SimulationRuns: 100, StudentTDf: 7, Seed: "99",
		},
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "h1", InstrumentID: "i1", SnapshotID: "s1",
			Currency: "CNY", AssetClass: domain.AssetClassEquity, IsCash: false,
			InitialMinor: 1_000_000_00, TargetWeight: 1.0,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15, MaxDrawdown: 0.20,
			SourceHash: "eq",
		}},
	}
}
