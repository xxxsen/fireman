-- td/061 §4.1.6：冻结的月度对数收益序列。
--
-- 联合风险模型的历史相关性估计需要每个标的快照的完整月度对数收益序列（仅完整自然年
-- 的连续月份）。该序列随快照一并冻结，使同一 run 的相关性可复现、可审计；run 输入快照
-- 中再以因子序列 hash 锁定，避免历史数据变化后旧 run 的相关性发生漂移。

CREATE TABLE instrument_simulation_snapshot_months (
  snapshot_id  TEXT    NOT NULL,
  year         INTEGER NOT NULL,
  month        INTEGER NOT NULL,
  log_return   REAL    NOT NULL,
  PRIMARY KEY(snapshot_id, year, month),
  FOREIGN KEY(snapshot_id) REFERENCES instrument_simulation_snapshots(id) ON DELETE CASCADE
);
