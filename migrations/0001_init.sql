-- Fireman initial schema. All amounts are stored as INTEGER minor units;
-- weights, rates and ratios are stored as REAL fractions (e.g. 3% = 0.03).
-- Timestamps use Unix milliseconds; trade dates use YYYY-MM-DD.
-- This migration creates the complete initial application schema.

------------------------------------------------------------
-- Allocation scenarios (declared first so that
-- plan_parameters can reference them via foreign key).
------------------------------------------------------------

CREATE TABLE allocation_scenarios (
  id          TEXT    PRIMARY KEY,
  name        TEXT    NOT NULL,
  description TEXT    NOT NULL DEFAULT '',
  is_builtin  INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE TABLE allocation_scenario_weights (
  scenario_id TEXT NOT NULL,
  asset_class TEXT NOT NULL,
  weight      REAL NOT NULL,
  PRIMARY KEY(scenario_id, asset_class),
  FOREIGN KEY(scenario_id) REFERENCES allocation_scenarios(id) ON DELETE CASCADE
);

------------------------------------------------------------
-- Plans
------------------------------------------------------------

CREATE TABLE plans (
  id              TEXT    PRIMARY KEY,
  name            TEXT    NOT NULL,
  base_currency   TEXT    NOT NULL DEFAULT 'CNY',
  valuation_date  TEXT    NOT NULL,
  status          TEXT    NOT NULL DEFAULT 'active',
  config_version  INTEGER NOT NULL DEFAULT 1,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);

CREATE TABLE plan_parameters (
  plan_id                       TEXT    PRIMARY KEY,
  current_age                   INTEGER NOT NULL,
  retirement_age                INTEGER NOT NULL,
  end_age                       INTEGER NOT NULL,
  total_assets_minor            INTEGER NOT NULL,
  annual_savings_minor          INTEGER NOT NULL,
  annual_savings_growth_rate    REAL    NOT NULL DEFAULT 0,
  annual_spending_minor         INTEGER NOT NULL,
  terminal_wealth_floor_minor   INTEGER NOT NULL DEFAULT 0,
  selected_scenario_id          TEXT,
  inflation_mode                TEXT    NOT NULL,
  fixed_inflation_rate          REAL    NOT NULL DEFAULT 0.03,
  inflation_mu                  REAL    NOT NULL DEFAULT 0.03,
  inflation_phi                 REAL    NOT NULL DEFAULT 0.5,
  inflation_sigma               REAL    NOT NULL DEFAULT 0.01,
  withdrawal_type               TEXT    NOT NULL DEFAULT 'fixed_real',
  withdrawal_rate               REAL    NOT NULL DEFAULT 0.04,
  withdrawal_floor_ratio        REAL    NOT NULL DEFAULT 0.70,
  withdrawal_ceiling_ratio      REAL    NOT NULL DEFAULT 1.30,
  withdrawal_tax_rate           REAL    NOT NULL DEFAULT 0,
  taxable_withdrawal_ratio      REAL    NOT NULL DEFAULT 0,
  rebalance_frequency           TEXT    NOT NULL DEFAULT 'annual',
  rebalance_threshold           REAL    NOT NULL DEFAULT 0.03,
  transaction_cost_rate         REAL    NOT NULL DEFAULT 0,
  simulation_runs               INTEGER NOT NULL DEFAULT 10000,
  student_t_df                  INTEGER NOT NULL DEFAULT 7,
  seed                          INTEGER,
  updated_at                    INTEGER NOT NULL,
  FOREIGN KEY(plan_id)             REFERENCES plans(id) ON DELETE CASCADE,
  FOREIGN KEY(selected_scenario_id) REFERENCES allocation_scenarios(id)
);

------------------------------------------------------------
-- Plan layered allocation targets
------------------------------------------------------------

CREATE TABLE plan_asset_class_targets (
  plan_id     TEXT NOT NULL,
  asset_class TEXT NOT NULL,
  weight      REAL NOT NULL,
  PRIMARY KEY(plan_id, asset_class),
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

CREATE TABLE plan_region_targets (
  plan_id              TEXT NOT NULL,
  asset_class          TEXT NOT NULL,
  region               TEXT NOT NULL,                   -- domestic | foreign
  weight_within_class  REAL NOT NULL,
  PRIMARY KEY(plan_id, asset_class, region),
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

------------------------------------------------------------
-- Asset library
------------------------------------------------------------

CREATE TABLE instruments (
  id                    TEXT    PRIMARY KEY,
  code                  TEXT    NOT NULL,
  name                  TEXT    NOT NULL,
  market                TEXT    NOT NULL,               -- CN | HK | US | SYSTEM
  instrument_type       TEXT    NOT NULL,
  asset_class           TEXT    NOT NULL,
  region                TEXT    NOT NULL,
  currency              TEXT    NOT NULL,
  provider              TEXT    NOT NULL DEFAULT 'akshare',
  provider_symbol       TEXT    NOT NULL DEFAULT '',
  adjust_policy         TEXT    NOT NULL DEFAULT 'none',
  is_system             INTEGER NOT NULL DEFAULT 0,
  expense_ratio         REAL,
  expense_ratio_status  TEXT    NOT NULL,               -- provider_verified | unavailable | not_applicable
  fee_treatment         TEXT    NOT NULL,               -- embedded | none
  status                TEXT    NOT NULL DEFAULT 'active',
  created_at            INTEGER NOT NULL,
  updated_at            INTEGER NOT NULL,
  UNIQUE(market, instrument_type, code, adjust_policy)
);

CREATE TABLE market_data_points (
  instrument_id TEXT    NOT NULL,
  trade_date    TEXT    NOT NULL,
  value         REAL    NOT NULL,
  point_type    TEXT    NOT NULL,                       -- adjusted_close | nav | total_return_index | fx_rate
  source_name   TEXT    NOT NULL,
  fetched_at    INTEGER NOT NULL,
  PRIMARY KEY(instrument_id, trade_date),
  FOREIGN KEY(instrument_id) REFERENCES instruments(id) ON DELETE CASCADE
);

CREATE TABLE instrument_annual_returns (
  instrument_id  TEXT    NOT NULL,
  year           INTEGER NOT NULL,
  annual_return  REAL    NOT NULL,
  start_date     TEXT    NOT NULL,
  end_date       TEXT    NOT NULL,
  start_value    REAL    NOT NULL,
  end_value      REAL    NOT NULL,
  observations   INTEGER NOT NULL,
  is_partial     INTEGER NOT NULL,
  PRIMARY KEY(instrument_id, year),
  FOREIGN KEY(instrument_id) REFERENCES instruments(id) ON DELETE CASCADE
);

CREATE TABLE instrument_simulation_snapshots (
  id                       TEXT    PRIMARY KEY,
  instrument_id            TEXT    NOT NULL,
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
  source_mode              TEXT    NOT NULL,            -- akshare_historical | system_cash
  quality_status           TEXT    NOT NULL,
  warnings_json            TEXT    NOT NULL DEFAULT '[]',
  source_hash              TEXT    NOT NULL,
  created_at               INTEGER NOT NULL,
  FOREIGN KEY(instrument_id) REFERENCES instruments(id),
  FOREIGN KEY(plan_id)       REFERENCES plans(id) ON DELETE CASCADE
);

CREATE TABLE instrument_simulation_snapshot_years (
  snapshot_id    TEXT    NOT NULL,
  year           INTEGER NOT NULL,
  annual_return  REAL    NOT NULL,
  start_date     TEXT    NOT NULL,
  end_date       TEXT    NOT NULL,
  observations   INTEGER NOT NULL,
  PRIMARY KEY(snapshot_id, year),
  FOREIGN KEY(snapshot_id) REFERENCES instrument_simulation_snapshots(id) ON DELETE CASCADE
);

------------------------------------------------------------
-- Plan holdings and portfolio snapshots
------------------------------------------------------------

CREATE TABLE plan_holdings (
  id                       TEXT    PRIMARY KEY,
  plan_id                  TEXT    NOT NULL,
  instrument_id            TEXT    NOT NULL,
  enabled                  INTEGER NOT NULL DEFAULT 1,
  asset_class              TEXT    NOT NULL,
  region                   TEXT    NOT NULL,
  weight_within_group      REAL    NOT NULL,
  current_amount_minor     INTEGER NOT NULL,
  simulation_snapshot_id   TEXT    NOT NULL,
  sort_order               INTEGER NOT NULL DEFAULT 0,
  created_at               INTEGER NOT NULL,
  updated_at               INTEGER NOT NULL,
  UNIQUE(plan_id, instrument_id),
  FOREIGN KEY(plan_id)                REFERENCES plans(id) ON DELETE CASCADE,
  FOREIGN KEY(instrument_id)          REFERENCES instruments(id),
  FOREIGN KEY(simulation_snapshot_id) REFERENCES instrument_simulation_snapshots(id)
);

CREATE TABLE portfolio_snapshots (
  id                  TEXT    PRIMARY KEY,
  plan_id             TEXT    NOT NULL,
  snapshot_date       TEXT    NOT NULL,
  total_amount_minor  INTEGER NOT NULL,
  note                TEXT    NOT NULL DEFAULT '',
  created_at          INTEGER NOT NULL,
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

CREATE TABLE portfolio_snapshot_items (
  snapshot_id    TEXT    NOT NULL,
  instrument_id  TEXT    NOT NULL,
  amount_minor   INTEGER NOT NULL,
  PRIMARY KEY(snapshot_id, instrument_id),
  FOREIGN KEY(snapshot_id)   REFERENCES portfolio_snapshots(id) ON DELETE CASCADE,
  FOREIGN KEY(instrument_id) REFERENCES instruments(id)
);

------------------------------------------------------------
-- Unified worker tasks and simulation results
------------------------------------------------------------

CREATE TABLE worker_task_versions (
  version_no INTEGER PRIMARY KEY AUTOINCREMENT
);

CREATE TABLE worker_tasks (
  id                  TEXT    PRIMARY KEY,
  version_no          INTEGER NOT NULL UNIQUE,
  worker_type         TEXT    NOT NULL,
  type                TEXT    NOT NULL,
  status              TEXT    NOT NULL,
  priority            INTEGER NOT NULL DEFAULT 100,
  scope_type          TEXT    NOT NULL DEFAULT '',
  scope_id            TEXT    NOT NULL DEFAULT '',
  dedupe_key          TEXT    NOT NULL DEFAULT '',
  input_hash          TEXT    NOT NULL DEFAULT '',
  payload_json        TEXT    NOT NULL DEFAULT '{}',
  result_key          TEXT    NOT NULL DEFAULT '',
  result_meta_json    TEXT    NOT NULL DEFAULT '{}',
  progress_current    INTEGER NOT NULL DEFAULT 0,
  progress_total      INTEGER NOT NULL DEFAULT 0,
  phase               TEXT    NOT NULL DEFAULT '',
  cancel_requested    INTEGER NOT NULL DEFAULT 0,
  attempt_count       INTEGER NOT NULL DEFAULT 0,
  max_attempts        INTEGER NOT NULL DEFAULT 2,
  available_at        INTEGER NOT NULL,
  claimed_by          TEXT    NOT NULL DEFAULT '',
  claim_token_hash    TEXT    NOT NULL DEFAULT '',
  attempt_started_at  INTEGER,
  heartbeat_at        INTEGER,
  lease_expires_at    INTEGER,
  finalize_attempts   INTEGER NOT NULL DEFAULT 0,
  next_finalize_at    INTEGER,
  error_code          TEXT    NOT NULL DEFAULT '',
  error_message       TEXT    NOT NULL DEFAULT '',
  created_at          INTEGER NOT NULL,
  started_at          INTEGER,
  result_reported_at  INTEGER,
  pre_completed_at    INTEGER,
  finished_at         INTEGER,
  updated_at          INTEGER NOT NULL
);

CREATE INDEX idx_worker_tasks_claim
ON worker_tasks(worker_type, status, available_at, priority DESC, created_at, id);

CREATE INDEX idx_worker_tasks_filter
ON worker_tasks(worker_type, status, created_at DESC, id DESC);

CREATE INDEX idx_worker_tasks_scope
ON worker_tasks(scope_type, scope_id, created_at DESC);

CREATE INDEX idx_worker_tasks_lease
ON worker_tasks(status, lease_expires_at);

CREATE INDEX idx_worker_tasks_finalize
ON worker_tasks(status, next_finalize_at);

CREATE UNIQUE INDEX uq_worker_tasks_active_dedupe
ON worker_tasks(worker_type, type, dedupe_key)
WHERE status IN ('pending', 'running', 'pre_complete') AND dedupe_key <> '';

CREATE TABLE worker_task_attempts (
  task_id             TEXT    NOT NULL,
  attempt_no          INTEGER NOT NULL,
  worker_type         TEXT    NOT NULL,
  worker_id           TEXT    NOT NULL,
  claim_token_hash    TEXT    NOT NULL DEFAULT '',
  claimed_at          INTEGER NOT NULL,
  last_heartbeat_at   INTEGER,
  released_at         INTEGER,
  outcome             TEXT    NOT NULL DEFAULT '',
  report_outcome      TEXT    NOT NULL DEFAULT '',
  result_key          TEXT    NOT NULL DEFAULT '',
  error_code          TEXT    NOT NULL DEFAULT '',
  error_message       TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY(task_id, attempt_no),
  FOREIGN KEY(task_id) REFERENCES worker_tasks(id) ON DELETE CASCADE
);

CREATE INDEX idx_worker_task_attempts_task
ON worker_task_attempts(task_id, attempt_no DESC);

CREATE TABLE simulation_runs (
  id                    TEXT    PRIMARY KEY,
  task_id               TEXT    NOT NULL UNIQUE,
  plan_id               TEXT    NOT NULL,
  input_hash            TEXT    NOT NULL,
  input_snapshot_json   TEXT    NOT NULL,
  market_snapshot_hash  TEXT    NOT NULL,
  engine_version        TEXT    NOT NULL,
  runs                  INTEGER NOT NULL,
  seed                  INTEGER NOT NULL,
  horizon_months        INTEGER NOT NULL,
  success_count         INTEGER NOT NULL,
  failure_count         INTEGER NOT NULL,
  summary_json          TEXT    NOT NULL,
  created_at            INTEGER NOT NULL,
  FOREIGN KEY(task_id) REFERENCES worker_tasks(id) ON DELETE RESTRICT,
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

CREATE TABLE simulation_path_index (
  run_id                    TEXT    NOT NULL,
  path_no                   INTEGER NOT NULL,
  path_seed                 INTEGER NOT NULL,
  succeeded                 INTEGER NOT NULL,
  failure_month             INTEGER,
  terminal_wealth_minor     INTEGER NOT NULL,
  max_drawdown              REAL    NOT NULL,
  representative_percentile TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY(run_id, path_no),
  FOREIGN KEY(run_id) REFERENCES simulation_runs(id) ON DELETE CASCADE
);

CREATE TABLE simulation_quantile_series (
  run_id        TEXT    NOT NULL,
  month_offset  INTEGER NOT NULL,
  p00_minor     INTEGER NOT NULL,
  p05_minor     INTEGER NOT NULL,
  p25_minor     INTEGER NOT NULL,
  p50_minor     INTEGER NOT NULL,
  p75_minor     INTEGER NOT NULL,
  p95_minor     INTEGER NOT NULL,
  PRIMARY KEY(run_id, month_offset),
  FOREIGN KEY(run_id) REFERENCES simulation_runs(id) ON DELETE CASCADE
);

CREATE TABLE analysis_results (
  task_id     TEXT    PRIMARY KEY,
  plan_id     TEXT    NOT NULL,
  type        TEXT    NOT NULL,
  input_hash  TEXT    NOT NULL,
  result_json TEXT    NOT NULL,
  created_at  INTEGER NOT NULL,
  FOREIGN KEY(task_id) REFERENCES worker_tasks(id) ON DELETE RESTRICT,
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

------------------------------------------------------------
-- Built-in scenarios
------------------------------------------------------------

INSERT INTO allocation_scenarios (id, name, description, is_builtin, created_at, updated_at) VALUES
  ('scn_builtin_accumulation', '积累期',     '距离 FIRE 较远，风险承受能力较高', 1, 0, 0),
  ('scn_builtin_near_fire',    '接近 FIRE',  'Excel 默认 70/30 配置',           1, 0, 0),
  ('scn_builtin_post_fire',    '已 FIRE',   '降低波动并预留现金缓冲',          1, 0, 0),
  ('scn_builtin_conservative', '保守',       '偏防守配置',                      1, 0, 0);

INSERT INTO allocation_scenario_weights (scenario_id, asset_class, weight) VALUES
  ('scn_builtin_accumulation', 'equity', 0.80),
  ('scn_builtin_accumulation', 'bond',   0.20),
  ('scn_builtin_accumulation', 'cash',   0.00),
  ('scn_builtin_near_fire',    'equity', 0.70),
  ('scn_builtin_near_fire',    'bond',   0.30),
  ('scn_builtin_near_fire',    'cash',   0.00),
  ('scn_builtin_post_fire',    'equity', 0.55),
  ('scn_builtin_post_fire',    'bond',   0.35),
  ('scn_builtin_post_fire',    'cash',   0.10),
  ('scn_builtin_conservative', 'equity', 0.45),
  ('scn_builtin_conservative', 'bond',   0.45),
  ('scn_builtin_conservative', 'cash',   0.10);

------------------------------------------------------------
-- System cash instrument and its immutable simulation snapshot.
-- The system cash instrument is non-deletable and represents the
-- 0% return / 0% volatility CNY cash bucket used by "其他" allocations.
------------------------------------------------------------

INSERT INTO instruments (
  id, code, name, market, instrument_type,
  asset_class, region, currency,
  provider, provider_symbol, adjust_policy,
  is_system, expense_ratio, expense_ratio_status, fee_treatment,
  status, created_at, updated_at
) VALUES (
  'system_cash_cny', 'SYSTEM_CASH_CNY', '人民币现金', 'SYSTEM', 'system_cash',
  'cash', 'domestic', 'CNY',
  'system', 'SYSTEM_CASH_CNY', 'none',
  1, NULL, 'not_applicable', 'none',
  'active', 0, 0
);

INSERT INTO instrument_simulation_snapshots (
  id, instrument_id, plan_id,
  inclusion_date, as_of_date,
  window_start, window_end,
  complete_year_start, complete_year_end,
  complete_year_count, daily_observation_count, monthly_return_count,
  volatility_method, metrics_version, history_depth,
  historical_cagr, modeled_annual_return, annual_volatility, max_drawdown,
  expense_ratio, expense_ratio_status, fee_treatment,
  source_mode, quality_status, warnings_json, source_hash,
  created_at
) VALUES (
  'sim_snapshot_system_cash_cny', 'system_cash_cny', NULL,
  '1970-01-01', '1970-01-01',
  NULL, NULL,
  NULL, NULL,
  0, 0, 0,
  'not_applicable', 'system_cash_v1', 'system',
  0, 0, 0, 0,
  NULL, 'not_applicable', 'none',
  'system_cash', 'available', '[]', 'system_cash_cny',
  0
);
