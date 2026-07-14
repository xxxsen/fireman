package db

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/fireman/fireman/migrations"
)

func TestMain(m *testing.M) {
	SetMigrations(migrations.FS)
	os.Exit(m.Run())
}

func TestValidateMigrationDDLRejectsDataStatements(t *testing.T) {
	for _, statement := range []string{
		"INSERT INTO example VALUES (1);",
		"UPDATE example SET value=1;",
		"DELETE FROM example;",
		"REPLACE INTO example VALUES (1);",
		"SELECT * FROM example;",
	} {
		if err := validateMigrationDDL("test.sql", statement); !errors.Is(err, errMigrationNotDDL) {
			t.Fatalf("statement %q error=%v, want errMigrationNotDDL", statement, err)
		}
	}
	if err := validateMigrationDDL(
		"test.sql",
		"CREATE TABLE child (id INTEGER, parent_id INTEGER REFERENCES parent(id) ON DELETE CASCADE);",
	); err != nil {
		t.Fatalf("valid DDL rejected: %v", err)
	}
}

func TestMigrationsAreOrderedAndDDLOnly(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	var sqlFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			sqlFiles = append(sqlFiles, entry.Name())
		}
	}
	want := []string{"0001_init.sql", "0002_single_asset_investment_path.sql"}
	if strings.Join(sqlFiles, ",") != strings.Join(want, ",") {
		t.Fatalf("migration files=%v, want %v", sqlFiles, want)
	}
	dml := regexp.MustCompile(`(?im)^\s*(insert|update|delete|replace|merge)\b`)
	for _, name := range sqlFiles {
		body, readErr := fs.ReadFile(migrations.FS, name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if match := dml.Find(body); match != nil {
			t.Fatalf("migration %s contains prohibited DML statement %q", name, match)
		}
		if name == "0001_init.sql" {
			historicalDDL := regexp.MustCompile(`(?im)^\s*(alter|drop)\b`)
			if match := historicalDDL.Find(body); match != nil {
				t.Fatalf("consolidated baseline contains historical DDL statement %q", match)
			}
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
		"worker_tasks", "worker_task_versions", "worker_task_attempts",
		"simulation_runs", "simulation_path_index",
		"simulation_quantile_series", "simulation_real_quantile_series",
		"plan_return_assumption_overrides",
		"analysis_results", "worker_task_idempotency_keys",
		"fire_frontier_runs", "fire_frontier_applications",
		"research_investment_path_runs", "research_investment_path_points",
		"research_investment_path_trades", "research_investment_path_windows",
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
		"SELECT name FROM sqlite_master WHERE type='index' AND name='idx_worker_tasks_claim'").Scan(&idxName); err != nil {
		t.Errorf("expected idx_worker_tasks_claim index: %v", err)
	}

	var businessRows int
	if err := pool.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM allocation_scenarios) +
		(SELECT COUNT(*) FROM instruments) +
		(SELECT COUNT(*) FROM market_assets) +
		(SELECT COUNT(*) FROM market_asset_simulation_snapshots)`).Scan(&businessRows); err != nil {
		t.Fatalf("count migration-created business rows: %v", err)
	}
	if businessRows != 0 {
		t.Fatalf("DDL migration created %d business rows", businessRows)
	}

	var foreignKeyViolations int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_foreign_key_check").
		Scan(&foreignKeyViolations); err != nil {
		t.Fatalf("run foreign key check: %v", err)
	}
	if foreignKeyViolations != 0 {
		t.Fatalf("foreign key check returned %d rows", foreignKeyViolations)
	}

	var confidenceDefault, horizonDefault string
	if err := pool.QueryRowContext(
		ctx,
		"SELECT dflt_value FROM pragma_table_info('research_collections') WHERE name='tail_risk_confidence'",
	).Scan(&confidenceDefault); err != nil {
		t.Fatalf("read tail_risk_confidence migration: %v", err)
	}
	if err := pool.QueryRowContext(
		ctx,
		"SELECT dflt_value FROM pragma_table_info('research_collections') WHERE name='tail_risk_horizon_days'",
	).Scan(&horizonDefault); err != nil {
		t.Fatalf("read tail_risk_horizon_days migration: %v", err)
	}
	if confidenceDefault != "0.95" || horizonDefault != "20" {
		t.Fatalf("unexpected CVaR defaults: confidence=%q horizon=%q", confidenceDefault, horizonDefault)
	}

	if err := Migrate(ctx, pool, dbPath, nil); err != nil {
		t.Fatalf("second Migrate (idempotent run): %v", err)
	}

	var migrationCount int
	if err := pool.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if migrationCount != 2 {
		t.Errorf("expected 2 migration records after idempotent re-run, got %d", migrationCount)
	}
}

func TestMigrateRejectsDatabaseWhoseRecordedBaselineHasDrifted(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fireman.db")
	pool, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := context.Background()
	if err := Migrate(ctx, pool, dbPath, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, `DROP TABLE fire_frontier_applications`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, `DROP TABLE fire_frontier_runs`); err != nil {
		t.Fatal(err)
	}
	err = Migrate(ctx, pool, dbPath, nil)
	if !errors.Is(err, errSchemaDrift) || !strings.Contains(err.Error(), "fire_frontier_runs (missing)") ||
		!strings.Contains(err.Error(), "rebuild this development database") {
		t.Fatalf("schema drift error=%v", err)
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

func TestMigrateUpgradesExistingBaselineToInvestmentPathSchema(t *testing.T) {
	baseline, err := fs.ReadFile(migrations.FS, "0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	SetMigrations(fstest.MapFS{"0001_init.sql": &fstest.MapFile{Data: baseline}})
	t.Cleanup(func() { SetMigrations(migrations.FS) })
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fireman.db")
	pool, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := Migrate(context.Background(), pool, dbPath, nil); err != nil {
		t.Fatal(err)
	}
	SetMigrations(migrations.FS)
	if err := Migrate(context.Background(), pool, dbPath, nil); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"research_investment_path_runs", "research_investment_path_points",
		"research_investment_path_trades", "research_investment_path_windows",
	} {
		var name string
		if err := pool.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("upgraded table %s: %v", table, err)
		}
	}
}
