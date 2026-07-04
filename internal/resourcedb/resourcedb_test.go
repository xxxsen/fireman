package resourcedb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(context.Background(), filepath.Join(dir, "resource.db"))
	if err != nil {
		t.Fatalf("open resource db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInsertContent_RoundTripAndIdempotency(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	raw := []byte(`{"type":"asset_directory_sync","assets":[]}`)
	payload, err := GzipBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	env, err := db.InsertContent(ctx, "application/json", "gzip", 1, payload, now, time.Hour)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	sum := sha256.Sum256(payload)
	wantKey := hex.EncodeToString(sum[:])
	if env.ResourceKey != wantKey {
		t.Fatalf("resource key = %s, want payload sha256 %s", env.ResourceKey, wantKey)
	}
	if env.SHA256 != wantKey || env.SizeBytes != int64(len(payload)) {
		t.Fatalf("envelope mismatch: %+v", env)
	}

	got, err := db.Read(ctx, env)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("read payload = %q, want %q", got, raw)
	}

	// Idempotent retry: same content re-uploaded later refreshes the TTL and
	// yields the identical key.
	env2, err := db.InsertContent(ctx, "application/json", "gzip", 1, payload, now.Add(time.Hour), time.Hour)
	if err != nil {
		t.Fatalf("re-insert: %v", err)
	}
	if env2.ResourceKey != env.ResourceKey {
		t.Fatalf("re-insert changed key: %s vs %s", env2.ResourceKey, env.ResourceKey)
	}
	if env2.ExpiresAt <= env.ExpiresAt {
		t.Fatalf("re-insert did not refresh TTL: %d <= %d", env2.ExpiresAt, env.ExpiresAt)
	}

	var count int
	if err := db.Pool().QueryRow(`SELECT COUNT(*) FROM resource_tab`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected a single content-addressed row, got %d", count)
	}
}

func TestRead_ValidatesEnvelope(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	payload, err := GzipBytes([]byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	env, err := db.InsertContent(ctx, "application/json", "gzip", 1, payload, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	missing := env
	missing.ResourceKey = "does-not-exist"
	if _, err := db.Read(ctx, missing); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("missing resource error = %v, want ErrResourceNotFound", err)
	}

	badSum := env
	badSum.SHA256 = "deadbeef"
	if _, err := db.Read(ctx, badSum); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("checksum error = %v, want ErrChecksumMismatch", err)
	}

	badSize := env
	badSize.SizeBytes = env.SizeBytes + 1
	if _, err := db.Read(ctx, badSize); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("size error = %v, want ErrSizeMismatch", err)
	}

	badSchema := env
	badSchema.SchemaVersion = 99
	if _, err := db.Read(ctx, badSchema); !errors.Is(err, ErrSchemaVersion) {
		t.Fatalf("schema error = %v, want ErrSchemaVersion", err)
	}

	badEncoding := env
	badEncoding.ContentEncoding = "zstd"
	if _, err := db.Read(ctx, badEncoding); !errors.Is(err, ErrUnsupportedEncoing) {
		t.Fatalf("encoding error = %v, want ErrUnsupportedEncoing", err)
	}
}

func TestRead_PlainEncoding(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	raw := []byte(`plain payload`)
	env, err := db.InsertContent(ctx, "application/json", "", 1, raw, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.Read(ctx, env)
	if err != nil {
		t.Fatalf("read plain: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatalf("plain payload roundtrip mismatch: %q", got)
	}
}

func TestDeleteExpired(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now()

	expired, err := db.InsertContent(ctx, "application/json", "", 1, []byte("old"), now.Add(-2*time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := db.InsertContent(ctx, "application/json", "", 1, []byte("new"), now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	n, err := db.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted %d rows, want 1", n)
	}
	if _, err := db.Read(ctx, expired); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expired resource still readable: %v", err)
	}
	if _, err := db.Read(ctx, fresh); err != nil {
		t.Fatalf("fresh resource unreadable: %v", err)
	}
}
