package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	errIntegrityCheckFailed = errors.New("integrity check failed")
	errBackupEmpty          = errors.New("db: backup file is empty")
)

// CheckpointWAL forces a WAL checkpoint so backup files are consistent.
func CheckpointWAL(ctx context.Context, pool *sql.DB) error {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db: checkpoint conn: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "PRAGMA wal_checkpoint(FULL)"); err != nil {
		return fmt.Errorf("db: wal_checkpoint: %w", err)
	}
	return nil
}

// ValidateDatabaseFile checks integrity and migration schema on a SQLite file.
func ValidateDatabaseFile(ctx context.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("db: resolve path: %w", err)
	}
	pool, err := sql.Open("sqlite", buildDSN(abs))
	if err != nil {
		return fmt.Errorf("db: open validate: %w", err)
	}
	defer func() { _ = pool.Close() }()

	var integrity string
	if err := pool.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("db: integrity_check: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("db: integrity_check failed: %s: %w", integrity, errIntegrityCheckFailed)
	}

	var count int
	if err := pool.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`,
	).Scan(&count); err != nil {
		return fmt.Errorf("db: schema table: %w", err)
	}
	if count == 0 {
		return errMissingSchemaMigrations
	}
	files, err := listMigrationFiles()
	if err != nil {
		return err
	}
	return validateMigratedSchema(ctx, pool, files, path)
}

// ReadDatabaseFile returns the raw SQLite file bytes after optional checkpoint.
func ReadDatabaseFile(ctx context.Context, pool *sql.DB, dbPath string) ([]byte, error) {
	if err := CheckpointWAL(ctx, pool); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return nil, fmt.Errorf("db: read backup file: %w", err)
	}
	if len(data) == 0 {
		return nil, errBackupEmpty
	}
	return data, nil
}
