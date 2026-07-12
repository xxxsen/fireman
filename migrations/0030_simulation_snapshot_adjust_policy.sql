ALTER TABLE market_asset_simulation_snapshots
  ADD COLUMN adjust_policy TEXT NOT NULL DEFAULT '';

UPDATE market_asset_simulation_snapshots
SET adjust_policy = 'none'
WHERE asset_key LIKE 'SYS|cash||%';
