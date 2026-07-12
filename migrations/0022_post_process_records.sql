-- Go-owned task result finalization records. Append-only and observational.
CREATE TABLE worker_task_finalize_records (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id       TEXT    NOT NULL,
  task_type     TEXT    NOT NULL DEFAULT '',
  attempt_no    INTEGER NOT NULL DEFAULT 0,
  result        TEXT    NOT NULL,            -- success | retryable_error | permanent_error
  error_code    TEXT    NOT NULL DEFAULT '',
  error_message TEXT    NOT NULL DEFAULT '',
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL
);

CREATE INDEX idx_worker_task_finalize_records_task
ON worker_task_finalize_records(task_id, created_at DESC);

CREATE INDEX idx_worker_task_finalize_records_created
ON worker_task_finalize_records(created_at);
