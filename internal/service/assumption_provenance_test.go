package service

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
)

func loadProfileFixture(t *testing.T, name string) assumptions.Profile {
	t.Helper()
	raw, err := os.ReadFile("../repository/testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var p assumptions.Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return p
}

func provenancePlanParams() (repository.Plan, repository.PlanParameters) {
	return repository.Plan{ID: "p1", BaseCurrency: "CNY"},
		repository.PlanParameters{CurrentAge: 30, RetirementAge: 55, EndAge: 90, StudentTDf: 7, SimulationRuns: 100}
}

// TestSnapshotPinnedV2VariantProvenance covers td/067 R14 acceptance #2: an
// explicit pin of the TD 065 v2 VARIANT records that variant's own historical CMA
// evidence hash and canonical hash — not the current v3 evidence — so historical
// replay provenance is exact.
func TestSnapshotPinnedV2VariantProvenance(t *testing.T) {
	variant := loadProfileFixture(t, "system_cma_v2_td065_canonical.json")
	if variant.OwnerScope != assumptions.OwnerSystem {
		t.Fatalf("v2 variant fixture must be system-owned, got %q", variant.OwnerScope)
	}
	resolved := resolvedAssumption{
		Profile: variant, Mode: assumptions.SourceHistoricalCAGR, Scenario: assumptions.ScenarioBaseline,
	}
	plan, params := provenancePlanParams()
	in, err := buildInputSnapshotStruct(plan, params, 42, "cfg", nil, resolved)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	contentHash, _ := variant.ContentHash()
	v, ok := assumptions.LookupSystemContent(variant.ID, variant.Version, contentHash)
	if !ok {
		t.Fatalf("TD065 v2 variant content %s must be recognized", contentHash)
	}
	if in.AssumptionProfileContentHash != contentHash {
		t.Fatalf("content hash = %q, want %q", in.AssumptionProfileContentHash, contentHash)
	}
	if in.AssumptionEvidenceHash != v.EvidenceHash || in.AssumptionEvidenceHash == "" {
		t.Fatalf("evidence hash = %q, want the variant's own %q", in.AssumptionEvidenceHash, v.EvidenceHash)
	}
	// It must NOT inherit the current v3 evidence.
	if in.AssumptionEvidenceHash == assumptions.CMAEvidenceContentHash {
		t.Fatal("a v2 variant must not inherit the current v3 evidence hash")
	}
}

// TestSnapshotTD064V2HasNoEvidenceHash covers td/067 R14: the TD 064 v2 content is
// recognized but has no backing evidence artifact, so its run snapshot records an
// empty evidence hash (and its own canonical hash).
func TestSnapshotTD064V2HasNoEvidenceHash(t *testing.T) {
	v2 := loadProfileFixture(t, "system_cma_v2_canonical.json")
	resolved := resolvedAssumption{
		Profile: v2, Mode: assumptions.SourceHistoricalCAGR, Scenario: assumptions.ScenarioBaseline,
	}
	plan, params := provenancePlanParams()
	in, err := buildInputSnapshotStruct(plan, params, 42, "cfg", nil, resolved)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if in.AssumptionEvidenceHash != "" {
		t.Fatalf("TD064 v2 must have no evidence hash, got %q", in.AssumptionEvidenceHash)
	}
	contentHash, _ := v2.ContentHash()
	if in.AssumptionProfileContentHash != contentHash {
		t.Fatalf("content hash = %q, want %q", in.AssumptionProfileContentHash, contentHash)
	}
}

// TestSnapshotUserProfileNeverInheritsEvidence covers td/067 R13 #4: a user profile
// (even one that copies the system content verbatim) never inherits official CMA
// evidence provenance.
func TestSnapshotUserProfileNeverInheritsEvidence(t *testing.T) {
	p := assumptions.SystemDefaultProfile()
	p.OwnerScope = assumptions.OwnerUser
	p.ID = "user_copy"
	resolved := resolvedAssumption{
		Profile: p, Mode: assumptions.SourceBlendedPrior, Scenario: assumptions.ScenarioBaseline,
	}
	plan, params := provenancePlanParams()
	in, err := buildInputSnapshotStruct(plan, params, 42, "cfg", nil, resolved)
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if in.AssumptionEvidenceHash != "" {
		t.Fatalf("user profile must not carry an evidence hash, got %q", in.AssumptionEvidenceHash)
	}
	if in.AssumptionProfileID != "user_copy" {
		t.Fatalf("provenance id = %q, want user_copy", in.AssumptionProfileID)
	}
}

// TestSnapshotRejectsUnrecognizedSystemContent covers td/067 R13 #4 / R14 #3: a
// system-owned profile whose content matches no recognized published identity must
// never run with forged provenance.
func TestSnapshotRejectsUnrecognizedSystemContent(t *testing.T) {
	p := assumptions.SystemDefaultProfile() // owner_scope=system
	p.StudentTDf = 4                        // diverges from the published v3 content
	resolved := resolvedAssumption{
		Profile: p, Mode: assumptions.SourceBlendedPrior, Scenario: assumptions.ScenarioBaseline,
	}
	plan, params := provenancePlanParams()
	_, err := buildInputSnapshotStruct(plan, params, 42, "cfg", nil, resolved)
	var ae *AppError
	if !errors.As(err, &ae) || ae.Code != "system_profile_identity_conflict" {
		t.Fatalf("unrecognized system content must be rejected, got %v", err)
	}
}
