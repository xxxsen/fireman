package repository_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

// realV1ReleaseContentHash is the sha256 of the actual published td/061/062
// system_cma_v1@1 canonical JSON (commit 6954346, before the td/063 in-place edit
// at 7cbb4c7). The static fixture in testdata is the byte-exact output of that
// release; pinning the hash here makes any drift in the fixture an immediate test
// failure (td/065 N7).
const realV1ReleaseContentHash = "6eecc14f7c8c8f812382e9cea88b7c056c18db8e6fd1a832961e63dd66f0971c"

// realPublishedV1 loads the static, byte-exact v1 canonical JSON fixture and its
// sha256. The fixture is the real released content (no tail truncation keys, cash
// correlation cells), not a simplified reconstruction.
func realPublishedV1(t *testing.T) (string, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/system_cma_v1_canonical.json")
	if err != nil {
		t.Fatalf("read v1 fixture: %v", err)
	}
	sum := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(sum[:])
}

// insertRealLegacyV1Row writes the real published v1 fixture into the DB exactly
// as a pre-td063/td064 release would have, simulating an upgraded database.
func insertRealLegacyV1Row(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	canonical, hash := realPublishedV1(t)
	if hash != realV1ReleaseContentHash {
		t.Fatalf("v1 fixture hash drift: got %s want %s", hash, realV1ReleaseContentHash)
	}
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_profiles
		(id, version, owner_scope, name, status, canonical_json, content_hash,
		 source_note, reviewed_by, reviewed_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion,
		assumptions.OwnerSystem, "系统默认（CMA v1）", assumptions.StatusActive,
		canonical, hash, "td061 seed", "FIRE 投研团队", "2026-06-20", now, now); err != nil {
		t.Fatalf("insert real legacy v1 row: %v", err)
	}
	return canonical, hash
}

func readProfileBytes(t *testing.T, db *sql.DB, id string, version int) (string, string) {
	t.Helper()
	var canonical, hash string
	if err := db.QueryRow(
		`SELECT canonical_json, content_hash FROM simulation_assumption_profiles WHERE id=? AND version=?`,
		id, version,
	).Scan(&canonical, &hash); err != nil {
		t.Fatalf("read profile %s@%d: %v", id, version, err)
	}
	return canonical, hash
}

// TestEnsureSystemDefaultUpgradesV1ToV2 covers td/064 R6 + td/065 N7: a database
// holding the REAL published legacy system_cma_v1@1 must, after running the new
// code, keep v1 byte-for-byte (matching the real release hash), gain an immutable
// system_cma_v2@1, and have a v1-pointing default preference atomically repointed
// to v2/baseline.
func TestEnsureSystemDefaultUpgradesV1ToV2(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	wantCanonical, wantHash := insertRealLegacyV1Row(t, db)
	if wantHash != realV1ReleaseContentHash {
		t.Fatalf("fixture must match real v1 release hash: got %s", wantHash)
	}
	// A default preference that still points at the legacy system identity.
	if _, err := db.Exec(`INSERT INTO simulation_assumption_preferences
		(id, default_profile_id, default_profile_version, default_scenario, updated_at)
		VALUES (1, ?, ?, 'baseline', ?)`,
		assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed v1 preference: %v", err)
	}

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	// v1 is byte-for-byte unchanged.
	gotCanonical, gotHash := readProfileBytes(t, db, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion)
	if gotCanonical != wantCanonical || gotHash != wantHash {
		t.Fatalf("legacy v1 must be immutable:\n want hash %s\n got  hash %s", wantHash, gotHash)
	}

	// v2 exists and is active.
	v2, err := repo.Get(ctx, assumptions.SystemProfileID, assumptions.SystemProfileVersion)
	if err != nil {
		t.Fatalf("v2 must exist: %v", err)
	}
	if v2.Status != assumptions.StatusActive {
		t.Fatalf("v2 must be active, got %q", v2.Status)
	}

	// Default preference migrated to v2/baseline.
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != assumptions.SystemProfileID ||
		pref.DefaultProfileVersion != assumptions.SystemProfileVersion ||
		pref.DefaultScenario != assumptions.ScenarioBaseline {
		t.Fatalf("preference not migrated to v2/baseline: %+v", pref)
	}

	// A historical CNY run that pinned v1 must still load and replay: the v1
	// profile parses under the current schema with its frozen identity, priors
	// and currency intact, so an old run referencing v1 content can be rebuilt.
	v1, err := repo.Get(ctx, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion)
	if err != nil {
		t.Fatalf("legacy v1 must still load for replay: %v", err)
	}
	if v1.ID != assumptions.SystemLegacyProfileID || v1.Version != assumptions.SystemLegacyProfileVersion {
		t.Fatalf("v1 identity changed: %s@%d", v1.ID, v1.Version)
	}
	if len(v1.ReturnPriors) != 5 || len(v1.FXPriors) != 1 {
		t.Fatalf("v1 priors lost on load: %d return / %d fx", len(v1.ReturnPriors), len(v1.FXPriors))
	}

	// Idempotent: a second run is a no-op and keeps v1 immutable.
	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	gotCanonical2, _ := readProfileBytes(t, db, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion)
	if gotCanonical2 != wantCanonical {
		t.Fatal("legacy v1 changed on a second upgrade run")
	}
}

// TestEnsureSystemDefaultKeepsCustomDefault covers td/064 R6: a user who already
// chose a custom global default must not be repointed to v2 by the upgrade.
func TestEnsureSystemDefaultKeepsCustomDefault(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	if _, err := db.Exec(`INSERT INTO simulation_assumption_preferences
		(id, default_profile_id, default_profile_version, default_scenario, updated_at)
		VALUES (1, 'user_custom', 3, 'conservative', ?)`, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed custom preference: %v", err)
	}

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != "user_custom" || pref.DefaultProfileVersion != 3 ||
		pref.DefaultScenario != "conservative" {
		t.Fatalf("custom default must be untouched, got %+v", pref)
	}
}

// TestEnsureSystemDefaultFreshInstallHasOnlyV2 covers td/064 R6: a brand-new
// database seeds only v2 (no legacy v1) and resolves the default to v2.
func TestEnsureSystemDefaultFreshInstallHasOnlyV2(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	if _, err := repo.Get(ctx, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion); err == nil {
		t.Fatal("fresh install must not create legacy v1")
	}
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != assumptions.SystemProfileID {
		t.Fatalf("fresh default must resolve to v2, got %+v", pref)
	}
}
