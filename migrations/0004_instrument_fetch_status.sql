-- Instrument async fetch lifecycle: status column and job payload routing.
-- instruments.status: pending_fetch | active | fetch_failed (SQLite TEXT, no ALTER enum).
-- jobs.payload_json stores instrument_fetch job parameters for Worker routing.

ALTER TABLE jobs ADD COLUMN payload_json TEXT NOT NULL DEFAULT '';
