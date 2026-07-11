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
