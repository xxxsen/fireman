-- Idempotency keys for job creation.

CREATE TABLE job_idempotency_keys (
  plan_id         TEXT NOT NULL,
  job_type        TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  job_id          TEXT NOT NULL UNIQUE,
  input_hash      TEXT NOT NULL,
  created_at      INTEGER NOT NULL,
  PRIMARY KEY (plan_id, job_type, idempotency_key),
  FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX idx_job_idempotency_job
ON job_idempotency_keys(job_id);
