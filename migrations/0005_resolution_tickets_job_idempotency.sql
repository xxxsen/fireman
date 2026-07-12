-- Short-lived resolution tickets bind resolve results to import-async.

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
