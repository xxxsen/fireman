package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func openTempMigratedDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fireman.db")
	pool, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := Migrate(context.Background(), pool, path, nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return pool, path
}

func TestValidateDatabaseFile(t *testing.T) {
	db, path := openTempMigratedDB(t)
	if err := ValidateDatabaseFile(path); err != nil {
		t.Fatalf("valid db: %v", err)
	}
	_ = db

	badPath := filepath.Join(filepath.Dir(path), "bad.db")
	if err := os.WriteFile(badPath, []byte("not-sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDatabaseFile(badPath); err == nil {
		t.Fatal("expected invalid backup to fail validation")
	}
}

func TestReadDatabaseFile(t *testing.T) {
	db, path := openTempMigratedDB(t)
	data, err := ReadDatabaseFile(context.Background(), db, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 16 {
		t.Fatal("backup too small")
	}
	if string(data[:15]) != "SQLite format 3" {
		t.Fatalf("unexpected header: %q", string(data[:15]))
	}
}
