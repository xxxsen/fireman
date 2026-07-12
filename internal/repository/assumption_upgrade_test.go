package repository_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

// realV1ReleaseContentHash is the sha256 of the actual published
// system_cma_v1@1 canonical JSON (commit 6954346). The static fixture in testdata
// is the byte-exact output of that release; pinning the hash here makes any drift
// in the fixture an immediate test failure.
const realV1ReleaseContentHash = "6eecc14f7c8c8f812382e9cea88b7c056c18db8e6fd1a832961e63dd66f0971c"

// realV2ReleaseContentHash is the sha256 of the actual published
// system_cma_v2@1 canonical JSON (commit d51a595, before the in-place edit
// that the immutability rule forbids). The static fixture in testdata is the byte-exact
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
// as an older release would have.
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

// insertRealV2Row writes the real published v2 fixture into the DB exactly
// as an intermediate profile-upgrade release would have.
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

// insertProfileRowRaw inserts a single profile row with explicit owner_scope and
// status, mimicking a row written by an older release or a hijacking client.
func insertProfileRowRaw(
	t *testing.T, db *sql.DB, p assumptions.Profile, ownerScope string,
) (string, string) {
	t.Helper()
	canonical, err := p.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	hash, err := p.ContentHash()
	if err != nil {
		t.Fatalf("content hash: %v", err)
	}
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_profiles
		(id, version, owner_scope, name, status, canonical_json, content_hash,
		 source_note, reviewed_by, reviewed_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Version, ownerScope, p.Name, assumptions.StatusActive, string(canonical), hash,
		"seed", "tester", "2026-06-20", now, now); err != nil {
		t.Fatalf("insert profile %s@%d: %v", p.ID, p.Version, err)
	}
	return string(canonical), hash
}

// seedPlanWithPinnedProfile inserts a minimal plan + plan_parameters that pins a
// specific assumption profile id@version, so a pin-repoint can be asserted.
func seedPlanWithPinnedProfile(t *testing.T, db *sql.DB, planID, pinID string, pinVersion int) {
	t.Helper()
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO plans (id, name, base_currency, valuation_date, created_at, updated_at)
		VALUES (?,?,?,?,?,?)`, planID, "test plan", "CNY", "2026-06-20", now, now); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO plan_parameters
		(plan_id, current_age, retirement_age, end_age, total_assets_minor,
		 annual_savings_minor, annual_spending_minor, inflation_mode,
		 assumption_selection_mode, return_assumption_set_id, return_assumption_set_version,
		 updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		planID, 30, 55, 90, 1000000, 0, 100000, "fixed",
		"pinned_profile", pinID, pinVersion, now); err != nil {
		t.Fatalf("insert plan_parameters: %v", err)
	}
}

func readPlanPin(t *testing.T, db *sql.DB, planID string) (string, int) {
	t.Helper()
	var id string
	var version int
	if err := db.QueryRow(
		`SELECT return_assumption_set_id, return_assumption_set_version
		 FROM plan_parameters WHERE plan_id=?`, planID).Scan(&id, &version); err != nil {
		t.Fatalf("read plan pin: %v", err)
	}
	return id, version
}

func seedDefaultPreference(t *testing.T, db *sql.DB, id string, version int, scenario string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_preferences
		(id, default_profile_id, default_profile_version, default_scenario, updated_at)
		VALUES (1, ?, ?, ?, ?)`, id, version, scenario, time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed preference: %v", err)
	}
}

// readProfileBytes reads the canonical JSON and stored content hash for version 1
// of a profile (all system identities in these upgrade tests are single-version).
func readProfileBytes(t *testing.T, db *sql.DB, id string) (string, string) {
	t.Helper()
	const version = 1
	var canonical, hash string
	if err := db.QueryRow(
		`SELECT canonical_json, content_hash FROM simulation_assumption_profiles WHERE id=? AND version=?`,
		id, version,
	).Scan(&canonical, &hash); err != nil {
		t.Fatalf("read profile %s@%d: %v", id, version, err)
	}
	return canonical, hash
}

// TestEnsureSystemDefaultUpgradesV2ToV3 covers the v2-to-v3 upgrade: a database
// holding the REAL published system_cma_v2@1 (alongside the frozen v1) must,
// after running the new code, keep v1 AND v2 byte-for-byte (matching their release
// hashes), gain an immutable system_cma_v3@1, and have a v2-pointing default
// preference atomically repointed to v3/baseline.
func TestEnsureSystemDefaultUpgradesV2ToV3(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	v1Canonical, v1Hash := insertRealLegacyV1Row(t, db)
	v2Canonical, v2Hash := insertRealV2Row(t, db)
	// A real upgraded DB has the default pointing at v2 (the migration target).
	seedDefaultPreference(t, db, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version, "baseline")

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	// v1 and v2 are byte-for-byte unchanged.
	if c, h := readProfileBytes(t, db, assumptions.SystemLegacyProfileID); c != v1Canonical || h != v1Hash {
		t.Fatalf("legacy v1 must be immutable: got hash %s want %s", h, v1Hash)
	}
	if c, h := readProfileBytes(t, db, assumptions.SystemProfileV2ID); c != v2Canonical || h != v2Hash {
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
	if c, _ := readProfileBytes(t, db, assumptions.SystemProfileV2ID); c != v2Canonical {
		t.Fatal("v2 changed on a second upgrade run")
	}
}

// TestEnsureSystemDefaultLeavesV1DefaultUntouched verifies that only the
// DIRECT predecessor (v2) is auto-migrated to v3. A database whose default still
// points at the non-direct predecessor v1 (i.e. it never ran the v2 migration) is left
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

// TestEnsureSystemDefaultKeepsCustomDefault verifies that a user
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

// TestEnsureSystemDefaultFreshInstallHasOnlyV3 verifies that a brand-new
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
	// A fresh-install v3 must hash identically to an upgraded-DB v3:
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

// TestEnsureSystemDefaultRepairsUserSystemSquatter covers the reserved-namespace repair:
// a database where a user profile illegally occupies system_cma_v3@1 must, after
// the upgrade, migrate that user profile to a deterministic user_legacy_<hash> id
// (repointing the plan pin and the global default to it), and publish the REAL
// system v3 with the registry-pinned content.
func TestEnsureSystemDefaultRepairsUserSystemSquatter(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	// A user-owned profile squatting on the reserved v3 identity (distinct content).
	squat := assumptions.SystemDefaultProfile()
	squat.OwnerScope = assumptions.OwnerUser
	squat.Name = "我的劫持模型"
	squat.StudentTDf = 9 // makes the content differ from the real v3
	_, squatHash := insertProfileRowRaw(t, db, squat, assumptions.OwnerUser)
	wantLegacyID := "user_legacy_" + squatHash[:16]

	// A plan pins it, and it is the global default.
	seedPlanWithPinnedProfile(t, db, "plan_hijack", assumptions.SystemProfileID, assumptions.SystemProfileVersion)
	seedDefaultPreference(t, db, assumptions.SystemProfileID, assumptions.SystemProfileVersion, "conservative")

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("upgrade with squatter: %v", err)
	}

	// The migrated user profile exists under the deterministic legacy id, owner=user.
	migrated, err := repo.Get(ctx, wantLegacyID, 1)
	if err != nil {
		t.Fatalf("migrated user profile %s must exist: %v", wantLegacyID, err)
	}
	if migrated.OwnerScope != assumptions.OwnerUser || migrated.StudentTDf != 9 {
		t.Fatalf("migrated profile must keep user content, got owner=%s df=%d",
			migrated.OwnerScope, migrated.StudentTDf)
	}

	// The plan pin and the global default both resolve to the migrated user profile.
	if id, ver := readPlanPin(t, db, "plan_hijack"); id != wantLegacyID || ver != 1 {
		t.Fatalf("plan pin must repoint to %s@1, got %s@%d", wantLegacyID, id, ver)
	}
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != wantLegacyID || pref.DefaultProfileVersion != 1 {
		t.Fatalf("default must repoint to migrated profile %s@1, got %+v", wantLegacyID, pref)
	}

	// The real system v3 is now published with the registry-pinned content.
	v3, err := repo.Get(ctx, assumptions.SystemProfileID, assumptions.SystemProfileVersion)
	if err != nil {
		t.Fatalf("real system v3 must exist: %v", err)
	}
	if v3.OwnerScope != assumptions.OwnerSystem {
		t.Fatalf("published v3 must be system-owned, got %s", v3.OwnerScope)
	}
	v3Hash, err := v3.ContentHash()
	if err != nil {
		t.Fatalf("v3 content hash: %v", err)
	}
	if v3Hash != assumptions.CurrentSystemIdentity().CanonicalHash {
		t.Fatalf("published v3 hash %s != registry %s", v3Hash, assumptions.CurrentSystemIdentity().CanonicalHash)
	}

	// Idempotent: re-running keeps everything stable.
	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
}

// TestEnsureSystemDefaultRejectsTamperedSystemV3 covers the tamper guard:
// an owner_scope=system v3 row whose content does not match the registry canonical
// hash is refused with a conflict and is never overwritten.
func TestEnsureSystemDefaultRejectsTamperedSystemV3(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	tampered := assumptions.SystemDefaultProfile()
	tampered.StudentTDf = 4 // diverges from the published v3 content
	canonicalBefore, _ := insertProfileRowRaw(t, db, tampered, assumptions.OwnerSystem)

	err := repo.EnsureSystemDefault(ctx)
	if !errors.Is(err, repository.ErrSystemProfileIdentityConflict) {
		t.Fatalf("tampered v3 must yield identity conflict, got %v", err)
	}
	// The on-disk row is untouched.
	if c, _ := readProfileBytes(t, db, assumptions.SystemProfileID); c != canonicalBefore {
		t.Fatal("tampered v3 row must not be overwritten")
	}
}

// TestEnsureSystemDefaultUpgradesV2EvidenceVariant covers the v2-variant upgrade: a
// database initialized by the affected build holds the v2 variant content; the
// upgrade must accept it (recognized variant), keep it byte-for-byte, publish v3,
// and migrate the v2-pointing default to v3.
func TestEnsureSystemDefaultUpgradesV2EvidenceVariant(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	canonical, hash := loadFixture(t, "system_cma_v2_evidence_variant_canonical.json")
	insertSystemProfileRow(t, db, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version,
		"系统默认（CMA v2 证据变体）", canonical, hash)
	seedDefaultPreference(t, db, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version, "baseline")

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("upgrade v2 evidence variant: %v", err)
	}
	// v2 variant is byte-for-byte unchanged.
	if c, h := readProfileBytes(t, db, assumptions.SystemProfileV2ID); c != canonical || h != hash {
		t.Fatalf("v2 evidence variant must be immutable: got hash %s want %s", h, hash)
	}
	// v3 published, default migrated.
	if _, err := repo.Get(ctx, assumptions.SystemProfileID, assumptions.SystemProfileVersion); err != nil {
		t.Fatalf("v3 must be published: %v", err)
	}
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != assumptions.SystemProfileID {
		t.Fatalf("default must migrate to v3, got %+v", pref)
	}
}

// TestEnsureSystemDefaultRejectsUnknownSystemV2 covers the unknown-content guard: an
// owner_scope=system v2 row whose content matches no recognized published variant
// is refused with a conflict (its meaning is never guessed).
func TestEnsureSystemDefaultRejectsUnknownSystemV2(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	// A fabricated v2 (v3 priors under the v2 id) is NOT a recognized v2 content.
	fake := assumptions.SystemDefaultProfile()
	fake.ID = assumptions.SystemProfileV2ID
	fake.Version = assumptions.SystemProfileV2Version
	fake.Name = "伪造 v2"
	insertProfileRowRaw(t, db, fake, assumptions.OwnerSystem)

	err := repo.EnsureSystemDefault(ctx)
	if !errors.Is(err, repository.ErrSystemProfileIdentityConflict) {
		t.Fatalf("unknown system v2 must yield identity conflict, got %v", err)
	}
}

// countReservedUserProfiles returns how many owner_scope=user rows occupy the
// reserved system_cma_ namespace.
func countReservedUserProfiles(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(1) FROM simulation_assumption_profiles
		 WHERE owner_scope='user' AND id LIKE 'system\_cma\_%' ESCAPE '\'`).Scan(&n); err != nil {
		t.Fatalf("count reserved user profiles: %v", err)
	}
	return n
}

// TestEnsureSystemDefaultRepairsUserV2OnPublishedV3DB covers the startup
// audit repair (cases #1 and #2): an upgraded database already holds a correct system v3, yet a
// user illegally created an active owner_scope=user system_cma_v2@1 and made it the
// global default + a plan pin. The previous fast path returned as soon as v3 was
// valid and skipped repair; the tightened path must still migrate the squatter to
// user_legacy_<hash>, repoint the pin and default, keep v3 byte-for-byte, and be
// idempotent with no reserved user rows left.
func TestEnsureSystemDefaultRepairsUserV2OnPublishedV3DB(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	// Correctly published system v3 registry content.
	v3CanonicalBefore, v3HashBefore := insertProfileRowRaw(
		t, db, assumptions.SystemDefaultProfile(), assumptions.OwnerSystem)

	// A user squats on system_cma_v2@1 with distinct content and makes it default + pin.
	squat := assumptions.SystemDefaultProfile()
	squat.ID = assumptions.SystemProfileV2ID
	squat.Version = assumptions.SystemProfileV2Version
	squat.OwnerScope = assumptions.OwnerUser
	squat.Name = "我的伪v2"
	squat.StudentTDf = 8 // distinct from registry content
	_, squatHash := insertProfileRowRaw(t, db, squat, assumptions.OwnerUser)
	wantLegacyID := "user_legacy_" + squatHash[:16]

	seedPlanWithPinnedProfile(t, db, "plan_v2pin", assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version)
	seedDefaultPreference(t, db, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version, "conservative")

	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("upgrade with published v3 + user v2 squatter: %v", err)
	}

	// Squatter migrated to deterministic legacy id, keeping user content.
	migrated, err := repo.Get(ctx, wantLegacyID, 1)
	if err != nil {
		t.Fatalf("migrated user profile %s must exist: %v", wantLegacyID, err)
	}
	if migrated.OwnerScope != assumptions.OwnerUser || migrated.StudentTDf != 8 {
		t.Fatalf("migrated profile must keep user content, got owner=%s df=%d",
			migrated.OwnerScope, migrated.StudentTDf)
	}
	// The original reserved user v2 row is gone.
	if _, err := repo.Get(ctx, assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version); err == nil {
		t.Fatal("reserved user system_cma_v2@1 row must be removed after migration")
	}
	// Pin and default both repoint to the migrated user profile (not v3).
	if id, ver := readPlanPin(t, db, "plan_v2pin"); id != wantLegacyID || ver != 1 {
		t.Fatalf("plan pin must repoint to %s@1, got %s@%d", wantLegacyID, id, ver)
	}
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences: %v", err)
	}
	if pref.DefaultProfileID != wantLegacyID || pref.DefaultProfileVersion != 1 {
		t.Fatalf("default must repoint to %s@1, got %+v", wantLegacyID, pref)
	}
	// Real v3 is untouched byte-for-byte.
	if c, h := readProfileBytes(t, db, assumptions.SystemProfileID); c != v3CanonicalBefore || h != v3HashBefore {
		t.Fatalf("system v3 must be immutable: hash %s want %s", h, v3HashBefore)
	}

	// Idempotent: a second run changes nothing and leaves no reserved user rows.
	if err := repo.EnsureSystemDefault(ctx); err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	if n := countReservedUserProfiles(t, db); n != 0 {
		t.Fatalf("no owner=user system_cma_ rows may remain, got %d", n)
	}
	pref2, err := repo.GetPreferences(ctx)
	if err != nil {
		t.Fatalf("get preferences after second run: %v", err)
	}
	if pref2.DefaultProfileID != wantLegacyID {
		t.Fatalf("default must stay on %s after idempotent run, got %+v", wantLegacyID, pref2)
	}
}

// TestEnsureSystemDefaultRejectsUnknownSystemRowsOnPublishedV3DB covers the startup
// audit rejection case: a database with a correct v3 plus an UNKNOWN owner_scope=system v2
// (and unknown system v1) must fail integrity at the very first EnsureSystemDefault
// call (not lazily when later pinned), and must not overwrite the existing rows.
func TestEnsureSystemDefaultRejectsUnknownSystemRowsOnPublishedV3DB(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	v3CanonicalBefore, _ := insertProfileRowRaw(
		t, db, assumptions.SystemDefaultProfile(), assumptions.OwnerSystem)

	// Unknown owner=system v2 (fabricated content) AND unknown owner=system v1.
	fakeV2 := assumptions.SystemDefaultProfile()
	fakeV2.ID = assumptions.SystemProfileV2ID
	fakeV2.Name = "未知系统v2"
	fakeV2.StudentTDf = 5
	insertProfileRowRaw(t, db, fakeV2, assumptions.OwnerSystem)

	fakeV1 := assumptions.SystemDefaultProfile()
	fakeV1.ID = assumptions.SystemLegacyProfileID
	fakeV1.Name = "未知系统v1"
	fakeV1.StudentTDf = 6
	insertProfileRowRaw(t, db, fakeV1, assumptions.OwnerSystem)

	if err := repo.EnsureSystemDefault(ctx); !errors.Is(err, repository.ErrSystemProfileIdentityConflict) {
		t.Fatalf("unknown system rows must yield identity conflict on first call, got %v", err)
	}
	// v3 is not overwritten.
	if c, _ := readProfileBytes(t, db, assumptions.SystemProfileID); c != v3CanonicalBefore {
		t.Fatal("v3 must not be overwritten when a sibling system row is unknown")
	}
}
