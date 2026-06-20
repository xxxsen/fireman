package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

// Fixed regression inputs. Any engine, profile, calibration, sampling or
// serialization change that moves these numbers must be reviewed and the pinned
// constants below updated DELIBERATELY (td/067 R15).
const (
	regressionSeed = "424242"
	regressionRuns = 1000
)

// Locked terminal P50 (minor units) for the fixed 50-year forward fixture under
// each frozen assumption identity. These are the byte-stable outputs of the real
// create/run/read flow; a change here signals a model/engine shift, not just a
// numeric drift to silently re-baseline.
const (
	wantV3TerminalP50 = 577080841
	// v1 and v2 share the 0.06 equity/domestic/CNY prior for this single-asset
	// fixture, so their terminal P50 coincides; the v3 geometric prior (0.0608)
	// produces a strictly different headline, proving the formula is applied per
	// pinned identity rather than bleeding across runs.
	wantV1TerminalP50 = 527399522
	wantV2TerminalP50 = 527399522
)

type regressionResult struct {
	terminalP50 int64
	inputHash   string
	snap        simulation.InputSnapshot
}

// TestForwardP50RegressionE2E covers td/067 R15: a fixed plan / holdings snapshot /
// 50-year horizon / seed / runs is driven through the REAL create→run→read flow.
// It locks the terminal P50 and asserts the four provenance fields persisted in
// input_snapshot_json against the registry, proving runs are reproducible and that
// historical v1/v2 pins keep their own frozen content (never the v3 formula).
func TestForwardP50RegressionE2E(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()

	// Seed the real frozen v1/v2 system contents so the control pins resolve to
	// genuine historical identities (recognized by the content registry).
	seedSystemFixtureRow(t, db, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion,
		"系统默认（CMA v1）", "system_cma_v1_canonical.json")
	seedSystemFixtureRow(t, db, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version,
		"系统默认（CMA v2）", "system_cma_v2_canonical.json")

	planID := seedSimulationReadyPlan(t, db)
	configureRegressionPlan(t, db, planID)

	services := buildServices(db, "")
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	simRepo := repository.NewSimulationRepo(db)

	// --- v3 default run (follow_global) ---
	setPlanAssumption(t, db, planID, "follow_global", "", 0)
	run1 := runRegression(ctx, t, services, runner, simRepo, planID)
	run2 := runRegression(ctx, t, services, runner, simRepo, planID)

	t.Logf("v3 P50=%d inputHash=%s", run1.terminalP50, run1.inputHash)

	// Reproducibility: an identical fixture run twice is byte-stable.
	if run1.inputHash != run2.inputHash {
		t.Fatalf("input hash not reproducible: %s vs %s", run1.inputHash, run2.inputHash)
	}
	if run1.terminalP50 != run2.terminalP50 {
		t.Fatalf("terminal P50 not reproducible: %d vs %d", run1.terminalP50, run2.terminalP50)
	}
	// Locked headline value.
	if run1.terminalP50 != wantV3TerminalP50 {
		t.Fatalf("v3 terminal P50 = %d, want locked %d (update only with a reviewed model change)",
			run1.terminalP50, wantV3TerminalP50)
	}
	// Four provenance fields exactly match the registry for the current identity.
	cur := assumptions.CurrentSystemIdentity()
	assertProvenance(t, run1.snap, assumptions.SystemProfileID, assumptions.SystemProfileVersion,
		cur.CanonicalHash, cur.EvidenceHash)
	if run1.snap.AssumptionEvidenceHash != assumptions.CMAEvidenceContentHash {
		t.Fatalf("v3 evidence hash %q != live artifact %q",
			run1.snap.AssumptionEvidenceHash, assumptions.CMAEvidenceContentHash)
	}

	// --- v1 / v2 historical pin controls ---
	setPlanAssumption(t, db, planID, "pinned_profile", assumptions.SystemLegacyProfileID, 1)
	runV1 := runRegression(ctx, t, services, runner, simRepo, planID)
	setPlanAssumption(t, db, planID, "pinned_profile", assumptions.SystemProfileV2ID, 1)
	runV2 := runRegression(ctx, t, services, runner, simRepo, planID)

	t.Logf("v1 P50=%d  v2 P50=%d", runV1.terminalP50, runV2.terminalP50)

	v1Hash := registryHash(t, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion)
	v2Hash := registryHash(t, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version)
	// Each pin records its OWN frozen content hash (never v3's) and carries no
	// evidence hash (neither v1 nor the TD064 v2 has a backing CMA artifact).
	assertProvenance(t, runV1.snap, assumptions.SystemLegacyProfileID, 1, v1Hash, "")
	assertProvenance(t, runV2.snap, assumptions.SystemProfileV2ID, 1, v2Hash, "")
	if runV1.snap.AssumptionProfileContentHash == run1.snap.AssumptionProfileContentHash {
		t.Fatal("v1 pin must use v1 content, not v3")
	}
	if runV2.snap.AssumptionProfileContentHash == run1.snap.AssumptionProfileContentHash {
		t.Fatal("v2 pin must use v2 content, not v3")
	}
	// The v3 geometric formula must change the headline vs the legacy contents.
	if run1.terminalP50 == runV1.terminalP50 {
		t.Fatalf("v3 P50 %d must differ from the v1 historical pin %d", run1.terminalP50, runV1.terminalP50)
	}
	// Locked control values.
	if runV1.terminalP50 != wantV1TerminalP50 {
		t.Fatalf("v1 pin terminal P50 = %d, want locked %d", runV1.terminalP50, wantV1TerminalP50)
	}
	if runV2.terminalP50 != wantV2TerminalP50 {
		t.Fatalf("v2 pin terminal P50 = %d, want locked %d", runV2.terminalP50, wantV2TerminalP50)
	}
}

func assertProvenance(
	t *testing.T, snap simulation.InputSnapshot, id string, version int, canonicalHash, evidenceHash string,
) {
	t.Helper()
	if snap.AssumptionProfileID != id || snap.AssumptionProfileVersion != version {
		t.Fatalf("provenance identity = %s@%d, want %s@%d",
			snap.AssumptionProfileID, snap.AssumptionProfileVersion, id, version)
	}
	if snap.AssumptionProfileContentHash != canonicalHash {
		t.Fatalf("provenance content hash = %q, want %q", snap.AssumptionProfileContentHash, canonicalHash)
	}
	if snap.AssumptionEvidenceHash != evidenceHash {
		t.Fatalf("provenance evidence hash = %q, want %q", snap.AssumptionEvidenceHash, evidenceHash)
	}
}

func registryHash(t *testing.T, id string, version int) string {
	t.Helper()
	e, ok := assumptions.LookupSystemIdentity(id, version)
	if !ok {
		t.Fatalf("no registry identity for %s@%d", id, version)
	}
	return e.CanonicalHash
}

func runRegression(
	ctx context.Context, t *testing.T, svc Services,
	runner *jobs.SimulationRunner, simRepo *repository.SimulationRepo, planID string,
) regressionResult {
	t.Helper()
	runs := regressionRuns
	seed := regressionSeed
	resp, err := svc.Simulations.Create(ctx, service.CreateSimulationRequest{
		PlanID: planID, Runs: &runs, Seed: &seed,
	})
	if err != nil {
		t.Fatalf("create simulation: %v", err)
	}
	run, err := simRepo.GetByID(ctx, resp.RunID)
	if err != nil {
		t.Fatalf("get pending run: %v", err)
	}
	var snap simulation.InputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snap); err != nil {
		t.Fatalf("decode input snapshot: %v", err)
	}
	if err := runner.RunSimulation(ctx, run.JobID, run.ID, &snap,
		func() bool { return false }, func(int, int, string) {}); err != nil {
		t.Fatalf("run simulation: %v", err)
	}
	done, err := simRepo.GetByID(ctx, resp.RunID)
	if err != nil {
		t.Fatalf("get completed run: %v", err)
	}
	var sum struct {
		TerminalQuantiles map[string]int64 `json:"terminal_quantiles"`
	}
	if err := json.Unmarshal(done.SummaryJSON, &sum); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	return regressionResult{
		terminalP50: sum.TerminalQuantiles["p50"],
		inputHash:   run.InputHash,
		snap:        snap,
	}
}

// configureRegressionPlan pins every horizon/cash-flow parameter so the only thing
// that varies across the runs is the resolved assumption identity.
func configureRegressionPlan(t *testing.T, db *sql.DB, planID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		UPDATE plan_parameters SET
			current_age=30, retirement_age=30, end_age=80,
			annual_savings_minor=0, annual_savings_growth_rate=0,
			annual_spending_minor=?, terminal_wealth_floor_minor=0,
			inflation_mode='fixed_real', fixed_inflation_rate=0.03,
			withdrawal_type='fixed_real', withdrawal_rate=0.03,
			withdrawal_floor_ratio=0.7, withdrawal_ceiling_ratio=1.3,
			rebalance_frequency='annual', rebalance_threshold=0.03,
			transaction_cost_rate=0, student_t_df=7,
			return_assumption_mode='blended_prior', return_assumption_scenario='baseline'
		WHERE plan_id=?`, 30_000_00, planID); err != nil {
		t.Fatalf("configure regression plan: %v", err)
	}
}

func setPlanAssumption(t *testing.T, db *sql.DB, planID, mode, pinID string, pinVersion int) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		UPDATE plan_parameters SET assumption_selection_mode=?,
			return_assumption_set_id=?, return_assumption_set_version=?
		WHERE plan_id=?`, mode, pinID, pinVersion, planID); err != nil {
		t.Fatalf("set plan assumption: %v", err)
	}
}

func seedSystemFixtureRow(t *testing.T, db *sql.DB, id string, version int, name, fixtureFile string) {
	t.Helper()
	raw, err := os.ReadFile("../repository/testdata/" + fixtureFile)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixtureFile, err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_profiles
		(id, version, owner_scope, name, status, canonical_json, content_hash,
		 source_note, reviewed_by, reviewed_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, version, "system", name, "active", string(raw), hash,
		"historical release", "FIRE", "2026-06-20", now, now); err != nil {
		t.Fatalf("seed system fixture %s@%d: %v", id, version, err)
	}
}
