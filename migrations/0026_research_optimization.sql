-- 研究组合自动调优（td/103）：自动枚举候选权重组合并回测，
-- 一次输出最高收益、最低回撤、收益回撤平衡三组结果。

CREATE TABLE research_optimization_runs (
  id                    TEXT PRIMARY KEY,
  collection_id         TEXT NOT NULL,
  job_id                TEXT NOT NULL UNIQUE,
  status                TEXT NOT NULL DEFAULT 'queued',
  input_hash            TEXT NOT NULL,
  source_hash           TEXT NOT NULL,
  engine_version        TEXT NOT NULL,
  base_currency         TEXT NOT NULL,
  rebalance_policy      TEXT NOT NULL,
  window_start          TEXT NOT NULL,
  window_end            TEXT NOT NULL,
  config_json           TEXT NOT NULL DEFAULT '{}',
  input_snapshot_json   TEXT NOT NULL DEFAULT '{}',
  candidate_count       INTEGER NOT NULL DEFAULT 0,
  evaluated_count       INTEGER NOT NULL DEFAULT 0,
  result_json           TEXT NOT NULL DEFAULT '{}',
  error_code            TEXT NOT NULL DEFAULT '',
  error_message         TEXT NOT NULL DEFAULT '',
  created_at            INTEGER NOT NULL,
  completed_at          INTEGER,
  FOREIGN KEY(collection_id) REFERENCES research_collections(id) ON DELETE CASCADE,
  FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX idx_research_optimization_runs_collection
ON research_optimization_runs(collection_id, created_at DESC);
