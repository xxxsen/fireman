-- 任务化市场数据获取。
--
-- Go 负责创建任务与查询状态，sidecar 作为市场数据 worker 领取并执行任务。
-- worker_tasks 是通用调度表，不放业务字段；业务差异由 type + payload_json 表达。
-- 结果 payload 存独立 resource_db（见 internal/resourcedb），worker_tasks.result_data
-- 只保存资源 envelope。

CREATE TABLE worker_tasks (
  id            TEXT PRIMARY KEY,
  version_no    INTEGER NOT NULL,
  type          TEXT NOT NULL,
  status        TEXT NOT NULL,
  dedupe_key    TEXT NOT NULL DEFAULT '',
  payload_json  TEXT NOT NULL,
  result_data   TEXT NOT NULL DEFAULT '',
  heartbeat_at  INTEGER,
  error_code    TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  post_process_attempts INTEGER NOT NULL DEFAULT 0,
  next_post_process_at INTEGER,
  created_at    INTEGER NOT NULL,
  started_at    INTEGER,
  pre_completed_at INTEGER,
  finished_at   INTEGER
);

CREATE INDEX idx_worker_tasks_claim
ON worker_tasks(status, created_at);

-- 同类型同 dedupe_key 的 active 任务只能有一个。
CREATE UNIQUE INDEX uq_worker_tasks_active
ON worker_tasks(type, dedupe_key)
WHERE status IN ('pending', 'running', 'pre_complete') AND dedupe_key <> '';

-- version_no 由 SQLite 自增表分配，保证单调递增且不依赖 task id 的可比较性。
CREATE TABLE worker_task_versions (
  version_no INTEGER PRIMARY KEY AUTOINCREMENT
);

-- Go post-process 可重入的依据：记录某一业务影响范围已处理到的任务版本。
CREATE TABLE market_data_versions (
  version_key      TEXT PRIMARY KEY,
  version_no       INTEGER NOT NULL,
  task_id          TEXT NOT NULL,
  updated_at       INTEGER NOT NULL
);

-- 全局资产目录，区别于用户导入后的 instruments 表。
CREATE TABLE market_assets (
  asset_key          TEXT PRIMARY KEY,
  market             TEXT NOT NULL,
  instrument_type    TEXT NOT NULL,
  region_code        TEXT NOT NULL DEFAULT '',
  symbol             TEXT NOT NULL,
  name               TEXT NOT NULL,
  exchange           TEXT NOT NULL DEFAULT '',
  instrument_kind    TEXT NOT NULL DEFAULT '',
  currency           TEXT NOT NULL DEFAULT '',
  canonical_symbol   TEXT NOT NULL DEFAULT '',
  fee_mode           TEXT NOT NULL DEFAULT '',
  active             INTEGER NOT NULL DEFAULT 1,
  listing_status     TEXT NOT NULL DEFAULT 'active',
  last_seen_at       INTEGER NOT NULL,
  source_name        TEXT NOT NULL,
  source_as_of       TEXT NOT NULL DEFAULT '',
  refreshed_at       INTEGER NOT NULL,
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL,
  UNIQUE(market, instrument_type, region_code, symbol)
);

CREATE INDEX idx_market_assets_search
ON market_assets(market, instrument_type, active);

CREATE INDEX idx_market_assets_canonical_fund
ON market_assets(market, instrument_type, canonical_symbol);

-- 资产目录同步状态；实时任务状态统一从 worker_tasks 读取，这里只保存关联与
-- 最后成功信息，避免出现两份需要同步的状态。
CREATE TABLE market_asset_sync_state (
  scope              TEXT PRIMARY KEY,
  last_task_id       TEXT NOT NULL DEFAULT '',
  last_success_task_id TEXT NOT NULL DEFAULT '',
  last_success_at    INTEGER,
  updated_at         INTEGER NOT NULL
);

-- 全局资产历史数据；adjust_policy/point_type 属于历史维度，不进入 asset_key。
CREATE TABLE market_asset_points (
  asset_key          TEXT NOT NULL,
  adjust_policy      TEXT NOT NULL DEFAULT 'none',
  point_type         TEXT NOT NULL,
  trade_date         TEXT NOT NULL,
  value              REAL NOT NULL,
  source_name        TEXT NOT NULL,
  fetched_at         INTEGER NOT NULL,
  PRIMARY KEY(asset_key, adjust_policy, point_type, trade_date),
  FOREIGN KEY(asset_key) REFERENCES market_assets(asset_key) ON DELETE CASCADE
);

CREATE TABLE market_asset_history_state (
  asset_key          TEXT NOT NULL,
  adjust_policy      TEXT NOT NULL DEFAULT 'none',
  point_type         TEXT NOT NULL,
  last_task_id       TEXT NOT NULL DEFAULT '',
  last_success_task_id TEXT NOT NULL DEFAULT '',
  last_success_at    INTEGER,
  data_as_of         TEXT NOT NULL DEFAULT '',
  point_count        INTEGER NOT NULL DEFAULT 0,
  source_name        TEXT NOT NULL DEFAULT '',
  updated_at         INTEGER NOT NULL,
  PRIMARY KEY(asset_key, adjust_policy, point_type),
  FOREIGN KEY(asset_key) REFERENCES market_assets(asset_key) ON DELETE CASCADE
);

-- 资产详情投影：post-process 提交时同步计算 annual/trailing returns，
-- 详情页读取时不再重算。
CREATE TABLE market_asset_detail_projections (
  asset_key            TEXT NOT NULL,
  adjust_policy        TEXT NOT NULL DEFAULT 'none',
  point_type           TEXT NOT NULL,
  annual_returns_json  TEXT NOT NULL DEFAULT '[]',
  trailing_returns_json TEXT NOT NULL DEFAULT '{}',
  computed_at          INTEGER NOT NULL,
  PRIMARY KEY(asset_key, adjust_policy, point_type),
  FOREIGN KEY(asset_key) REFERENCES market_assets(asset_key) ON DELETE CASCADE
);

-- P4：用户导入的 instruments 记录其来源 market asset。
ALTER TABLE instruments ADD COLUMN asset_key TEXT NOT NULL DEFAULT '';
