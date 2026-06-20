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
// system_cma_v1@1 canonical JSON (commit 6954346). The static fixture in testdata
// is the byte-exact output of that release; pinning the hash here makes any drift
// in the fixture an immediate test failure (td/065 N7).
const realV1ReleaseContentHash = "6eecc14f7c8c8f812382e9cea88b7c056c18db8e6fd1a832961e63dd66f0971c"

// realV2ReleaseContentHash is the sha256 of the actual published td/064
// system_cma_v2@1 canonical JSON (commit d51a595, before the td/065 in-place edit
// that td/066 R12 forbids). The static fixture in testdata is the byte-exact
// output of that release; pinning it here makes the v2-frozen guarantee testable.
const realV2ReleaseContentHash = "3a1545466b5f40856706e66952a3cad26ef546a929e181949727b96dbd143698"

func loadFixture(t *testing.T, name string) (string, string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	sum := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(sum[:])
}

func insertSystemProfileRow(t *testing.T, db *sql.DB, id string, version int, name, canonical, hash string) {
	t.Helper()
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_profiles
		(id, version, owner_scope, name, status, canonical_json, content_hash,
		 source_note, reviewed_by, reviewed_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, version, assumptions.OwnerSystem, name, assumptions.StatusActive,
		canonical, hash, "historical release", "FIRE 投研团队", "2026-06-20", now, now); err != nil {
		t.Fatalf("insert system profile %s@%d: %v", id, version, err)
	}
}

// insertRealLegacyV1Row writes the real published v1 fixture into the DB exactly
// as a pre-td063 release would have.
func insertRealLegacyV1Row(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	canonical, hash := loadFixture(t, "system_cma_v1_canonical.json")
	if hash != realV1ReleaseContentHash {
		t.Fatalf("v1 fixture hash drift: got %s want %s", hash, realV1ReleaseContentHash)
	}
	insertSystemProfileRow(t, db, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion,
		"系统默认（CMA v1）", canonical, hash)
	return canonical, hash
}

// insertRealV2Row writes the real published td/064 v2 fixture into the DB exactly
// as a post-td064 (pre-td066) release would have.
func insertRealV2Row(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()
	canonical, hash := loadFixture(t, "system_cma_v2_canonical.json")
	if hash != realV2ReleaseContentHash {
		t.Fatalf("v2 fixture hash drift: got %s want %s", hash, realV2ReleaseContentHash)
	}
	insertSystemProfileRow(t, db, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version,
		"系统默认（CMA v2）", canonical, hash)
	return canonical, hash
}

func seedDefaultPreference(t *testing.T, db *sql.DB, id string, version int, scenario string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_preferences
		(id, default_profile_id, default_profile_version, default_scenario, updated_at)
		VALUES (1, ?, ?, ?, ?)`, id, version, scenario, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed preference: %v", err)
	}
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

// TestEnsureSystemDefaultUpgradesV2ToV3 covers td/066 R12 acceptance #1: a database
// holding the REAL published td/064 system_cma_v2@1 (alongside the frozen v1) must,
// after running the new code, keep v1 AND v2 byte-for-byte (matching their release
// hashes), gain an immutable system_cma_v3@1, and have a v2-pointing default
// preference atomically repointed to v3/baseline.
func TestEnsureSystemDefaultUpgradesV2ToV3(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	v1Canonical, v1Hash := insertRealLegacyV1Row(t, db)
	v2Canonical, v2Hash := insertRealV2Row(t, db)
	// A real upgraded DB has the default pointing at v2 (the td/064 migration target).
	seedDefaultPreference(t, db, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version, "baseline")

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	// v1 and v2 are byte-for-byte unchanged.
	if c, h := readProfileBytes(t, db, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion); c != v1Canonical || h != v1Hash {
		t.Fatalf("legacy v1 must be immutable: got hash %s want %s", h, v1Hash)
	}
	if c, h := readProfileBytes(t, db, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version); c != v2Canonical || h != v2Hash {
		t.Fatalf("v2 must be immutable: got hash %s want %s", h, v2Hash)
	}

	// v3 exists, is active, and matches the registry canonical hash.
	v3, err := repo.Get(ctx, assumptions.SystemProfileID, assumptions.SystemProfileVersion)
	if err != nil {
		t.Fatalf("v3 must exist: %v", err)
	}
	if v3.Status != assumptions.StatusActive {
		t.Fatalf("v3 must be active, got %q", v3.Status)
	}
	v3Hash, err := v3.ContentHash()
	if err != nil {
		t.Fatalf("v3 content hash: %v", err)
	}
	if v3Hash != assumptions.CurrentSystemIdentity().CanonicalHash {
		t.Fatalf("inserted v3 hash %s != registry %s", v3Hash, assumptions.CurrentSystemIdentity().CanonicalHash)
	}

	// Default preference migrated v2 -> v3/baseline.
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != assumptions.SystemProfileID ||
		pref.DefaultProfileVersion != assumptions.SystemProfileVersion ||
		pref.DefaultScenario != assumptions.ScenarioBaseline {
		t.Fatalf("preference not migrated to v3/baseline: %+v", pref)
	}

	// Historical replay: v2 (and v1) still load for pinned runs.
	if _, err := repo.Get(ctx, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version); err != nil {
		t.Fatalf("v2 must still load for replay: %v", err)
	}

	// Idempotent: a second run is a no-op and keeps v2 immutable.
	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	if c, _ := readProfileBytes(t, db, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version); c != v2Canonical {
		t.Fatal("v2 changed on a second upgrade run")
	}
}

// TestEnsureSystemDefaultLeavesV1DefaultUntouched covers td/066 R12: only the
// DIRECT predecessor (v2) is auto-migrated to v3. A database whose default still
// points at the non-direct predecessor v1 (i.e. it never ran td/064) is left
// untouched — v3 is published but the default is not silently rewritten.
func TestEnsureSystemDefaultLeavesV1DefaultUntouched(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	insertRealLegacyV1Row(t, db)
	seedDefaultPreference(t, db, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion, "baseline")

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if _, err := repo.Get(ctx, assumptions.SystemProfileID, assumptions.SystemProfileVersion); err != nil {
		t.Fatalf("v3 must be published: %v", err)
	}
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != assumptions.SystemLegacyProfileID ||
		pref.DefaultProfileVersion != assumptions.SystemLegacyProfileVersion {
		t.Fatalf("non-direct predecessor (v1) default must NOT be auto-migrated, got %+v", pref)
	}
}

// TestEnsureSystemDefaultKeepsCustomDefault covers td/064 R6 / td/066 R12: a user
// who already chose a custom global default must not be repointed to v3.
func TestEnsureSystemDefaultKeepsCustomDefault(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	seedDefaultPreference(t, db, "user_custom", 3, "conservative")

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

// TestEnsureSystemDefaultFreshInstallHasOnlyV3 covers td/066 R12: a brand-new
// database seeds only v3 (no legacy v1/v2) and resolves the default to v3.
func TestEnsureSystemDefaultFreshInstallHasOnlyV3(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	if _, err := repo.Get(ctx, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion); err == nil {
		t.Fatal("fresh install must not create legacy v1")
	}
	if _, err := repo.Get(ctx, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version); err == nil {
		t.Fatal("fresh install must not create legacy v2")
	}
	// A fresh-install v3 must hash identically to an upgraded-DB v3 (td/066 R12 #2):
	// both equal the registry-pinned canonical hash.
	v3, err := repo.Get(ctx, assumptions.SystemProfileID, assumptions.SystemProfileVersion)
	if err != nil {
		t.Fatalf("v3 must exist on fresh install: %v", err)
	}
	v3Hash, err := v3.ContentHash()
	if err != nil {
		t.Fatalf("v3 content hash: %v", err)
	}
	if v3Hash != assumptions.CurrentSystemIdentity().CanonicalHash {
		t.Fatalf("fresh v3 hash %s != registry %s", v3Hash, assumptions.CurrentSystemIdentity().CanonicalHash)
	}
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != assumptions.SystemProfileID ||
		pref.DefaultProfileVersion != assumptions.SystemProfileVersion {
		t.Fatalf("fresh default must resolve to v3, got %+v", pref)
	}
}
