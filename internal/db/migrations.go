package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// migrationsFS is populated by the parent package via SetMigrations to inject
// the embedded SQL files. Keeping the embed directive in the migrations
// package (co-located with the SQL files) allows internal/db to remain
// agnostic about the on-disk location of the migration files.
var migrationsFS fs.FS

// SetMigrations registers the embedded migrations FS that Migrate consumes.
// The provided FS must expose the *.sql files at its root.
func SetMigrations(f fs.FS) {
	migrationsFS = f
}

// backupRetention is the number of timestamped backups to retain.
const backupRetention = 5

var (
	errMigrationNameFormat       = errors.New("migration filename must start with NNNN_")
	errDuplicateMigrationVersion = errors.New("duplicate migration version")
)

// Migrate applies all pending SQL migrations from the embedded FS to the
// database. Before applying any migration it creates a timestamped backup of
// the database file (when it exists and is non-empty) and prunes older
// backups beyond the retention window.
func Migrate(ctx context.Context, pool *sql.DB, dbPath string, logger *slog.Logger) error {
	if migrationsFS == nil {
		return errMigrationsNotRegistered
	}
	if logger == nil {
		logger = slog.Default()
	}

	files, err := listMigrationFiles()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	priorAppliedCount, err := schemaMigrationsRowCount(ctx, pool)
	if err != nil {
		return err
	}

	if _, err := pool.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		filename TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("db: ensure schema_migrations: %w", err)
	}

	pending, err := pendingMigrations(ctx, pool, files)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	// Only back up an already-populated database. A brand new install whose
	// schema_migrations table was just created (or did not exist at all) has
	// no user data worth preserving and would otherwise leave a stale
	// "empty" backup behind on first boot.
	if priorAppliedCount > 0 {
		if err := backupDatabase(dbPath, logger); err != nil {
			return fmt.Errorf("db: backup before migration: %w", err)
		}
	}

	for _, m := range pending {
		body, err := fs.ReadFile(migrationsFS, m.filename)
		if err != nil {
			return fmt.Errorf("db: read migration %s: %w", m.filename, err)
		}
		if err := applyMigration(ctx, pool, m, body); err != nil {
			return err
		}
		logger.Info("applied migration", "version", m.version, "filename", m.filename)
	}

	if err := repairSnapshotSchema(ctx, pool); err != nil {
		return fmt.Errorf("db: repair snapshot schema: %w", err)
	}

	return nil
}

type migrationFile struct {
	version  int
	filename string
}

func listMigrationFiles() ([]migrationFile, error) {
	entries, err := fs.ReadDir(migrationsFS, ".")
	if err != nil {
		return nil, fmt.Errorf("db: read migrations dir: %w", err)
	}
	var out []migrationFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		idx := strings.IndexByte(name, '_')
		if idx <= 0 {
			return nil, fmt.Errorf("db: migration %q: %w", name, errMigrationNameFormat)
		}
		v, err := strconv.Atoi(name[:idx])
		if err != nil {
			return nil, fmt.Errorf("db: migration %q has non-numeric prefix: %w", name, err)
		}
		out = append(out, migrationFile{version: v, filename: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("db: duplicate migration version %d: %w", out[i].version, errDuplicateMigrationVersion)
		}
	}
	return out, nil
}

func schemaMigrationsRowCount(ctx context.Context, pool *sql.DB) (int, error) {
	var exists int
	if err := pool.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`,
	).Scan(&exists); err != nil {
		return 0, fmt.Errorf("db: probe schema_migrations: %w", err)
	}
	if exists == 0 {
		return 0, nil
	}
	var n int
	if err := pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		return 0, fmt.Errorf("db: count schema_migrations: %w", err)
	}
	return n, nil
}

func pendingMigrations(ctx context.Context, pool *sql.DB, files []migrationFile) ([]migrationFile, error) {
	rows, err := pool.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("db: read schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	applied := make(map[int]struct{})
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema migration version: %w", err)
		}
		applied[v] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema migrations: %w", err)
	}
	var pending []migrationFile
	for _, f := range files {
		if _, ok := applied[f.version]; ok {
			continue
		}
		pending = append(pending, f)
	}
	return pending, nil
}

func applyMigration(ctx context.Context, pool *sql.DB, m migrationFile, body []byte) error {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin tx for %s: %w", m.filename, err)
	}
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("db: execute migration %s: %w", m.filename, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, filename, applied_at) VALUES (?, ?, ?)`,
		m.version, m.filename, time.Now().UnixMilli()); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("db: record migration %s: %w", m.filename, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration %s: %w", m.filename, err)
	}
	return nil
}

func backupDatabase(dbPath string, logger *slog.Logger) error {
	info, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat database file: %w", err)
	}
	if info.Size() == 0 {
		return nil
	}

	dir := filepath.Dir(dbPath)
	base := filepath.Base(dbPath)
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	backupName := fmt.Sprintf("%s.%s.bak", base, timestamp)
	backupPath := filepath.Join(dir, backupName)

	if err := copyFile(dbPath, backupPath); err != nil {
		return err
	}
	logger.Info("created database backup", "backup", backupPath)
	return pruneBackups(dir, base)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open destination file: %w", err)
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy database file: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync database backup: %w", err)
	}
	return nil
}

func pruneBackups(dir, base string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read backup directory: %w", err)
	}
	prefix := base + "."
	suffix := ".bak"
	var backups []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		backups = append(backups, name)
	}
	sort.Strings(backups)
	if len(backups) <= backupRetention {
		return nil
	}
	for _, name := range backups[:len(backups)-backupRetention] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("remove old backup: %w", err)
		}
	}
	return nil
}
