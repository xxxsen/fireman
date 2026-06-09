package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAndPing(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fireman.db")

	pool, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	if err := Ping(context.Background(), pool); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestMigrate_AppliesInitialSchemaAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fireman.db")

	pool, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	if err := Migrate(ctx, pool, dbPath, nil); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	expectedTables := []string{
		"plans", "plan_parameters", "plan_cash_flows",
		"allocation_scenarios", "allocation_scenario_weights",
		"plan_asset_class_targets", "plan_region_targets",
		"instruments", "market_data_points", "instrument_annual_returns",
		"instrument_simulation_snapshots", "instrument_simulation_snapshot_years",
		"plan_holdings", "portfolio_snapshots", "portfolio_snapshot_items",
		"jobs", "simulation_runs", "simulation_path_index",
		"simulation_quantile_series", "analysis_results", "job_idempotency_keys",
	}
	for _, name := range expectedTables {
		var got string
		err := pool.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&got)
		if err != nil {
			t.Errorf("expected table %q to exist: %v", name, err)
		}
	}

	var idxName string
	if err := pool.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='index' AND name='idx_jobs_claim'").Scan(&idxName); err != nil {
		t.Errorf("expected idx_jobs_claim index: %v", err)
	}

	var scenarioCount int
	if err := pool.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM allocation_scenarios WHERE is_builtin=1").Scan(&scenarioCount); err != nil {
		t.Fatalf("count builtin scenarios: %v", err)
	}
	if scenarioCount != 4 {
		t.Errorf("expected 4 builtin scenarios, got %d", scenarioCount)
	}

	var systemCash string
	if err := pool.QueryRowContext(ctx,
		"SELECT id FROM instruments WHERE id='system_cash_cny' AND is_system=1").Scan(&systemCash); err != nil {
		t.Fatalf("expected system_cash_cny instrument: %v", err)
	}

	var snapID string
	var completeYearCount int
	var sourceMode string
	if err := pool.QueryRowContext(ctx,
		`SELECT id, complete_year_count, source_mode FROM instrument_simulation_snapshots
		 WHERE instrument_id='system_cash_cny'`).Scan(&snapID, &completeYearCount, &sourceMode); err != nil {
		t.Fatalf("expected system cash snapshot: %v", err)
	}
	if completeYearCount != 0 {
		t.Errorf("expected complete_year_count=0, got %d", completeYearCount)
	}
	if sourceMode != "system_cash" {
		t.Errorf("expected source_mode=system_cash, got %q", sourceMode)
	}

	var fxCount int
	if err := pool.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM instruments WHERE is_system=1 AND asset_class='fx' AND code IN ('USDCNY','HKDCNY')`,
	).Scan(&fxCount); err != nil {
		t.Fatalf("count system fx instruments: %v", err)
	}
	if fxCount != 2 {
		t.Errorf("expected 2 system FX instruments, got %d", fxCount)
	}

	if err := Migrate(ctx, pool, dbPath, nil); err != nil {
		t.Fatalf("second Migrate (idempotent run): %v", err)
	}

	var migrationCount int
	if err := pool.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if migrationCount != 3 {
		t.Errorf("expected 3 migration records after idempotent re-run, got %d", migrationCount)
	}
}

func TestMigrate_DoesNotBackupFreshDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fireman.db")
	pool, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	if err := Migrate(context.Background(), pool, dbPath, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bak") {
			t.Errorf("did not expect backup on fresh install, got %q", e.Name())
		}
	}
}
