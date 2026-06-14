-- Multi-day rebalance execution: cash pool + staged trades + timeline.

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

CREATE TABLE rebalance_execution_lines (
  id                     TEXT    PRIMARY KEY,
  execution_id           TEXT    NOT NULL,
  holding_id             TEXT    NOT NULL,
  instrument_id          TEXT    NOT NULL,
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
  instrument_id         TEXT,
  amount_minor          INTEGER NOT NULL DEFAULT 0,
  cash_pool_after_minor INTEGER NOT NULL DEFAULT 0,
  payload_json          TEXT    NOT NULL DEFAULT '{}',
  created_at            INTEGER NOT NULL,
  FOREIGN KEY(execution_id) REFERENCES rebalance_executions(id) ON DELETE CASCADE,
  UNIQUE(execution_id, seq)
);

CREATE INDEX idx_rebalance_execution_events_execution ON rebalance_execution_events(execution_id);
