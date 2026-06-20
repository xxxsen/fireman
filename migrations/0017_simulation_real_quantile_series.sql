-- td/061 §4.1.8：真实（起点购买力）月度财富分位序列。
--
-- 不向名义表 simulation_quantile_series 塞混合口径列；改用与之字段对齐的独立表，
-- 以 run_id + month_offset 为主键，并随 simulation_runs 级联删除。名义/真实分别成表，
-- 读取时按口径选择，避免误把真实分位当作名义分位。

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
