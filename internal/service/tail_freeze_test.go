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
	// A profile with edited tail params is, by definition, a user-customized copy
	// (the system identity is immutable), so mark it user-owned: the snapshot
	// provenance guard only blesses byte-faithful system content (td/067 R13/R14).
	profile := assumptions.SystemDefaultProfile()
	profile.OwnerScope = assumptions.OwnerUser
	profile.ID = "user_tail_custom"
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

// TestSnapshotRecordsAssumptionProvenance covers td/066 R11/R12 acceptance #4: a
// run snapshot freezes the resolved profile identity, its canonical content hash
// and (for the current system profile) the backing CMA evidence hash, so a result
// is always explainable by a specific immutable model. For the system default this
// must be system_cma_v3@1 with the registry-pinned hashes.
func TestSnapshotRecordsAssumptionProvenance(t *testing.T) {
	profile := assumptions.SystemDefaultProfile()
	resolved := resolvedAssumption{
		Profile: profile, Mode: assumptions.SourceBlendedPrior, Scenario: assumptions.ScenarioBaseline,
	}
	plan := repository.Plan{ID: "p1", BaseCurrency: "CNY"}
	params := repository.PlanParameters{
		CurrentAge: 30, RetirementAge: 55, EndAge: 90, StudentTDf: 7, SimulationRuns: 100,
	}
	in, err := buildInputSnapshotStruct(plan, params, 42, "cfg", nil, resolved)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if in.AssumptionProfileID != assumptions.SystemProfileID ||
		in.AssumptionProfileVersion != assumptions.SystemProfileVersion {
		t.Fatalf("provenance identity = %s@%d, want %s@%d",
			in.AssumptionProfileID, in.AssumptionProfileVersion,
			assumptions.SystemProfileID, assumptions.SystemProfileVersion)
	}
	wantHash, err := profile.ContentHash()
	if err != nil {
		t.Fatalf("content hash: %v", err)
	}
	cur := assumptions.CurrentSystemIdentity()
	if in.AssumptionProfileContentHash != wantHash || in.AssumptionProfileContentHash != cur.CanonicalHash {
		t.Fatalf("provenance content hash = %q, want %q (registry %q)",
			in.AssumptionProfileContentHash, wantHash, cur.CanonicalHash)
	}
	if in.AssumptionEvidenceHash != assumptions.CMAEvidenceContentHash {
		t.Fatalf("provenance evidence hash = %q, want %q",
			in.AssumptionEvidenceHash, assumptions.CMAEvidenceContentHash)
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
