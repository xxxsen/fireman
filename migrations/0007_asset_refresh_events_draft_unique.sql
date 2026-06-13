-- Asset refresh audit events + one active rebalance draft per plan.

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

-- At most one in-progress draft per plan (concurrent create guard).
CREATE UNIQUE INDEX idx_rebalance_drafts_one_active_per_plan
  ON rebalance_drafts(plan_id) WHERE status = 'draft';
