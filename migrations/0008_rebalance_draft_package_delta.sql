-- Reference rebalance package deltas frozen at draft creation (td/020).

ALTER TABLE rebalance_draft_lines
  ADD COLUMN recommended_package_delta_minor INTEGER NOT NULL DEFAULT 0;
