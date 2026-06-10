-- Short-lived resolution tickets bind resolve results to import-async.
-- Partial unique index enforces one queued/running instrument_fetch per input_hash.

CREATE TABLE resolution_tickets (
    id TEXT PRIMARY KEY,
    market TEXT NOT NULL,
    instrument_type TEXT NOT NULL,
    code TEXT NOT NULL,
    provider_symbol TEXT NOT NULL,
    name TEXT NOT NULL,
    exchange TEXT NOT NULL DEFAULT '',
    instrument_kind TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER
);

CREATE INDEX idx_resolution_tickets_expires_at ON resolution_tickets(expires_at);

-- Cancel duplicate active instrument_fetch jobs before partial unique index.
-- Keep one job per (type, input_hash) by created_at ASC, id ASC; cancel the rest.
UPDATE jobs
SET status = 'canceled',
    finished_at = (CAST(strftime('%s', 'now') AS INTEGER) * 1000),
    error_code = 'duplicate_instrument_fetch_migrated',
    error_message = 'Canceled during migration: duplicate active instrument_fetch job',
    cancel_requested = 1,
    phase = ''
WHERE type = 'instrument_fetch'
  AND status IN ('queued', 'running')
  AND id IN (
    SELECT id
    FROM (
      SELECT id,
             ROW_NUMBER() OVER (
               PARTITION BY type, input_hash
               ORDER BY created_at ASC, id ASC
             ) AS rn
      FROM jobs
      WHERE type = 'instrument_fetch'
        AND status IN ('queued', 'running')
    )
    WHERE rn > 1
  );

CREATE UNIQUE INDEX uq_jobs_instrument_fetch_active
ON jobs(type, input_hash)
WHERE type = 'instrument_fetch' AND status IN ('queued', 'running');
