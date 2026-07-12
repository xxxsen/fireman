package bootstrap_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/fireman/fireman/internal/bootstrap"
	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/migrations"
)

func TestEnsureBuiltinDataIsSeparateFromDDLAndIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fireman.db")
	db, err := fdb.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fdb.SetMigrations(migrations.FS)
	if err := fdb.Migrate(ctx, db, path, nil); err != nil {
		t.Fatal(err)
	}

	assertCount(t, db, "SELECT COUNT(*) FROM allocation_scenarios", "allocation_scenarios", 0)
	assertCount(t, db, "SELECT COUNT(*) FROM market_assets", "market_assets", 0)
	assertCount(t, db, "SELECT COUNT(*) FROM market_asset_simulation_snapshots", "cash snapshots", 0)

	for range 2 {
		if err := bootstrap.EnsureBuiltinData(ctx, db); err != nil {
			t.Fatal(err)
		}
	}

	assertCount(t, db, "SELECT COUNT(*) FROM allocation_scenarios", "allocation_scenarios", 4)
	assertCount(t, db, "SELECT COUNT(*) FROM allocation_scenario_weights", "scenario weights", 12)
	assertCount(t, db, "SELECT COUNT(*) FROM allocation_scenario_region_targets", "scenario regions", 24)
	assertCount(t, db, "SELECT COUNT(*) FROM instruments", "instruments", 3)
	assertCount(t, db, "SELECT COUNT(*) FROM market_assets", "market_assets", 3)
	assertCount(t, db, "SELECT COUNT(*) FROM market_asset_simulation_snapshots", "cash snapshots", 3)

	var equityWeight, bondWeight float64
	if err := db.QueryRowContext(ctx, `SELECT
		(SELECT weight FROM allocation_scenario_weights
		 WHERE scenario_id='scn_builtin_near_fire' AND asset_class='equity'),
		(SELECT weight FROM allocation_scenario_weights
		 WHERE scenario_id='scn_builtin_near_fire' AND asset_class='bond')`).Scan(
		&equityWeight, &bondWeight,
	); err != nil {
		t.Fatal(err)
	}
	if equityWeight != 0.70 || bondWeight != 0.30 {
		t.Fatalf("near-FIRE weights=(%v,%v), want (0.70,0.30)", equityWeight, bondWeight)
	}

	var sourceMode, adjustPolicy string
	if err := db.QueryRowContext(ctx, `SELECT source_mode, adjust_policy
		FROM market_asset_simulation_snapshots
		WHERE id='sim_snapshot_system_cash_cny'`).Scan(&sourceMode, &adjustPolicy); err != nil {
		t.Fatal(err)
	}
	if sourceMode != "system_cash" || adjustPolicy != "none" {
		t.Fatalf("cash snapshot source=%q adjust=%q", sourceMode, adjustPolicy)
	}

	var invalidForeignKeys int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(
		&invalidForeignKeys,
	); err != nil {
		t.Fatal(err)
	}
	if invalidForeignKeys != 0 {
		t.Fatalf("builtin data has %d foreign-key violations", invalidForeignKeys)
	}
}

func assertCount(t *testing.T, db *sql.DB, query, label string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(context.Background(), query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count=%d, want %d", label, got, want)
	}
}
