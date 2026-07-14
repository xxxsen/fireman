CREATE TABLE research_investment_path_runs (
  id                  TEXT PRIMARY KEY,
  task_id             TEXT NOT NULL UNIQUE,
  asset_key           TEXT NOT NULL,
  mode                TEXT NOT NULL,
  input_hash          TEXT NOT NULL,
  source_hash         TEXT NOT NULL,
  input_snapshot_json TEXT NOT NULL,
  engine_version      TEXT NOT NULL,
  base_currency       TEXT NOT NULL,
  evaluation_start    TEXT NOT NULL,
  evaluation_end      TEXT NOT NULL,
  primary_start       TEXT NOT NULL,
  primary_end         TEXT NOT NULL,
  horizon_months      INTEGER NOT NULL,
  summary_json        TEXT NOT NULL DEFAULT '{}',
  data_quality_json   TEXT NOT NULL DEFAULT '{}',
  created_at          INTEGER NOT NULL,
  completed_at        INTEGER,
  FOREIGN KEY(asset_key) REFERENCES market_assets(asset_key),
  FOREIGN KEY(task_id) REFERENCES worker_tasks(id) ON DELETE RESTRICT
);
CREATE INDEX idx_research_investment_path_runs_asset_created
ON research_investment_path_runs(asset_key, created_at DESC);
CREATE INDEX idx_research_investment_path_runs_asset_input
ON research_investment_path_runs(asset_key, input_hash);

CREATE TABLE research_investment_path_points (
  run_id                                TEXT NOT NULL,
  strategy_key                          TEXT NOT NULL,
  valuation_date                        TEXT NOT NULL,
  account_value_minor                   INTEGER NOT NULL,
  asset_value_minor                     INTEGER NOT NULL,
  cash_value_minor                      INTEGER NOT NULL,
  cumulative_external_contribution_minor INTEGER NOT NULL,
  unit_nav                              REAL NOT NULL,
  drawdown                              REAL NOT NULL,
  PRIMARY KEY(run_id, strategy_key, valuation_date),
  FOREIGN KEY(run_id) REFERENCES research_investment_path_runs(id) ON DELETE CASCADE
);

CREATE TABLE research_investment_path_trades (
  run_id                  TEXT NOT NULL,
  strategy_key            TEXT NOT NULL,
  sequence_no             INTEGER NOT NULL,
  trade_date              TEXT NOT NULL,
  side                    TEXT NOT NULL,
  reason                  TEXT NOT NULL,
  gross_trade_minor       INTEGER NOT NULL,
  fee_minor               INTEGER NOT NULL,
  asset_value_delta_minor INTEGER NOT NULL,
  cash_delta_minor        INTEGER NOT NULL,
  PRIMARY KEY(run_id, strategy_key, sequence_no),
  FOREIGN KEY(run_id) REFERENCES research_investment_path_runs(id) ON DELETE CASCADE
);

CREATE TABLE research_investment_path_windows (
  run_id                              TEXT NOT NULL,
  strategy_key                        TEXT NOT NULL,
  window_start                        TEXT NOT NULL,
  window_end                          TEXT NOT NULL,
  total_contribution_minor            INTEGER NOT NULL,
  terminal_value_minor                INTEGER NOT NULL,
  profit_minor                        INTEGER NOT NULL,
  xirr                                REAL,
  xirr_reason                         TEXT NOT NULL DEFAULT '',
  twr_total                           REAL NOT NULL,
  twr_annualized                      REAL NOT NULL,
  max_drawdown                        REAL NOT NULL,
  max_drawdown_start                  TEXT NOT NULL DEFAULT '',
  max_drawdown_end                    TEXT NOT NULL DEFAULT '',
  longest_underwater_days             INTEGER NOT NULL,
  max_principal_deficit_minor         INTEGER NOT NULL,
  max_principal_deficit_ratio         REAL NOT NULL,
  longest_below_principal_days        INTEGER NOT NULL,
  first_recovery_above_principal_date TEXT NOT NULL DEFAULT '',
  average_cash_weight                 REAL NOT NULL,
  total_transaction_cost_minor        INTEGER NOT NULL,
  trade_count                         INTEGER NOT NULL,
  turnover                            REAL NOT NULL,
  deployment_complete_date            TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(run_id, strategy_key, window_start),
  FOREIGN KEY(run_id) REFERENCES research_investment_path_runs(id) ON DELETE CASCADE
);
CREATE INDEX idx_research_investment_path_windows_run_start
ON research_investment_path_windows(run_id, window_start, strategy_key);
