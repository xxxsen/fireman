-- td/061 阶段 B：全局“模拟假设” profile 系统与计划收益率假设选择。
--
-- 任何适用于全部 FIRE 计划的数值（资本市场先验、收缩强度、相关性先验、波动率
-- 边界、情景定义、厚尾自由度）必须由全局版本化 profile 统一管理；计划行只保存
-- “跟随全局 / 固定某个 profile + 情景 + 模式”的选择，绝不复制全局数值。
--
-- canonical JSON 是模拟读取时的唯一真相；规范化投影表仅供列表查询与表单校验。
-- (id, version) 唯一；被 run 引用的 active 版本绝不可 UPDATE，只能新建版本。

CREATE TABLE simulation_assumption_profiles (
  id              TEXT    NOT NULL,
  version         INTEGER NOT NULL,
  owner_scope     TEXT    NOT NULL DEFAULT 'user',   -- system | user
  name            TEXT    NOT NULL DEFAULT '',
  status          TEXT    NOT NULL DEFAULT 'draft',   -- draft | active | superseded
  canonical_json  TEXT    NOT NULL,
  content_hash    TEXT    NOT NULL,
  source_note     TEXT    NOT NULL DEFAULT '',
  reviewed_by     TEXT    NOT NULL DEFAULT '',
  reviewed_at     TEXT    NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  PRIMARY KEY (id, version)
);

CREATE INDEX idx_assumption_profiles_status ON simulation_assumption_profiles(id, status);

-- 规范化投影（写入 profile 时与 canonical JSON 在同一 transaction 内生成）。
CREATE TABLE simulation_assumption_scenarios (
  profile_id            TEXT    NOT NULL,
  profile_version       INTEGER NOT NULL,
  scenario              TEXT    NOT NULL,
  return_shift_log      REAL    NOT NULL DEFAULT 0,
  return_shift_log_fx   REAL    NOT NULL DEFAULT 0,
  volatility_multiplier REAL    NOT NULL DEFAULT 1,
  PRIMARY KEY (profile_id, profile_version, scenario),
  FOREIGN KEY (profile_id, profile_version)
    REFERENCES simulation_assumption_profiles(id, version) ON DELETE CASCADE
);

CREATE TABLE simulation_assumption_return_priors (
  profile_id               TEXT    NOT NULL,
  profile_version          INTEGER NOT NULL,
  asset_class              TEXT    NOT NULL,
  region                   TEXT    NOT NULL,
  valuation_currency       TEXT    NOT NULL,
  annual_geometric_return  REAL    NOT NULL,
  annual_volatility_floor  REAL    NOT NULL DEFAULT 0,
  annual_volatility_ceiling REAL   NOT NULL DEFAULT 0,
  source_url               TEXT    NOT NULL DEFAULT '',
  published_at             TEXT    NOT NULL DEFAULT '',
  reviewed_at              TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (profile_id, profile_version, asset_class, region, valuation_currency),
  FOREIGN KEY (profile_id, profile_version)
    REFERENCES simulation_assumption_profiles(id, version) ON DELETE CASCADE
);

CREATE TABLE simulation_assumption_correlation_priors (
  profile_id      TEXT    NOT NULL,
  profile_version INTEGER NOT NULL,
  factor_a        TEXT    NOT NULL,
  factor_b        TEXT    NOT NULL,
  rho             REAL    NOT NULL DEFAULT 0,
  PRIMARY KEY (profile_id, profile_version, factor_a, factor_b),
  FOREIGN KEY (profile_id, profile_version)
    REFERENCES simulation_assumption_profiles(id, version) ON DELETE CASCADE
);

-- 单行偏好：当前本地用户的全局默认 profile / version / scenario。
-- 记录缺失时解析为系统 system_cma_v1/baseline。
CREATE TABLE simulation_assumption_preferences (
  id                     INTEGER PRIMARY KEY CHECK (id = 1),
  default_profile_id     TEXT    NOT NULL DEFAULT '',
  default_profile_version INTEGER NOT NULL DEFAULT 0,
  default_scenario       TEXT    NOT NULL DEFAULT 'baseline',
  updated_at             INTEGER NOT NULL DEFAULT 0
);

-- 计划收益率假设选择（不复制全局数值）。
ALTER TABLE plan_parameters ADD COLUMN return_assumption_mode TEXT NOT NULL DEFAULT 'historical_cagr';
ALTER TABLE plan_parameters ADD COLUMN assumption_selection_mode TEXT NOT NULL DEFAULT 'follow_global';
ALTER TABLE plan_parameters ADD COLUMN return_assumption_set_id TEXT NOT NULL DEFAULT '';
ALTER TABLE plan_parameters ADD COLUMN return_assumption_set_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE plan_parameters ADD COLUMN return_assumption_scenario TEXT NOT NULL DEFAULT 'baseline';
ALTER TABLE plan_parameters ADD COLUMN custom_return_assumptions_json TEXT NOT NULL DEFAULT '';
