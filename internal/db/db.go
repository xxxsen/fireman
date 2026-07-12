package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // SQLite driver registered for sql.Open("sqlite", ...)
)

var (
	errSelectOneUnexpected     = errors.New("db: select 1 returned unexpected value")
	errEmptyDatabasePath       = errors.New("db: empty database path")
	errMissingSchemaMigrations = errors.New("db: missing schema_migrations table")
	errMigrationsNotRegistered = errors.New("db: migrations filesystem not registered")
)

// pragmaStatements lists the PRAGMA statements that MUST be applied to every
// pooled SQLite connection.
var pragmaStatements = []string{
	"PRAGMA journal_mode = WAL",
	"PRAGMA foreign_keys = ON",
	"PRAGMA busy_timeout = 30000",
	"PRAGMA synchronous = NORMAL",
	"PRAGMA temp_store = MEMORY",
}

// Open returns a *sql.DB pointing at the given SQLite file. Required PRAGMAs
// are applied to every newly created connection via a connector hook.
func Open(ctx context.Context, dbPath string) (*sql.DB, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, errEmptyDatabasePath
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("db: resolve path: %w", err)
	}

	dsn := buildDSN(abs)

	pool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open sqlite: %w", err)
	}

	// Single writer connection: SQLite serializes writers anyway, and one
	// pooled connection removes SQLITE_BUSY races between Go-side writers
	// (directory finalization vs. task creation vs. sync-state updates).
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)

	if err := applyPragmas(ctx, pool); err != nil {
		_ = pool.Close()
		return nil, err
	}

	if err := Ping(ctx, pool); err != nil {
		_ = pool.Close()
		return nil, err
	}

	return pool, nil
}

// Ping verifies that the database can answer "SELECT 1". It is used both at
// startup and by the /healthz endpoint.
func Ping(ctx context.Context, pool *sql.DB) error {
	var one int
	if err := pool.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("db: select 1 failed: %w", err)
	}
	if one != 1 {
		return fmt.Errorf("%w: %d", errSelectOneUnexpected, one)
	}
	return nil
}

// applyPragmas opens a fresh connection for each pragma so we know the PRAGMAs
// stick on at least one live connection. Per-connection enforcement is handled
// by the modernc driver via the DSN parameters built in buildDSN.
func applyPragmas(ctx context.Context, pool *sql.DB) error {
	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db: acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	for _, stmt := range pragmaStatements {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db: apply %q: %w", stmt, err)
		}
	}
	return nil
}

// buildDSN encodes the PRAGMAs in the DSN so that every new connection
// established by the modernc driver has them applied at handshake time.
func buildDSN(path string) string {
	q := url.Values{}
	q.Set("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "busy_timeout(30000)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "temp_store(MEMORY)")

	return fmt.Sprintf("file:%s?%s", path, q.Encode())
}
