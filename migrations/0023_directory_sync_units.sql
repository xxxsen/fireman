-- 090: 资产目录同步从 scope 粒度拆为目录同步单元（sync_key）粒度。
-- market_asset_sync_state 由 scope 主键改为 sync_key 主键，scope 仅作为
-- UI 聚合分组字段。系统未上线，旧的 scope 行（cn_all/hk_all/us_all）直接
-- 丢弃；fx_rates 行结构不变（sync_key=scope=fx_rates），予以保留。

CREATE TABLE market_asset_sync_state_new (
  sync_key             TEXT PRIMARY KEY,
  scope                TEXT NOT NULL,
  last_task_id         TEXT NOT NULL DEFAULT '',
  last_success_task_id TEXT NOT NULL DEFAULT '',
  last_success_at      INTEGER,
  updated_at           INTEGER NOT NULL
);

INSERT INTO market_asset_sync_state_new
  (sync_key, scope, last_task_id, last_success_task_id, last_success_at, updated_at)
SELECT scope, scope, last_task_id, last_success_task_id, last_success_at, updated_at
FROM market_asset_sync_state
WHERE scope = 'fx_rates';

DROP TABLE market_asset_sync_state;
ALTER TABLE market_asset_sync_state_new RENAME TO market_asset_sync_state;

CREATE INDEX idx_market_asset_sync_state_scope
  ON market_asset_sync_state(scope);

-- 目录版本键从 asset_directory|{scope} 变为 asset_directory|{sync_key}；
-- 旧的 scope 级版本键清理，避免拆分后子任务被旧的高版本幂等跳过。
DELETE FROM market_data_versions
WHERE version_key IN (
  'asset_directory|cn_all',
  'asset_directory|hk_all',
  'asset_directory|us_all'
);
