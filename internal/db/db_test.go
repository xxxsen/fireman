package db

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fireman/fireman/migrations"
)

func TestMain(m *testing.M) {
	SetMigrations(migrations.FS)
	os.Exit(m.Run())
}

func applyMigrationsThrough(t *testing.T, pool *sql.DB, _ string, maxVersion int) {
	t.Helper()
	SetMigrations(migrations.FS)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		filename TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		t.Fatalf("ensure schema_migrations: %v", err)
	}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	type mig struct {
		version int
		name    string
	}
	var files []mig
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		idx := strings.IndexByte(e.Name(), '_')
		v, err := strconv.Atoi(e.Name()[:idx])
		if err != nil {
			t.Fatalf("parse version %s: %v", e.Name(), err)
		}
		if v > maxVersion {
			continue
		}
		files = append(files, mig{version: v, name: e.Name()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	for _, f := range files {
		body, err := fs.ReadFile(migrations.FS, f.name)
		if err != nil {
			t.Fatalf("read %s: %v", f.name, err)
		}
		if err := applyMigration(ctx, pool, migrationFile{version: f.version, filename: f.name}, body); err != nil {
			t.Fatalf("apply %s: %v", f.name, err)
		}
	}
}

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
		"plans", "plan_parameters",
		"allocation_scenarios", "allocation_scenario_weights",
		"plan_asset_class_targets", "plan_region_targets",
		"instruments", "market_data_points",
		"market_assets", "market_asset_points", "market_asset_history_state",
		"market_asset_simulation_snapshots", "market_asset_simulation_snapshot_years",
		"market_asset_simulation_snapshot_months",
		"plan_holdings", "portfolio_snapshots", "portfolio_snapshot_items",
		"jobs", "simulation_runs", "simulation_path_index",
		"simulation_quantile_series", "simulation_real_quantile_series",
		"plan_return_assumption_overrides",
		"analysis_results", "job_idempotency_keys",
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
		"SELECT asset_key FROM market_assets WHERE asset_key='SYS|cash||CNY'").Scan(&systemCash); err != nil {
		t.Fatalf("expected SYS|cash||CNY market asset: %v", err)
	}

	var snapID string
	var completeYearCount int
	var sourceMode string
	if err := pool.QueryRowContext(ctx,
		`SELECT id, complete_year_count, source_mode FROM market_asset_simulation_snapshots
		 WHERE asset_key='SYS|cash||CNY'`).Scan(&snapID, &completeYearCount, &sourceMode); err != nil {
		t.Fatalf("expected system cash snapshot: %v", err)
	}
	if completeYearCount != 0 {
		t.Errorf("expected complete_year_count=0, got %d", completeYearCount)
	}
	if sourceMode != "system_cash" {
		t.Errorf("expected source_mode=system_cash, got %q", sourceMode)
	}

	var fxCount int
	if err := pool.QueryRowContext(
		ctx,
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
	if migrationCount != 21 {
		t.Errorf("expected 21 migration records after idempotent re-run, got %d", migrationCount)
	}
}

func TestMigrate_0004To0005_DeduplicatesDuplicateInstrumentFetch(t *testing.T) {
	testMigrate0004To0005Deduplicates(t, int64(1_700_000_000_000), int64(1_700_000_000_000+1000))
}

func TestMigrate_0004To0005_DeduplicatesSameTimestampDuplicateInstrumentFetch(t *testing.T) {
	sameTs := int64(1_700_000_000_000)
	testMigrate0004To0005Deduplicates(t, sameTs, sameTs)
}

func testMigrate0004To0005Deduplicates(t *testing.T, createdAtFirst, createdAtSecond int64) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fireman.db")
	pool, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	applyMigrationsThrough(t, pool, dbPath, 4)

	inputHash := "dup_hash_0004_upgrade"
	_, err = pool.ExecContext(context.Background(), `
		INSERT INTO jobs (
			id, type, status, input_hash, payload_json,
			progress_current, progress_total, phase,
			cancel_requested, retry_count, created_at
		) VALUES
		('job_dup_q', 'instrument_fetch', 'queued', ?, '{"instrument_id":"ins_dup"}', 0, 1, 'queued', 0, 0, ?),
		('job_dup_r', 'instrument_fetch', 'running', ?, '{"instrument_id":"ins_dup2"}', 0, 1, 'fetching_history', 0, 0, ?)
	`, inputHash, createdAtFirst, inputHash, createdAtSecond)
	if err != nil {
		t.Fatalf("seed duplicate jobs: %v", err)
	}

	if err := Migrate(context.Background(), pool, dbPath, nil); err != nil {
		t.Fatalf("migrate to 0005: %v", err)
	}

	var activeCount int
	if err := pool.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM jobs
		WHERE type='instrument_fetch' AND input_hash=? AND status IN ('queued', 'running')`,
		inputHash).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 1 {
		t.Fatalf("active duplicate jobs=%d want 1", activeCount)
	}

	var keptID string
	if err := pool.QueryRowContext(context.Background(), `
		SELECT id FROM jobs
		WHERE type='instrument_fetch' AND input_hash=? AND status IN ('queued', 'running')`,
		inputHash).Scan(&keptID); err != nil {
		t.Fatal(err)
	}
	if createdAtFirst < createdAtSecond {
		if keptID != "job_dup_q" {
			t.Fatalf("kept job=%q want job_dup_q", keptID)
		}
	} else if createdAtFirst == createdAtSecond {
		if keptID != "job_dup_q" {
			t.Fatalf("same timestamp kept job=%q want job_dup_q (id ASC)", keptID)
		}
	}

	var canceledCount int
	if err := pool.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM jobs
		WHERE type='instrument_fetch' AND input_hash=? AND status='canceled'
		  AND error_code='duplicate_instrument_fetch_migrated' AND finished_at IS NOT NULL`,
		inputHash).Scan(&canceledCount); err != nil {
		t.Fatal(err)
	}
	if canceledCount != 1 {
		t.Fatalf("canceled migrated jobs=%d want 1", canceledCount)
	}

	var idxName string
	if err := pool.QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='index' AND name='uq_jobs_instrument_fetch_active'`).Scan(&idxName); err != nil {
		t.Fatalf("expected unique index: %v", err)
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
