-- Idempotency keys for unified worker task creation.

CREATE TABLE worker_task_idempotency_keys (
  scope_type      TEXT NOT NULL,
  scope_id        TEXT NOT NULL,
  task_type       TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  task_id         TEXT NOT NULL UNIQUE,
  input_hash      TEXT NOT NULL,
  created_at      INTEGER NOT NULL,
  PRIMARY KEY (scope_type, scope_id, task_type, idempotency_key),
  FOREIGN KEY (task_id) REFERENCES worker_tasks(id) ON DELETE CASCADE
);

CREATE INDEX idx_worker_task_idempotency_task
ON worker_task_idempotency_keys(task_id);
