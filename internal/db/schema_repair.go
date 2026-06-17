package db

import (
	"context"
	"database/sql"
	"fmt"
)

type schemaExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func repairSnapshotSchema(ctx context.Context, exec schemaExec) error {
	cols, err := tableColumns(ctx, exec, "instrument_simulation_snapshots")
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return nil
	}
	colSet := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		colSet[c] = struct{}{}
	}

	if _, ok := colSet["observation_count"]; ok {
		if _, err := exec.ExecContext(ctx, `
			ALTER TABLE instrument_simulation_snapshots
			RENAME COLUMN observation_count TO daily_observation_count`); err != nil {
			return fmt.Errorf("rename observation_count: %w", err)
		}
		delete(colSet, "observation_count")
		colSet["daily_observation_count"] = struct{}{}
	}

	type colAdd struct {
		name string
		ddl  string
	}
	for _, add := range []colAdd{
		{
			name: "monthly_return_count",
			ddl:  `ALTER TABLE instrument_simulation_snapshots ADD COLUMN monthly_return_count INTEGER NOT NULL DEFAULT 0`,
		},
		{
			name: "volatility_method",
			ddl:  `ALTER TABLE instrument_simulation_snapshots ADD COLUMN volatility_method TEXT NOT NULL DEFAULT 'monthly_log_return'`,
		},
		{
			name: "metrics_version",
			ddl:  `ALTER TABLE instrument_simulation_snapshots ADD COLUMN metrics_version TEXT NOT NULL DEFAULT 'monthly_log_return_v1'`,
		},
		{
			name: "history_depth",
			ddl:  `ALTER TABLE instrument_simulation_snapshots ADD COLUMN history_depth TEXT NOT NULL DEFAULT 'unknown'`,
		},
	} {
		if _, ok := colSet[add.name]; ok {
			continue
		}
		if _, err := exec.ExecContext(ctx, add.ddl); err != nil {
			return fmt.Errorf("add column %s: %w", add.name, err)
		}
		colSet[add.name] = struct{}{}
	}
	return nil
}

func tableColumns(ctx context.Context, exec schemaExec, table string) ([]string, error) {
	rows, err := exec.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, fmt.Errorf("pragma table_info %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan table_info: %w", err)
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table_info: %w", err)
	}
	return cols, nil
}
