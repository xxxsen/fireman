package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	fdb "github.com/fireman/fireman/internal/db"
)

func mustDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	pool, err := fdb.Open(context.Background(), filepath.Join(dir, "fireman.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

func TestHealthz_OK(t *testing.T) {
	pool := mustDB(t)
	r := NewRouter(context.Background(), Deps{DB: pool})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestHealthz_DBUnavailable(t *testing.T) {
	pool := mustDB(t)
	_ = pool.Close()
	r := NewRouter(context.Background(), Deps{DB: pool})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body=%s)", w.Code, w.Body.String())
	}
}
