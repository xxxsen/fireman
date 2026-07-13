-- Fireman consolidated baseline schema.
-- DDL only: business/reference data is initialized by internal/bootstrap.
-- Amounts use integer minor units; timestamps use Unix milliseconds.

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
  return_assumption_mode       TEXT NOT NULL DEFAULT 'historical_cagr',
  assumption_selection_mode    TEXT NOT NULL DEFAULT 'follow_global',
  return_assumption_set_id     TEXT NOT NULL DEFAULT '',
  return_assumption_set_version INTEGER NOT NULL DEFAULT 0,
  return_assumption_scenario   TEXT NOT NULL DEFAULT 'baseline',
  custom_return_assumptions_json TEXT NOT NULL DEFAULT '',
  annual_retirement_income_minor INTEGER NOT NULL DEFAULT 0,
  annual_retirement_income_growth_rate REAL NOT NULL DEFAULT 0,
  FOREIGN KEY(plan_id)             REFERENCES plans(id) ON DELETE CASCADE,
  FOREIGN KEY(selected_scenario_id) REFERENCES allocation_scenarios(id)
);
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
  instrument_kind       TEXT NOT NULL DEFAULT '',
  asset_key             TEXT NOT NULL DEFAULT '',
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
CREATE TABLE portfolio_snapshots (
  id                  TEXT    PRIMARY KEY,
  plan_id             TEXT    NOT NULL,
  snapshot_date       TEXT    NOT NULL,
  total_amount_minor  INTEGER NOT NULL,
  note                TEXT    NOT NULL DEFAULT '',
  created_at          INTEGER NOT NULL,
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);
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
  simulation_run_id TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(task_id) REFERENCES worker_tasks(id) ON DELETE RESTRICT,
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);
CREATE TABLE fire_plan_improvement_runs (
  id                       TEXT    PRIMARY KEY,
  task_id                  TEXT    NOT NULL UNIQUE,
  plan_id                  TEXT    NOT NULL,
  source_simulation_run_id TEXT    NOT NULL,
  input_hash               TEXT    NOT NULL,
  algorithm_version        TEXT    NOT NULL,
  source_engine_version    TEXT    NOT NULL,
  source_config_hash       TEXT    NOT NULL,
  source_market_hash       TEXT    NOT NULL,
  config_json              TEXT    NOT NULL,
  input_snapshot_json      TEXT    NOT NULL,
  result_json              TEXT    NOT NULL DEFAULT '{}',
  created_at               INTEGER NOT NULL,
  completed_at             INTEGER,
  FOREIGN KEY(task_id) REFERENCES worker_tasks(id) ON DELETE RESTRICT,
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);
CREATE INDEX idx_fire_plan_improvement_runs_plan
ON fire_plan_improvement_runs(plan_id, created_at DESC);
CREATE INDEX idx_fire_plan_improvement_runs_input
ON fire_plan_improvement_runs(plan_id, input_hash, created_at DESC);
CREATE TABLE fire_plan_improvement_applications (
  id                    TEXT    PRIMARY KEY,
  improvement_run_id    TEXT    NOT NULL,
  proposal_id           TEXT    NOT NULL,
  plan_id               TEXT    NOT NULL,
  before_config_version INTEGER NOT NULL,
  after_config_version  INTEGER NOT NULL,
  preview_hash          TEXT    NOT NULL,
  before_json           TEXT    NOT NULL,
  after_json            TEXT    NOT NULL,
  applied_at            INTEGER NOT NULL,
  UNIQUE(improvement_run_id, proposal_id),
  FOREIGN KEY(improvement_run_id) REFERENCES fire_plan_improvement_runs(id) ON DELETE CASCADE,
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);
CREATE INDEX idx_fire_plan_improvement_applications_plan
ON fire_plan_improvement_applications(plan_id, applied_at DESC);
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
CREATE TABLE asset_refresh_events (
  id                  TEXT    PRIMARY KEY,
  plan_id             TEXT    NOT NULL,
  refreshed_at        INTEGER NOT NULL,
  before_total_minor  INTEGER NOT NULL,
  after_total_minor   INTEGER NOT NULL,
  sync_scale          INTEGER NOT NULL DEFAULT 0,
  config_changed      INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);
CREATE INDEX idx_asset_refresh_events_plan ON asset_refresh_events(plan_id, refreshed_at DESC);
CREATE TABLE allocation_scenario_region_targets (
  scenario_id         TEXT NOT NULL,
  asset_class         TEXT NOT NULL,
  region              TEXT NOT NULL,
  weight_within_class REAL NOT NULL,
  PRIMARY KEY (scenario_id, asset_class, region),
  FOREIGN KEY (scenario_id) REFERENCES allocation_scenarios(id) ON DELETE CASCADE
);
CREATE TABLE rebalance_executions (
  id                            TEXT    PRIMARY KEY,
  plan_id                       TEXT    NOT NULL,
  status                        TEXT    NOT NULL DEFAULT 'draft',
  created_at                    INTEGER NOT NULL,
  updated_at                    INTEGER NOT NULL,
  started_at                    INTEGER,
  completed_at                  INTEGER,
  baseline_holdings_total_minor INTEGER NOT NULL,
  baseline_config_version       INTEGER NOT NULL,
  baseline_snapshot_json        TEXT    NOT NULL DEFAULT '{}',
  cash_pool_minor               INTEGER NOT NULL DEFAULT 0,
  note                          TEXT    NOT NULL DEFAULT '',
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);
CREATE INDEX idx_rebalance_executions_plan_status ON rebalance_executions(plan_id, status);
CREATE UNIQUE INDEX idx_rebalance_executions_one_active_per_plan
  ON rebalance_executions(plan_id) WHERE status IN ('draft', 'in_progress');
CREATE INDEX idx_analysis_results_run_type_created
ON analysis_results(simulation_run_id, type, created_at DESC);
CREATE TABLE simulation_assumption_profiles (
  id              TEXT    NOT NULL,
  version         INTEGER NOT NULL,
  owner_scope     TEXT    NOT NULL DEFAULT 'user',   -- system | user
  name            TEXT    NOT NULL DEFAULT '',
  status          TEXT    NOT NULL DEFAULT 'draft',   -- draft | active | superseded
  canonical_json  TEXT    NOT NULL,
  content_hash    TEXT    NOT NULL,
  source_note     TEXT    NOT NULL DEFAULT '',
  reviewed_by     TEXT    NOT NULL DEFAULT '',
  reviewed_at     TEXT    NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  PRIMARY KEY (id, version)
);
CREATE INDEX idx_assumption_profiles_status ON simulation_assumption_profiles(id, status);
CREATE TABLE simulation_assumption_scenarios (
  profile_id            TEXT    NOT NULL,
  profile_version       INTEGER NOT NULL,
  scenario              TEXT    NOT NULL,
  return_shift_log      REAL    NOT NULL DEFAULT 0,
  return_shift_log_fx   REAL    NOT NULL DEFAULT 0,
  volatility_multiplier REAL    NOT NULL DEFAULT 1,
  PRIMARY KEY (profile_id, profile_version, scenario),
  FOREIGN KEY (profile_id, profile_version)
    REFERENCES simulation_assumption_profiles(id, version) ON DELETE CASCADE
);
CREATE TABLE simulation_assumption_return_priors (
  profile_id               TEXT    NOT NULL,
  profile_version          INTEGER NOT NULL,
  asset_class              TEXT    NOT NULL,
  region                   TEXT    NOT NULL,
  valuation_currency       TEXT    NOT NULL,
  annual_geometric_return  REAL    NOT NULL,
  annual_volatility_floor  REAL    NOT NULL DEFAULT 0,
  annual_volatility_ceiling REAL   NOT NULL DEFAULT 0,
  source_url               TEXT    NOT NULL DEFAULT '',
  published_at             TEXT    NOT NULL DEFAULT '',
  reviewed_at              TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (profile_id, profile_version, asset_class, region, valuation_currency),
  FOREIGN KEY (profile_id, profile_version)
    REFERENCES simulation_assumption_profiles(id, version) ON DELETE CASCADE
);
CREATE TABLE simulation_assumption_correlation_priors (
  profile_id      TEXT    NOT NULL,
  profile_version INTEGER NOT NULL,
  factor_a        TEXT    NOT NULL,
  factor_b        TEXT    NOT NULL,
  rho             REAL    NOT NULL DEFAULT 0,
  PRIMARY KEY (profile_id, profile_version, factor_a, factor_b),
  FOREIGN KEY (profile_id, profile_version)
    REFERENCES simulation_assumption_profiles(id, version) ON DELETE CASCADE
);
CREATE TABLE simulation_assumption_preferences (
  id                     INTEGER PRIMARY KEY CHECK (id = 1),
  default_profile_id     TEXT    NOT NULL DEFAULT '',
  default_profile_version INTEGER NOT NULL DEFAULT 0,
  default_scenario       TEXT    NOT NULL DEFAULT 'baseline',
  updated_at             INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE simulation_real_quantile_series (
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
CREATE TABLE market_data_versions (
  version_key      TEXT PRIMARY KEY,
  version_no       INTEGER NOT NULL,
  task_id          TEXT NOT NULL,
  updated_at       INTEGER NOT NULL
);
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
  adjust_policy            TEXT NOT NULL DEFAULT '',
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
CREATE TABLE plan_holdings (
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
CREATE INDEX idx_plan_holdings_plan  ON plan_holdings(plan_id, sort_order, created_at);
CREATE INDEX idx_plan_holdings_asset ON plan_holdings(asset_key);
CREATE TABLE portfolio_snapshot_items (
  snapshot_id  TEXT    NOT NULL,
  asset_key    TEXT    NOT NULL,
  amount_minor INTEGER NOT NULL,
  PRIMARY KEY(snapshot_id, asset_key),
  FOREIGN KEY(snapshot_id) REFERENCES portfolio_snapshots(id) ON DELETE CASCADE,
  FOREIGN KEY(asset_key)   REFERENCES market_assets(asset_key)
);
CREATE TABLE plan_return_assumption_overrides (
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
CREATE TABLE market_asset_sync_state (
  sync_key             TEXT PRIMARY KEY,
  scope                TEXT NOT NULL,
  last_task_id         TEXT NOT NULL DEFAULT '',
  last_success_task_id TEXT NOT NULL DEFAULT '',
  last_success_at      INTEGER,
  updated_at           INTEGER NOT NULL
);
CREATE INDEX idx_market_asset_sync_state_scope
  ON market_asset_sync_state(scope);
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
  tail_risk_confidence    REAL NOT NULL DEFAULT 0.95,
  tail_risk_horizon_days  INTEGER NOT NULL DEFAULT 20,
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
  task_id               TEXT NOT NULL UNIQUE,
  input_hash            TEXT NOT NULL,
  input_snapshot_json   TEXT NOT NULL,
  source_hash           TEXT NOT NULL,
  engine_version        TEXT NOT NULL,
  base_currency         TEXT NOT NULL,
  rebalance_policy      TEXT NOT NULL,
  window_start          TEXT NOT NULL,
  window_end            TEXT NOT NULL,
  summary_json          TEXT NOT NULL DEFAULT '{}',
  data_quality_json     TEXT NOT NULL DEFAULT '{}',
  created_at            INTEGER NOT NULL,
  completed_at          INTEGER,
  FOREIGN KEY(collection_id) REFERENCES research_collections(id) ON DELETE CASCADE,
  FOREIGN KEY(task_id) REFERENCES worker_tasks(id) ON DELETE RESTRICT
);
CREATE INDEX idx_research_backtest_runs_input
ON research_backtest_runs(collection_id, input_hash);
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
CREATE TABLE research_optimization_runs (
  id                    TEXT PRIMARY KEY,
  collection_id         TEXT NOT NULL,
  task_id               TEXT NOT NULL UNIQUE,
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
  created_at            INTEGER NOT NULL,
  completed_at          INTEGER,
  FOREIGN KEY(collection_id) REFERENCES research_collections(id) ON DELETE CASCADE,
  FOREIGN KEY(task_id) REFERENCES worker_tasks(id) ON DELETE RESTRICT
);
CREATE INDEX idx_research_optimization_runs_collection
ON research_optimization_runs(collection_id, created_at DESC);
CREATE TABLE market_data_auto_update_rules (
  id TEXT PRIMARY KEY,
  target_type TEXT NOT NULL CHECK (target_type IN ('directory_unit', 'asset_history')),
  sync_key TEXT NOT NULL DEFAULT '',
  asset_key TEXT NOT NULL DEFAULT '',
  adjust_policy TEXT NOT NULL DEFAULT '',
  point_type TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  interval_hours INTEGER NOT NULL DEFAULT 24 CHECK (interval_hours BETWEEN 1 AND 168),
  next_run_at INTEGER,
  last_enqueued_at INTEGER,
  last_task_id TEXT NOT NULL DEFAULT '',
  last_success_at INTEGER,
  last_failed_at INTEGER,
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  CHECK ((target_type = 'directory_unit' AND sync_key <> '' AND asset_key = '' AND adjust_policy = '' AND point_type = '')
      OR (target_type = 'asset_history' AND sync_key = '' AND asset_key <> '' AND adjust_policy <> '' AND point_type <> ''))
);
CREATE UNIQUE INDEX uq_market_data_auto_update_directory
  ON market_data_auto_update_rules(target_type, sync_key) WHERE target_type = 'directory_unit';
CREATE UNIQUE INDEX uq_market_data_auto_update_history
  ON market_data_auto_update_rules(target_type, asset_key, adjust_policy, point_type) WHERE target_type = 'asset_history';
CREATE INDEX idx_market_data_auto_update_due
  ON market_data_auto_update_rules(enabled, next_run_at);
CREATE INDEX idx_market_data_auto_update_task
  ON market_data_auto_update_rules(last_task_id) WHERE last_task_id <> '';
