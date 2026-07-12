package service

import (
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

func TestBuildRunAssumptionViewUsesRunLevelSelection(t *testing.T) {
	snap := simulation.InputSnapshot{
		EngineVersion:              simulation.EngineVersion,
		ReturnAssumptionMode:       assumptions.SourceBlendedPrior,
		ReturnAssumptionScenario:   assumptions.ScenarioOptimistic,
		ReturnAssumptionSetID:      "profile_1",
		ReturnAssumptionSetVersion: 7,
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "cash", IsCash: true, ReturnAssumptionSource: "cash_rule",
		}, {
			HoldingID: "override", ReturnAssumptionSource: "plan_override",
			ReturnAssumptionScenario: assumptions.ScenarioConservative,
		}},
	}
	view := buildRunAssumptionView(snap)
	if view == nil || view.Mode != assumptions.SourceBlendedPrior ||
		view.Scenario != assumptions.ScenarioOptimistic || view.ProfileID != "profile_1" ||
		view.ProfileVersion != 7 {
		t.Fatalf("run-level assumption view = %+v", view)
	}
}

func TestBuildRunAssumptionViewDoesNotGuessLegacyMode(t *testing.T) {
	view := buildRunAssumptionView(simulation.InputSnapshot{
		EngineVersion: "3.3.0",
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "override", ReturnAssumptionSource: "plan_override",
			ReturnAssumptionScenario: assumptions.ScenarioConservative,
		}},
	})
	if view == nil || view.Mode != "" || view.Scenario != "" || view.ProfileID != "" {
		t.Fatalf("legacy run-level values must remain unknown: %+v", view)
	}
}

func TestBuildInputSnapshotFreezesSelectionAndNormalizesInactiveFields(t *testing.T) {
	params := validParametersFixture()
	params.InflationMode = "random_ar1"
	params.FixedInflationRate = 0.19
	params.WithdrawalType = "guardrail"
	params.WithdrawalRate = 0.75
	resolved := sysResolved(assumptions.SourceHistoricalCAGR, assumptions.ScenarioConservative)
	in, err := buildInputSnapshotStruct(
		repository.Plan{ID: "plan_1", BaseCurrency: "CNY"}, params, 1, "hash", nil, resolved,
	)
	if err != nil {
		t.Fatal(err)
	}
	if in.ReturnAssumptionMode != assumptions.SourceHistoricalCAGR ||
		in.ReturnAssumptionScenario != assumptions.ScenarioConservative ||
		in.ReturnAssumptionSetID != resolved.Profile.ID ||
		in.ReturnAssumptionSetVersion != resolved.Profile.Version {
		t.Fatalf("frozen selection = %+v", in)
	}
	if in.Parameters.FixedInflationRate != 0 || in.Parameters.WithdrawalRate != 0 {
		t.Fatalf("inactive snapshot fields were not normalized: %+v", in.Parameters)
	}
}
