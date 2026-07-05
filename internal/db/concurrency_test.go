package db

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrentWrites_NoBusyErrors exercises the single-writer pool: many
// goroutines inserting/updating concurrently must never observe SQLITE_BUSY.
func TestConcurrentWrites_NoBusyErrors(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")
	pool, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = pool.Close() }()

	if _, err := pool.ExecContext(ctx, `
		CREATE TABLE sync_state (
			scope      TEXT PRIMARY KEY,
			last_task  TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	const goroutines = 16
	const writesPerGoroutine = 30
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*writesPerGoroutine)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ {
				scope := fmt.Sprintf("scope_%d", i%4)
				_, err := pool.ExecContext(ctx, `
					INSERT INTO sync_state (scope, last_task, updated_at) VALUES (?,?,?)
					ON CONFLICT(scope) DO UPDATE SET
						last_task=excluded.last_task,
						updated_at=excluded.updated_at`,
					scope, fmt.Sprintf("task_%d_%d", g, i), time.Now().UnixMilli())
				if err != nil {
					errCh <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if strings.Contains(err.Error(), "SQLITE_BUSY") ||
			strings.Contains(err.Error(), "database is locked") {
			t.Fatalf("concurrent write hit busy error: %v", err)
		}
		t.Fatalf("concurrent write failed: %v", err)
	}
}
