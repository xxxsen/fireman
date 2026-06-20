package repository_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

// legacyV1Profile reconstructs the original td/061/062 system_cma_v1@1 content
// (CNY-only priors, cash correlation cells, no tail truncation). It deliberately
// does NOT satisfy the current Validate (cash correlations + zero truncation), so
// it is inserted via raw SQL to simulate a pre-td063/td064 database.
func legacyV1Profile() assumptions.Profile {
	eqD := assumptions.AssetFactorKey("equity", "domestic")
	eqF := assumptions.AssetFactorKey("equity", "foreign")
	bdD := assumptions.AssetFactorKey("bond", "domestic")
	bdF := assumptions.AssetFactorKey("bond", "foreign")
	cash := assumptions.AssetFactorKey("cash", "domestic")
	fx := assumptions.FXFactorKey("USD", "CNY")
	rp := func(class, region string, ret float64) assumptions.ReturnPrior {
		return assumptions.ReturnPrior{
			AssetClass: class, Region: region, ValuationCurrency: "CNY",
			AnnualGeometricReturn: ret, AnnualVolatilityFloor: 0.02, AnnualVolatilityCeiling: 0.40,
			SourceURL:   "https://github.com/fireman/fireman/tree/master/td#td-061",
			PublishedAt: "2026-06-20", ReviewedAt: "2026-06-20",
		}
	}
	return assumptions.Profile{
		ID: assumptions.SystemLegacyProfileID, Version: assumptions.SystemLegacyProfileVersion,
		OwnerScope: assumptions.OwnerSystem, Name: "系统默认（CMA v1）", Status: assumptions.StatusActive,
		PriorStrengthYears: 20, CorrelationStrengthMonths: 36, StudentTDf: 7,
		Scenarios: map[string]assumptions.Scenario{
			assumptions.ScenarioConservative: {ReturnShiftLog: -0.015, VolatilityMultiplier: 1.15},
			assumptions.ScenarioBaseline:     {VolatilityMultiplier: 1.00},
			assumptions.ScenarioOptimistic:   {ReturnShiftLog: 0.015, VolatilityMultiplier: 0.90},
		},
		ReturnPriors: []assumptions.ReturnPrior{
			rp("equity", "domestic", 0.060), rp("equity", "foreign", 0.065),
			rp("bond", "domestic", 0.030), rp("bond", "foreign", 0.030),
			rp("cash", "domestic", 0.018),
		},
		FXPriors: []assumptions.FXPrior{{
			FromCurrency: "USD", BaseCurrency: "CNY", AnnualVolatilityFloor: 0.03, AnnualVolatilityCeiling: 0.12,
			SourceURL:   "https://github.com/fireman/fireman/tree/master/td#td-061",
			PublishedAt: "2026-06-20", ReviewedAt: "2026-06-20",
		}},
		CorrelationPriors: []assumptions.CorrelationPrior{
			{FactorA: eqD, FactorB: eqF, Rho: 0.60},
			{FactorA: cash, FactorB: fx, Rho: 0},
			{FactorA: bdD, FactorB: bdF, Rho: 0.40},
		},
	}
}

func insertLegacyProfileRow(t *testing.T, db *sql.DB, p assumptions.Profile) (string, string) {
	t.Helper()
	cb, err := p.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	h, err := p.ContentHash()
	if err != nil {
		t.Fatalf("content hash: %v", err)
	}
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_profiles
		(id, version, owner_scope, name, status, canonical_json, content_hash,
		 source_note, reviewed_by, reviewed_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Version, p.OwnerScope, p.Name, p.Status, string(cb), h,
		"legacy seed", "legacy", "2026-06-20", now, now); err != nil {
		t.Fatalf("insert legacy v1 row: %v", err)
	}
	return string(cb), h
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

// TestEnsureSystemDefaultUpgradesV1ToV2 covers td/064 R6: a database holding the
// legacy system_cma_v1@1 must, after running the new code, keep v1 byte-for-byte,
// gain an immutable system_cma_v2@1, and have a v1-pointing default preference
// atomically repointed to v2/baseline.
func TestEnsureSystemDefaultUpgradesV1ToV2(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := repository.NewAssumptionProfileRepo(db)

	wantCanonical, wantHash := insertLegacyProfileRow(t, db, legacyV1Profile())
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
