-- 计划持仓与模拟快照直接引用全局市场资产目录（market_assets.asset_key），
-- 移除“用户资产库”中间层：
--
--   market_assets -> plan_holdings.asset_key
--           |
--           +-> asset_history_sync -> market_asset_points
--           |
--           +-> simulation snapshot builder -> FIRE simulation
--
-- instruments 收缩为内部系统表（系统 FX 汇率），不再作为用户可见资产库。
-- 现金类持仓通过内置市场资产（SYS|cash||CNY 等）表达。
-- 系统未上线：可映射的存量数据尽力迁移，无法映射的旧录入数据直接丢弃。

------------------------------------------------------------
-- 1. 内置系统现金市场资产（不参与外部目录同步）
------------------------------------------------------------

INSERT OR IGNORE INTO market_assets (
  asset_key, market, instrument_type, region_code, symbol, name,
  exchange, instrument_kind, currency,
  active, listing_status, last_seen_at,
  source_name, source_as_of, refreshed_at, created_at, updated_at
) VALUES
  ('SYS|cash||CNY', 'SYS', 'cash', '', 'CNY', '人民币现金',
   '', 'cash', 'CNY', 1, 'active', 0, 'system', '', 0, 0, 0),
  ('SYS|cash||USD', 'SYS', 'cash', '', 'USD', '美元现金',
   '', 'cash', 'USD', 1, 'active', 0, 'system', '', 0, 0, 0),
  ('SYS|cash||HKD', 'SYS', 'cash', '', 'HKD', '港币现金',
   '', 'cash', 'HKD', 1, 'active', 0, 'system', '', 0, 0, 0);

------------------------------------------------------------
-- 2. 市场资产模拟快照（替代 instrument_simulation_snapshots）
------------------------------------------------------------

CREATE TABLE market_asset_simulation_snapshots (
  id                       TEXT    PRIMARY KEY,
  asset_key                TEXT    NOT NULL,
  plan_id                  TEXT,
  inclusion_date           TEXT    NOT NULL,
  as_of_date               TEXT    NOT NULL,
  window_start             TEXT,
  window_end               TEXT,
  complete_year_start      INTEGER,
  complete_year_end        INTEGER,
  complete_year_count      INTEGER NOT NULL,
  daily_observation_count  INTEGER NOT NULL,
  monthly_return_count     INTEGER NOT NULL,
  volatility_method        TEXT    NOT NULL,
  metrics_version          TEXT    NOT NULL,
  history_depth            TEXT    NOT NULL,
  historical_cagr          REAL    NOT NULL,
  modeled_annual_return    REAL    NOT NULL,
  annual_volatility        REAL    NOT NULL,
  max_drawdown             REAL    NOT NULL,
  expense_ratio            REAL,
  expense_ratio_status     TEXT    NOT NULL,
  fee_treatment            TEXT    NOT NULL,
  source_mode              TEXT    NOT NULL,            -- market_asset_history | system_cash
  quality_status           TEXT    NOT NULL,
  warnings_json            TEXT    NOT NULL DEFAULT '[]',
  source_hash              TEXT    NOT NULL,
  created_at               INTEGER NOT NULL,
  FOREIGN KEY(asset_key) REFERENCES market_assets(asset_key),
  FOREIGN KEY(plan_id)   REFERENCES plans(id) ON DELETE CASCADE
);

CREATE INDEX idx_market_asset_sim_snapshots_asset
ON market_asset_simulation_snapshots(asset_key, created_at DESC);

CREATE TABLE market_asset_simulation_snapshot_years (
  snapshot_id    TEXT    NOT NULL,
  year           INTEGER NOT NULL,
  annual_return  REAL    NOT NULL,
  start_date     TEXT    NOT NULL,
  end_date       TEXT    NOT NULL,
  observations   INTEGER NOT NULL,
  PRIMARY KEY(snapshot_id, year),
  FOREIGN KEY(snapshot_id) REFERENCES market_asset_simulation_snapshots(id) ON DELETE CASCADE
);

CREATE TABLE market_asset_simulation_snapshot_months (
  snapshot_id  TEXT    NOT NULL,
  year         INTEGER NOT NULL,
  month        INTEGER NOT NULL,
  log_return   REAL    NOT NULL,
  PRIMARY KEY(snapshot_id, year, month),
  FOREIGN KEY(snapshot_id) REFERENCES market_asset_simulation_snapshots(id) ON DELETE CASCADE
);

-- 系统现金快照：0 收益 / 0 波动，全部内置现金资产共用列结构，各留一行。
INSERT INTO market_asset_simulation_snapshots (
  id, asset_key, plan_id,
  inclusion_date, as_of_date,
  window_start, window_end,
  complete_year_start, complete_year_end,
  complete_year_count, daily_observation_count, monthly_return_count,
  volatility_method, metrics_version, history_depth,
  historical_cagr, modeled_annual_return, annual_volatility, max_drawdown,
  expense_ratio, expense_ratio_status, fee_treatment,
  source_mode, quality_status, warnings_json, source_hash,
  created_at
) VALUES
  ('sim_snapshot_system_cash_cny', 'SYS|cash||CNY', NULL,
   '1970-01-01', '1970-01-01', NULL, NULL, NULL, NULL,
   0, 0, 0, 'not_applicable', 'system_cash_v1', 'system',
   0, 0, 0, 0, NULL, 'not_applicable', 'none',
   'system_cash', 'available', '[]', 'system_cash_cny', 0),
  ('sim_snapshot_system_cash_usd', 'SYS|cash||USD', NULL,
   '1970-01-01', '1970-01-01', NULL, NULL, NULL, NULL,
   0, 0, 0, 'not_applicable', 'system_cash_v1', 'system',
   0, 0, 0, 0, NULL, 'not_applicable', 'none',
   'system_cash', 'available', '[]', 'system_cash_usd', 0),
  ('sim_snapshot_system_cash_hkd', 'SYS|cash||HKD', NULL,
   '1970-01-01', '1970-01-01', NULL, NULL, NULL, NULL,
   0, 0, 0, 'not_applicable', 'system_cash_v1', 'system',
   0, 0, 0, 0, NULL, 'not_applicable', 'none',
   'system_cash', 'available', '[]', 'system_cash_hkd', 0);

-- 尽力迁移旧计划快照：仅迁移能映射到 market_assets 的（导入时记录过 asset_key）。
INSERT OR IGNORE INTO market_asset_simulation_snapshots (
  id, asset_key, plan_id, inclusion_date, as_of_date,
  window_start, window_end, complete_year_start, complete_year_end,
  complete_year_count, daily_observation_count, monthly_return_count,
  volatility_method, metrics_version, history_depth,
  historical_cagr, modeled_annual_return, annual_volatility, max_drawdown,
  expense_ratio, expense_ratio_status, fee_treatment,
  source_mode, quality_status, warnings_json, source_hash, created_at
)
SELECT s.id, i.asset_key, s.plan_id, s.inclusion_date, s.as_of_date,
  s.window_start, s.window_end, s.complete_year_start, s.complete_year_end,
  s.complete_year_count, s.daily_observation_count, s.monthly_return_count,
  s.volatility_method, s.metrics_version, s.history_depth,
  s.historical_cagr, s.modeled_annual_return, s.annual_volatility, s.max_drawdown,
  s.expense_ratio, s.expense_ratio_status, s.fee_treatment,
  'market_asset_history', s.quality_status, s.warnings_json, s.source_hash, s.created_at
FROM instrument_simulation_snapshots s
JOIN instruments i ON i.id = s.instrument_id
WHERE s.id <> 'sim_snapshot_system_cash_cny'
  AND i.asset_key <> ''
  AND EXISTS (SELECT 1 FROM market_assets ma WHERE ma.asset_key = i.asset_key);

INSERT OR IGNORE INTO market_asset_simulation_snapshot_years (
  snapshot_id, year, annual_return, start_date, end_date, observations
)
SELECT y.snapshot_id, y.year, y.annual_return, y.start_date, y.end_date, y.observations
FROM instrument_simulation_snapshot_years y
WHERE y.snapshot_id IN (SELECT id FROM market_asset_simulation_snapshots);

INSERT OR IGNORE INTO market_asset_simulation_snapshot_months (
  snapshot_id, year, month, log_return
)
SELECT m.snapshot_id, m.year, m.month, m.log_return
FROM instrument_simulation_snapshot_months m
WHERE m.snapshot_id IN (SELECT id FROM market_asset_simulation_snapshots);

------------------------------------------------------------
-- 3. plan_holdings 重建为 asset_key 引用
------------------------------------------------------------

CREATE TABLE plan_holdings_v2 (
  id                       TEXT    PRIMARY KEY,
  plan_id                  TEXT    NOT NULL,
  asset_key                TEXT    NOT NULL,
  enabled                  INTEGER NOT NULL DEFAULT 1,
  asset_class              TEXT    NOT NULL,
  region                   TEXT    NOT NULL,
  weight_within_group      REAL    NOT NULL,
  current_amount_minor     INTEGER NOT NULL DEFAULT 0,
  simulation_snapshot_id   TEXT    NOT NULL DEFAULT '',
  sort_order               INTEGER NOT NULL DEFAULT 0,
  created_at               INTEGER NOT NULL,
  updated_at               INTEGER NOT NULL,
  UNIQUE(plan_id, asset_key, asset_class, region),
  FOREIGN KEY(plan_id)   REFERENCES plans(id) ON DELETE CASCADE,
  FOREIGN KEY(asset_key) REFERENCES market_assets(asset_key)
);

INSERT OR IGNORE INTO plan_holdings_v2 (
  id, plan_id, asset_key, enabled, asset_class, region,
  weight_within_group, current_amount_minor, simulation_snapshot_id,
  sort_order, created_at, updated_at
)
SELECT h.id, h.plan_id,
  CASE WHEN h.instrument_id = 'system_cash_cny' THEN 'SYS|cash||CNY' ELSE i.asset_key END,
  h.enabled, h.asset_class, h.region,
  h.weight_within_group, h.current_amount_minor,
  CASE
    WHEN h.simulation_snapshot_id IN (SELECT id FROM market_asset_simulation_snapshots)
    THEN h.simulation_snapshot_id
    ELSE ''
  END,
  h.sort_order, h.created_at, h.updated_at
FROM plan_holdings h
JOIN instruments i ON i.id = h.instrument_id
WHERE h.instrument_id = 'system_cash_cny'
   OR (i.asset_key <> ''
       AND EXISTS (SELECT 1 FROM market_assets ma WHERE ma.asset_key = i.asset_key));

DROP TABLE plan_holdings;
ALTER TABLE plan_holdings_v2 RENAME TO plan_holdings;

CREATE INDEX idx_plan_holdings_plan  ON plan_holdings(plan_id, sort_order, created_at);
CREATE INDEX idx_plan_holdings_asset ON plan_holdings(asset_key);

------------------------------------------------------------
-- 4. 组合快照条目改为 asset_key
------------------------------------------------------------

CREATE TABLE portfolio_snapshot_items_v2 (
  snapshot_id  TEXT    NOT NULL,
  asset_key    TEXT    NOT NULL,
  amount_minor INTEGER NOT NULL,
  PRIMARY KEY(snapshot_id, asset_key),
  FOREIGN KEY(snapshot_id) REFERENCES portfolio_snapshots(id) ON DELETE CASCADE,
  FOREIGN KEY(asset_key)   REFERENCES market_assets(asset_key)
);

INSERT OR IGNORE INTO portfolio_snapshot_items_v2 (snapshot_id, asset_key, amount_minor)
SELECT p.snapshot_id,
  CASE WHEN p.instrument_id = 'system_cash_cny' THEN 'SYS|cash||CNY' ELSE i.asset_key END,
  p.amount_minor
FROM portfolio_snapshot_items p
JOIN instruments i ON i.id = p.instrument_id
WHERE p.instrument_id = 'system_cash_cny'
   OR (i.asset_key <> ''
       AND EXISTS (SELECT 1 FROM market_assets ma WHERE ma.asset_key = i.asset_key));

DROP TABLE portfolio_snapshot_items;
ALTER TABLE portfolio_snapshot_items_v2 RENAME TO portfolio_snapshot_items;

------------------------------------------------------------
-- 5. 计划级前瞻收益 override 改为 asset_key
------------------------------------------------------------

CREATE TABLE plan_return_assumption_overrides_v2 (
  plan_id            TEXT    NOT NULL,
  asset_key          TEXT    NOT NULL,
  forward_return     REAL,
  annual_volatility  REAL,
  reason             TEXT    NOT NULL,
  expires_at         TEXT    NOT NULL,
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL,
  PRIMARY KEY(plan_id, asset_key),
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

INSERT OR IGNORE INTO plan_return_assumption_overrides_v2 (
  plan_id, asset_key, forward_return, annual_volatility,
  reason, expires_at, created_at, updated_at
)
SELECT o.plan_id,
  CASE WHEN o.instrument_id = 'system_cash_cny' THEN 'SYS|cash||CNY' ELSE i.asset_key END,
  o.forward_return, o.annual_volatility, o.reason, o.expires_at, o.created_at, o.updated_at
FROM plan_return_assumption_overrides o
JOIN instruments i ON i.id = o.instrument_id
WHERE o.instrument_id = 'system_cash_cny'
   OR (i.asset_key <> ''
       AND EXISTS (SELECT 1 FROM market_assets ma WHERE ma.asset_key = i.asset_key));

DROP TABLE plan_return_assumption_overrides;
ALTER TABLE plan_return_assumption_overrides_v2 RENAME TO plan_return_assumption_overrides;

------------------------------------------------------------
-- 6. 再平衡草稿/执行行改为 asset_key。
--    草稿与执行是操作过程数据，其冻结基线内嵌了旧的持仓结构，直接清空重建。
------------------------------------------------------------

DELETE FROM rebalance_drafts;
DELETE FROM rebalance_executions;

DROP TABLE rebalance_draft_lines;
CREATE TABLE rebalance_draft_lines (
  id                              TEXT    PRIMARY KEY,
  draft_id                        TEXT    NOT NULL,
  holding_id                      TEXT    NOT NULL,
  asset_key                       TEXT    NOT NULL,
  baseline_current_minor          INTEGER NOT NULL,
  planned_current_minor           INTEGER NOT NULL,
  frozen_target_minor             INTEGER NOT NULL,
  frozen_gap_minor                INTEGER NOT NULL,
  frozen_gap_weight               REAL    NOT NULL,
  frozen_action                   TEXT    NOT NULL,
  frozen_suggested_trade_minor    INTEGER NOT NULL,
  recommended_package_delta_minor INTEGER NOT NULL DEFAULT 0,
  last_saved_at                   INTEGER,
  FOREIGN KEY(draft_id) REFERENCES rebalance_drafts(id) ON DELETE CASCADE
);
CREATE INDEX idx_rebalance_draft_lines_draft ON rebalance_draft_lines(draft_id);

DROP TABLE rebalance_execution_lines;
CREATE TABLE rebalance_execution_lines (
  id                     TEXT    PRIMARY KEY,
  execution_id           TEXT    NOT NULL,
  holding_id             TEXT    NOT NULL,
  asset_key              TEXT    NOT NULL,
  baseline_current_minor INTEGER NOT NULL,
  target_delta_minor     INTEGER NOT NULL,
  executed_delta_minor   INTEGER NOT NULL DEFAULT 0,
  remaining_delta_minor  INTEGER NOT NULL,
  action_direction       TEXT    NOT NULL,
  execution_status       TEXT    NOT NULL DEFAULT 'not_started',
  sort_order             INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(execution_id) REFERENCES rebalance_executions(id) ON DELETE CASCADE
);
CREATE INDEX idx_rebalance_execution_lines_execution ON rebalance_execution_lines(execution_id);

DROP TABLE rebalance_execution_events;
CREATE TABLE rebalance_execution_events (
  id                    TEXT    PRIMARY KEY,
  execution_id          TEXT    NOT NULL,
  seq                   INTEGER NOT NULL,
  event_type            TEXT    NOT NULL,
  asset_key             TEXT,
  amount_minor          INTEGER NOT NULL DEFAULT 0,
  cash_pool_after_minor INTEGER NOT NULL DEFAULT 0,
  payload_json          TEXT    NOT NULL DEFAULT '{}',
  created_at            INTEGER NOT NULL,
  FOREIGN KEY(execution_id) REFERENCES rebalance_executions(id) ON DELETE CASCADE,
  UNIQUE(execution_id, seq)
);
CREATE INDEX idx_rebalance_execution_events_execution ON rebalance_execution_events(execution_id);

------------------------------------------------------------
-- 7. 移除用户资产库遗留结构
------------------------------------------------------------

DROP TABLE instrument_simulation_snapshot_months;
DROP TABLE instrument_simulation_snapshot_years;
DROP TABLE instrument_simulation_snapshots;
DROP TABLE instrument_annual_returns;
DROP TABLE instrument_library_metrics;
DROP TABLE resolution_tickets;

-- instruments 收缩为内部系统表：仅保留系统行（FX 汇率等），
-- 用户录入行及其镜像 market_data_points 一并删除（ON DELETE CASCADE）。
DELETE FROM instruments WHERE is_system = 0;
