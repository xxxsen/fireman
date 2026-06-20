-- td/061 §4.1.5：资产级（计划特异）前瞻收益/波动率 override。
--
-- 仅当确有计划特异事实（例如锁定到期收益率的持有至到期债券）时使用；键为
-- (plan_id, instrument_id)，必须写明 reason 与 expires_at，且只能覆盖前瞻几何收益率
-- 与前瞻波动率，绝不能覆盖历史事实、相关性先验或 FX 共同因子。到期（expires_at 早于
-- 计划估值日）后自动忽略，回退到全局 profile 校准值。随 plan 级联删除。

CREATE TABLE plan_return_assumption_overrides (
  plan_id            TEXT    NOT NULL,
  instrument_id      TEXT    NOT NULL,
  forward_return     REAL,
  annual_volatility  REAL,
  reason             TEXT    NOT NULL,
  expires_at         TEXT    NOT NULL,
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL,
  PRIMARY KEY(plan_id, instrument_id),
  FOREIGN KEY(plan_id) REFERENCES plans(id) ON DELETE CASCADE
);
