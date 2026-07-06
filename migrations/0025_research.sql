-- 组合研究（td/099）：研究集合、资产项、保存的筛选条件、回测运行与结果，
-- 以及筛选器使用的预计算研究指标投影。
--
-- 研究集合是独立业务对象，不复用 plans / plan_holdings。回测运行的结果
-- 与创建时的输入快照（source_hash / input_hash）绑定，行情刷新不会改写旧 run。

CREATE TABLE research_collections (
  id                      TEXT PRIMARY KEY,
  name                    TEXT NOT NULL,
  description             TEXT NOT NULL DEFAULT '',
  base_currency           TEXT NOT NULL DEFAULT 'CNY',
  initial_amount_minor    INTEGER NOT NULL DEFAULT 100000000,
  rebalance_policy        TEXT NOT NULL DEFAULT 'monthly',
  rebalance_threshold     REAL NOT NULL DEFAULT 0,
  start_policy            TEXT NOT NULL DEFAULT 'common_intersection',
  window_start            TEXT NOT NULL DEFAULT '',
  window_end              TEXT NOT NULL DEFAULT '',
  benchmark_asset_key     TEXT,
  risk_free_rate          REAL NOT NULL DEFAULT 0,
  transaction_cost_rate   REAL NOT NULL DEFAULT 0,
  status                  TEXT NOT NULL DEFAULT 'active',
  tags_json               TEXT NOT NULL DEFAULT '[]',
  created_at              INTEGER NOT NULL,
  updated_at              INTEGER NOT NULL,
  FOREIGN KEY(benchmark_asset_key) REFERENCES market_assets(asset_key)
);

CREATE TABLE research_collection_items (
  id              TEXT PRIMARY KEY,
  collection_id   TEXT NOT NULL,
  asset_key       TEXT NOT NULL,
  enabled         INTEGER NOT NULL DEFAULT 1,
  weight          REAL NOT NULL,
  weight_locked   INTEGER NOT NULL DEFAULT 0,
  adjust_policy   TEXT NOT NULL DEFAULT 'none',
  point_type      TEXT NOT NULL,
  asset_class     TEXT NOT NULL DEFAULT '',
  region          TEXT NOT NULL DEFAULT '',
  note            TEXT NOT NULL DEFAULT '',
  sort_order      INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  UNIQUE(collection_id, asset_key, adjust_policy, point_type),
  FOREIGN KEY(collection_id) REFERENCES research_collections(id) ON DELETE CASCADE,
  FOREIGN KEY(asset_key) REFERENCES market_assets(asset_key)
);

CREATE INDEX idx_research_collection_items_collection
ON research_collection_items(collection_id, sort_order);

CREATE TABLE research_saved_filters (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  filters_json  TEXT NOT NULL,
  sort_order    INTEGER NOT NULL DEFAULT 0,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL
);

CREATE TABLE research_backtest_runs (
  id                    TEXT PRIMARY KEY,
  collection_id         TEXT NOT NULL,
  job_id                TEXT NOT NULL UNIQUE,
  input_hash            TEXT NOT NULL,
  input_snapshot_json   TEXT NOT NULL,
  source_hash           TEXT NOT NULL,
  engine_version        TEXT NOT NULL,
  base_currency         TEXT NOT NULL,
  rebalance_policy      TEXT NOT NULL,
  window_start          TEXT NOT NULL,
  window_end            TEXT NOT NULL,
  status                TEXT NOT NULL,
  summary_json          TEXT NOT NULL DEFAULT '{}',
  data_quality_json     TEXT NOT NULL DEFAULT '{}',
  created_at            INTEGER NOT NULL,
  completed_at          INTEGER,
  FOREIGN KEY(collection_id) REFERENCES research_collections(id) ON DELETE CASCADE,
  FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX uq_research_backtest_runs_success_input
ON research_backtest_runs(collection_id, input_hash)
WHERE status = 'succeeded';

CREATE INDEX idx_research_backtest_runs_collection
ON research_backtest_runs(collection_id, created_at DESC);

CREATE TABLE research_backtest_points (
  run_id             TEXT NOT NULL,
  trade_date         TEXT NOT NULL,
  nav                REAL NOT NULL,
  cumulative_return  REAL NOT NULL,
  period_return      REAL NOT NULL,
  drawdown           REAL NOT NULL,
  benchmark_nav      REAL,
  benchmark_return   REAL,
  weights_json       TEXT NOT NULL DEFAULT '{}',
  contributions_json TEXT NOT NULL DEFAULT '{}',
  PRIMARY KEY(run_id, trade_date),
  FOREIGN KEY(run_id) REFERENCES research_backtest_runs(id) ON DELETE CASCADE
);

CREATE TABLE research_backtest_years (
  run_id           TEXT NOT NULL,
  year             INTEGER NOT NULL,
  annual_return    REAL NOT NULL,
  volatility       REAL NOT NULL,
  max_drawdown     REAL NOT NULL,
  start_nav        REAL NOT NULL,
  end_nav          REAL NOT NULL,
  is_partial       INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(run_id, year),
  FOREIGN KEY(run_id) REFERENCES research_backtest_runs(id) ON DELETE CASCADE
);

CREATE TABLE research_backtest_months (
  run_id          TEXT NOT NULL,
  year            INTEGER NOT NULL,
  month           INTEGER NOT NULL,
  monthly_return  REAL NOT NULL,
  PRIMARY KEY(run_id, year, month),
  FOREIGN KEY(run_id) REFERENCES research_backtest_runs(id) ON DELETE CASCADE
);

-- 筛选器使用的预计算研究指标（收益/风险/风险收益），按历史维度存储。
-- 在历史 post-process 提交时与 detail projection 一起计算；筛选查询时对
-- 已有历史但缺指标的维度做惰性补算。
CREATE TABLE research_asset_metrics (
  asset_key          TEXT NOT NULL,
  adjust_policy      TEXT NOT NULL DEFAULT 'none',
  point_type         TEXT NOT NULL,
  start_date         TEXT NOT NULL DEFAULT '',
  end_date           TEXT NOT NULL DEFAULT '',
  point_count        INTEGER NOT NULL DEFAULT 0,
  history_years      REAL NOT NULL DEFAULT 0,
  cagr               REAL,
  annual_volatility  REAL,
  max_drawdown       REAL,
  downside_volatility REAL,
  sharpe             REAL,
  calmar             REAL,
  return_1y          REAL,
  return_3y          REAL,
  return_5y          REAL,
  computed_at        INTEGER NOT NULL,
  PRIMARY KEY(asset_key, adjust_policy, point_type),
  FOREIGN KEY(asset_key) REFERENCES market_assets(asset_key) ON DELETE CASCADE
);
