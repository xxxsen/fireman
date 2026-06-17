package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fireman/fireman/migrations"
)

func TestRepairSnapshotSchemaFromLegacyColumns(t *testing.T) {
	SetMigrations(migrations.FS)
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "legacy.db")
	pool, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err := pool.ExecContext(ctx, `
		CREATE TABLE instrument_simulation_snapshots (
			id TEXT PRIMARY KEY,
			instrument_id TEXT NOT NULL,
			plan_id TEXT,
			inclusion_date TEXT NOT NULL,
			as_of_date TEXT NOT NULL,
			window_start TEXT,
			window_end TEXT,
			complete_year_start INTEGER,
			complete_year_end INTEGER,
			complete_year_count INTEGER NOT NULL,
			observation_count INTEGER NOT NULL,
			historical_cagr REAL NOT NULL,
			modeled_annual_return REAL NOT NULL,
			annual_volatility REAL NOT NULL,
			max_drawdown REAL NOT NULL,
			expense_ratio REAL,
			expense_ratio_status TEXT NOT NULL,
			fee_treatment TEXT NOT NULL,
			source_mode TEXT NOT NULL,
			quality_status TEXT NOT NULL,
			warnings_json TEXT NOT NULL DEFAULT '[]',
			source_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}

	if err := repairSnapshotSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cols, err := tableColumns(ctx, pool, "instrument_simulation_snapshots")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"daily_observation_count": true,
		"monthly_return_count":    true,
		"volatility_method":       true,
		"metrics_version":         true,
		"history_depth":           true,
	}
	for _, c := range cols {
		delete(want, c)
	}
	for missing := range want {
		t.Fatalf("missing column after repair: %s", missing)
	}
}
