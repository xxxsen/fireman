// Package resourcedb manages the standalone SQLite resource database used to
// exchange large task result payloads between the sidecar worker and the Go
// backend. The database is owned exclusively by Go: the sidecar uploads
// payloads through the internal HTTP API and only ever holds resource keys,
// never a database handle. The resource database is intentionally
// task-agnostic: it stores opaque blobs keyed by resource_key (the payload
// sha256) and never records task ids, task types or version numbers.
package resourcedb

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
)

// MaxDecompressedBytes caps the decompressed payload size accepted from the
// resource database. Larger payloads are rejected before any business write.
const MaxDecompressedBytes = 256 << 20 // 256 MiB

// Schema errors surfaced to the post-process layer. They map to
// permanent_error responses because retrying cannot fix them.
var (
	ErrResourceNotFound   = errors.New("resourcedb: resource not found")
	ErrChecksumMismatch   = errors.New("resourcedb: sha256 mismatch")
	ErrSizeMismatch       = errors.New("resourcedb: size_bytes mismatch")
	ErrUnsupportedEncoing = errors.New("resourcedb: unsupported content_encoding")
	ErrPayloadTooLarge    = errors.New("resourcedb: decompressed payload exceeds limit")
	ErrSchemaVersion      = errors.New("resourcedb: unsupported schema_version")
)

// SupportedSchemaVersion is the resource payload schema the Go post-process
// understands.
const SupportedSchemaVersion = 1

const schemaSQL = `
CREATE TABLE IF NOT EXISTS resource_tab (
  resource_key     TEXT PRIMARY KEY,
  content_type     TEXT NOT NULL,
  content_encoding TEXT NOT NULL DEFAULT '',
  schema_version   INTEGER NOT NULL,
  sha256           TEXT NOT NULL,
  size_bytes       INTEGER NOT NULL,
  payload          BLOB NOT NULL,
  created_at       INTEGER NOT NULL,
  expires_at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_resource_tab_expire ON resource_tab(expires_at);
`

// Envelope mirrors the JSON stored in worker_tasks.result_data.
type Envelope struct {
	ResourceKey     string `json:"resource_key"`
	ContentType     string `json:"content_type"`
	ContentEncoding string `json:"content_encoding"`
	SchemaVersion   int    `json:"schema_version"`
	SHA256          string `json:"sha256"`
	SizeBytes       int64  `json:"size_bytes"`
	ExpiresAt       int64  `json:"expires_at"`
}

// DB wraps the resource database pool.
type DB struct {
	pool *sql.DB
}

// Open opens (creating if necessary) the resource database and ensures the
// resource_tab schema exists.
func Open(ctx context.Context, path string) (*DB, error) {
	pool, err := fdb.Open(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("resourcedb: open: %w", err)
	}
	if _, err := pool.ExecContext(ctx, schemaSQL); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("resourcedb: ensure schema: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Close releases the underlying pool.
func (d *DB) Close() error { return d.pool.Close() }

// Pool exposes the raw pool for tests.
func (d *DB) Pool() *sql.DB { return d.pool }

type storedResource struct {
	contentType     string
	contentEncoding string
	schemaVersion   int
	sha256Hex       string
	sizeBytes       int64
	payload         []byte
}

// Read loads the resource referenced by env, validates the envelope against
// the stored row (sha256, size_bytes, content_encoding, schema_version) and
// returns the decompressed payload subject to MaxDecompressedBytes.
func (d *DB) Read(ctx context.Context, env Envelope) ([]byte, error) {
	var row storedResource
	err := d.pool.QueryRowContext(ctx, `
		SELECT content_type, content_encoding, schema_version, sha256, size_bytes, payload
		FROM resource_tab WHERE resource_key = ?`, env.ResourceKey).
		Scan(&row.contentType, &row.contentEncoding, &row.schemaVersion,
			&row.sha256Hex, &row.sizeBytes, &row.payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrResourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("resourcedb: read resource: %w", err)
	}

	if row.schemaVersion != env.SchemaVersion || row.schemaVersion != SupportedSchemaVersion {
		return nil, fmt.Errorf("%w: stored=%d envelope=%d", ErrSchemaVersion, row.schemaVersion, env.SchemaVersion)
	}
	if row.contentEncoding != env.ContentEncoding {
		return nil, fmt.Errorf("%w: stored=%q envelope=%q",
			ErrUnsupportedEncoing, row.contentEncoding, env.ContentEncoding)
	}
	if int64(len(row.payload)) != row.sizeBytes || row.sizeBytes != env.SizeBytes {
		return nil, fmt.Errorf("%w: stored=%d envelope=%d actual=%d",
			ErrSizeMismatch, row.sizeBytes, env.SizeBytes, len(row.payload))
	}
	sum := sha256.Sum256(row.payload)
	if got := hex.EncodeToString(sum[:]); got != row.sha256Hex || got != env.SHA256 {
		return nil, fmt.Errorf("%w: computed=%s stored=%s envelope=%s",
			ErrChecksumMismatch, got, row.sha256Hex, env.SHA256)
	}

	switch row.contentEncoding {
	case "gzip":
		return gunzipLimited(row.payload)
	case "":
		if int64(len(row.payload)) > MaxDecompressedBytes {
			return nil, ErrPayloadTooLarge
		}
		return row.payload, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedEncoing, row.contentEncoding)
	}
}

func gunzipLimited(payload []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("resourcedb: gzip reader: %w", err)
	}
	defer func() { _ = zr.Close() }()
	// Read one byte beyond the limit to detect oversize payloads.
	out, err := io.ReadAll(io.LimitReader(zr, MaxDecompressedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("resourcedb: gunzip: %w", err)
	}
	if int64(len(out)) > MaxDecompressedBytes {
		return nil, ErrPayloadTooLarge
	}
	return out, nil
}

// DeleteExpired removes all resources whose expires_at is strictly before now.
// It never inspects worker_tasks: resource lifetime is governed solely by the
// expires_at written at insert time.
func (d *DB) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := d.pool.ExecContext(ctx,
		`DELETE FROM resource_tab WHERE expires_at < ?`, now.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("resourcedb: delete expired: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("resourcedb: rows affected: %w", err)
	}
	return n, nil
}

// InsertContent stores a resource using content addressing: the resource key
// IS the payload's sha256 hex. Repeated uploads of identical content are
// idempotent (the row is kept, its TTL refreshed), which makes sidecar upload
// retries safe. The sidecar never touches resource_db directly; all writes
// flow through this method via the internal upload API.
func (d *DB) InsertContent(
	ctx context.Context,
	contentType, contentEncoding string,
	schemaVersion int,
	payload []byte,
	now time.Time,
	ttl time.Duration,
) (Envelope, error) {
	sum := sha256.Sum256(payload)
	key := hex.EncodeToString(sum[:])
	env := Envelope{
		ResourceKey:     key,
		ContentType:     contentType,
		ContentEncoding: contentEncoding,
		SchemaVersion:   schemaVersion,
		SHA256:          key,
		SizeBytes:       int64(len(payload)),
		ExpiresAt:       now.Add(ttl).UnixMilli(),
	}
	_, err := d.pool.ExecContext(ctx, `
		INSERT INTO resource_tab
			(resource_key, content_type, content_encoding, schema_version,
			 sha256, size_bytes, payload, created_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(resource_key) DO UPDATE SET
			expires_at=excluded.expires_at`,
		env.ResourceKey, env.ContentType, env.ContentEncoding, env.SchemaVersion,
		env.SHA256, env.SizeBytes, payload, now.UnixMilli(), env.ExpiresAt)
	if err != nil {
		return Envelope{}, fmt.Errorf("resourcedb: insert resource: %w", err)
	}
	return env, nil
}

// GzipBytes compresses raw with gzip; helper shared by tests that need to
// fabricate sidecar-equivalent resources.
func GzipBytes(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, fmt.Errorf("resourcedb: gzip write: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("resourcedb: gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

// StartCleanup launches a goroutine that deletes expired resources every
// interval until ctx is done.
func StartCleanup(ctx context.Context, d *DB, interval time.Duration, logf func(msg string, args ...any)) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := d.DeleteExpired(ctx, time.Now())
				if err != nil {
					if ctx.Err() == nil {
						logf("resource_db cleanup failed", "error", err)
					}
					continue
				}
				if n > 0 {
					logf("resource_db cleanup removed expired resources", "count", n)
				}
			}
		}
	}()
}
