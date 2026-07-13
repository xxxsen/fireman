# 市场数据质量、收益指标与模拟准入

> 资产数据统一来自全局市场资产目录：行情点位存于 `market_asset_points`，收益投影存于 `market_asset_detail_projections`，模拟输入使用 `market_asset_simulation_snapshots`；完整架构见 [021-market-data-task-worker-architecture.md](./021-market-data-task-worker-architecture.md)。

## 目的

Fireman 的资产目录、计划持仓和 FIRE 模拟共用同一套行情质量与收益指标口径。本文整理已实现的规则，避免目录展示、模拟快照和资产准入各自解释历史数据。

## 完整自然年

完整自然年是 FIRE 模拟历史样本的最小年度单位。一个年份必须同时满足：

- 年内有足够的月末点位，可以构造连续月度收益。
- 起止点覆盖完整年度边界；成立首年和当前未结束年份通常不计入完整年。
- 非连续完整年份可以同时保留，快照按实际可用年份取样，不要求年份连续。

模拟快照最多纳入最近 20 个完整自然年。历史不足时，资产仍保留在市场资产目录中，但会在模拟 readiness 检查中被拦截。

## 收益与风险指标

| 指标 | 口径 |
| --- | --- |
| CAGR | 基于完整年度区间的累计收益年化，不用缺失年度补零 |
| 月度收益序列 | 由月末值计算，作为波动率和模拟快照的基础序列 |
| 年化波动率 | 使用月度收益标准差乘以 `sqrt(12)`，不再用年度收益样本计算 |
| 最大回撤 | 基于点位序列计算峰谷回撤 |
| 近 1/3/5 年收益 | 以该标的自身最后可用交易日为终点，按实际起点累计/年化 |

系统现金和系统 FX 有内置零收益/汇率快照来源；市场资产通过 `market_asset_points` 计算指标并落入 `market_asset_detail_projections`。

## 资产详情投影

`market_asset_detail_projections` 是资产详情的预计算投影，按 `(asset_key, adjust_policy, point_type)` 存储：

- `annual_returns_json`：逐年收益（年份、收益率、起止日期与点位、观测数、是否不完整年 `is_partial`）；
- `trailing_returns_json`：近 1/3/5 年收益，以该标的自身最后可用交易日为终点。

历史同步任务的 post-process 在落库行情点位的同一事务内重算投影，详情页读取时不重算；全量替换（含来源切换）会先删除旧点位再写入并重算投影，替换后序列为空视为数据不完整而失败，投影写入失败会使整笔行情写入回滚。

## 模拟准入

计划持仓进入 FIRE 模拟前，由 `GET /api/v1/plans/:plan_id/simulation-readiness` 统一检查，快照试算（`BuildSnapshotForHolding`，不落库）是唯一准入标准——仅有历史点位（`point_count > 0`）不代表可模拟：

- 每个持仓的 `asset_key` 在 `market_assets` 中存在且可用；
- 无法通过快照试算的持仓进入 `blocking_assets`，按原因细分：`history_missing`（未同步历史）、`history_sync_running`（同步任务处于 `pending`、`running` 或等待 Go 侧结果落库的 `pre_complete`）、`simulation_insufficient_history`（历史已同步但完整年度不足）、`provider_data_anomaly`（历史已同步但指标/数据质量异常）；
- 持仓保存的完整 `asset_key` 是用户明确选择的资产身份。模拟准入、历史同步和快照构建只检查该身份自身的数据，不根据同代码目录记录推断或建议切换身份；同代码多身份仅在资产选择器中并列展示，由用户决定；
- readiness 未通过时模拟创建被 `market_asset_history_missing` 错误阻断（details 携带 `blocking_assets`）；
- 懒保存的持仓在 readiness 检查时试算快照（不落库）；模拟创建前再统一构建并持久化 `market_asset_simulation_snapshots`；
- 对外币资产，模拟快照必须能解析对应 FX 因子。

`POST /api/v1/plans/:plan_id/sync-missing-asset-history` 同样以快照试算为准入：可试算的资产返回 `ready`；仅对 `history_missing` 创建（或复用 active）`default_refresh` 历史同步任务；`history_sync_running` 归入 `existing`；`simulation_insufficient_history`/`provider_data_anomaly` 归入 `blocked` 并附原因，不再反复创建无效任务。不满足准入时在服务层返回明确错误，而不是让引擎在中途失败。

## API 与前端展示

- `GET /api/v1/market-assets` 返回目录列表并附带每个资产的历史就绪状态（已同步的数据截至日/点位数/来源，或最近同步任务的进行中/失败状态）。
- `GET /api/v1/market-assets/by-key` 返回资产详情：元信息、历史状态、行情点位与投影中的年度/区间收益。
- 资产详情页按最新年份优先展示年度收益，并基于本地点位渲染归一化累计收益曲线。

## 验证重点

- 月度收益、年化波动率、CAGR、最大回撤和近 1/3/5 年收益的纯函数测试。
- post-process 在同一事务内更新行情与投影；全量替换后空序列会使任务失败并回滚，不留下过期收益。
- 缺历史持仓被 readiness 拦截，`sync-missing-asset-history` 完成后可通过；已有历史但快照不可构建的持仓返回细分原因并在一键同步中 `blocked`，不创建新任务。
- 短历史、非连续年份、停更资产和系统现金/FX 均有稳定展示与准入结果。
