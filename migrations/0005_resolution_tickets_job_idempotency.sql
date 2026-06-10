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

CREATE UNIQUE INDEX uq_jobs_instrument_fetch_active
ON jobs(type, input_hash)
WHERE type = 'instrument_fetch' AND status IN ('queued', 'running');
