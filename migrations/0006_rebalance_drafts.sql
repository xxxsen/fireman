-- Rebalance plan drafts: frozen baseline + staged edits (td/019).

CREATE TABLE rebalance_drafts (
  id                            TEXT    PRIMARY KEY,
  plan_id                       TEXT    NOT NULL,
  status                        TEXT    NOT NULL DEFAULT 'draft',
  config_version                INTEGER NOT NULL,
  baseline_holdings_total_minor INTEGER NOT NULL,
  created_at                    INTEGER NOT NULL,
  updated_at                    INTEGER NOT NULL,
  committed_at                  INTEGER,
  note                          TEXT    NOT NULL DEFAULT '',
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

CREATE INDEX idx_rebalance_drafts_plan_status ON rebalance_drafts(plan_id, status);

CREATE TABLE rebalance_draft_lines (
  id                            TEXT    PRIMARY KEY,
  draft_id                      TEXT    NOT NULL,
  holding_id                    TEXT    NOT NULL,
  instrument_id                 TEXT    NOT NULL,
  baseline_current_minor        INTEGER NOT NULL,
  planned_current_minor         INTEGER NOT NULL,
  frozen_target_minor           INTEGER NOT NULL,
  frozen_gap_minor              INTEGER NOT NULL,
  frozen_gap_weight             REAL    NOT NULL,
  frozen_action                 TEXT    NOT NULL,
  frozen_suggested_trade_minor  INTEGER NOT NULL,
  last_saved_at                 INTEGER,
  FOREIGN KEY(draft_id) REFERENCES rebalance_drafts(id) ON DELETE CASCADE
);

CREATE INDEX idx_rebalance_draft_lines_draft ON rebalance_draft_lines(draft_id);

CREATE TABLE rebalance_draft_events (
  id           TEXT    PRIMARY KEY,
  draft_id     TEXT    NOT NULL,
  seq          INTEGER NOT NULL,
  event_type   TEXT    NOT NULL,
  payload_json TEXT    NOT NULL DEFAULT '{}',
  created_at   INTEGER NOT NULL,
  FOREIGN KEY(draft_id) REFERENCES rebalance_drafts(id) ON DELETE CASCADE,
  UNIQUE(draft_id, seq)
);

CREATE INDEX idx_rebalance_draft_events_draft ON rebalance_draft_events(draft_id);
