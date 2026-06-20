package service

import (
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

// TestForwardSnapshotFreezesProfileTailParams covers td/063 R3: a forward
// (blended_prior) snapshot freezes the active profile's Student-t df and return
// truncation band into the InputSnapshot, ignoring the plan's legacy df.
func TestForwardSnapshotFreezesProfileTailParams(t *testing.T) {
	profile := assumptions.SystemDefaultProfile()
	profile.StudentTDf = 11
	profile.ReturnFloor = -0.80
	profile.ReturnCeil = 1.5

	resolved := resolvedAssumption{Profile: profile, Mode: assumptions.SourceBlendedPrior, Scenario: assumptions.ScenarioBaseline}
	plan := repository.Plan{ID: "p1", BaseCurrency: "CNY"}
	params := repository.PlanParameters{
		CurrentAge: 30, RetirementAge: 55, EndAge: 90,
		StudentTDf: 7, SimulationRuns: 100,
	}

	in, err := buildInputSnapshotStruct(plan, params, 42, "cfg", nil, resolved)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if in.EngineVersion != simulation.EngineVersion {
		t.Fatalf("forward run must be 3.0.0, got %s", in.EngineVersion)
	}
	if in.TailStudentTDf != 11 {
		t.Fatalf("frozen df = %d, want 11 (profile)", in.TailStudentTDf)
	}
	if in.EffectiveDf() != 11 {
		t.Fatalf("effective df = %d, want 11 (must ignore plan df 7)", in.EffectiveDf())
	}
	b := in.TailTruncationBounds()
	if b.Floor != -0.80 || b.Ceil != 1.5 {
		t.Fatalf("frozen truncation = %+v, want {-0.80, 1.5}", b)
	}
}

// TestLegacySnapshotKeepsConstantsAndPlanDf covers td/063 R3: a historical_cagr
// snapshot stays on the legacy engine and does not freeze profile tail params, so
// it falls back to the plan df and the engine constants for byte-for-byte replay.
func TestLegacySnapshotKeepsConstantsAndPlanDf(t *testing.T) {
	resolved := resolvedAssumption{
		Profile: assumptions.SystemDefaultProfile(),
		Mode:    assumptions.SourceHistoricalCAGR,
	}
	plan := repository.Plan{ID: "p1", BaseCurrency: "CNY"}
	params := repository.PlanParameters{
		CurrentAge: 30, RetirementAge: 55, EndAge: 90,
		StudentTDf: 9, SimulationRuns: 100,
	}
	in, err := buildInputSnapshotStruct(plan, params, 42, "cfg", nil, resolved)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if in.EngineVersion != simulation.LegacyEngineVersion {
		t.Fatalf("historical run must stay 2.0.0, got %s", in.EngineVersion)
	}
	if in.TailStudentTDf != 0 || in.TailReturnFloor != nil || in.TailReturnCeil != nil {
		t.Fatalf("legacy snapshot must not freeze profile tail params")
	}
	if in.EffectiveDf() != 9 {
		t.Fatalf("legacy effective df = %d, want plan df 9", in.EffectiveDf())
	}
	b := in.TailTruncationBounds()
	if b.Floor != simulation.ReturnFloor || b.Ceil != simulation.ReturnCeil {
		t.Fatalf("legacy truncation must use constants, got %+v", b)
	}
}
