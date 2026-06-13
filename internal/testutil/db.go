package testutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/migrations"
)

// OpenTestDBPath creates a migrated temporary SQLite database and returns its path.
func OpenTestDBPath(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fireman.db")
	pool, err := fdb.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	fdb.SetMigrations(migrations.FS)
	if err := fdb.Migrate(context.Background(), pool, path, nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return pool, path
}

// OpenTestDB creates a migrated temporary SQLite database.
func OpenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := OpenTestDBPath(t)
	return db
}
