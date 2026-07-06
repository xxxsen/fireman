# 组合研究（Portfolio Research）

- 方案来源：`td/099-asset-selection.md`；实施 review 见 `td/100-td099-implementation-review.md`
- 定位：与「计划」「资产」平级的研究工作台——筛选资产、组建研究集合、运行确定性历史回测，并与 FIRE 计划双向复制。研究集合不直接改写任何计划持仓。

## 1. 信息架构

| 路由 | 页面 |
| --- | --- |
| `/research` | 首页：集合列表（含归档区）、最近运行、回测计算中、需要处理、JSON 导入 |
| `/research/screener` | 资产筛选器 + 候选池 + 候选比较；`?collection={id}` 时支持逐行「加入集合」 |
| `/research/collections/new` | 新建集合 |
| `/research/collections/{id}` | 集合编辑：基础参数 / 资产与权重 / 数据状态 / 回测入口四个工作区 |
| `/research/collections/{id}/runs` | 运行记录 |
| `/research/collections/{id}/runs/{runId}` | 回测结果详情 |

侧边栏在「计划」之后新增「组合研究」入口。

## 2. 资产筛选器与候选比较

- 数据完全来自本地：`market_assets` + `market_asset_history_state` + 预计算指标投影 `research_asset_metrics`（asset_key + adjust_policy + point_type 维度，含 CAGR、年化/下行波动率、最大回撤、Sharpe、Calmar、近 1/3/5 年收益、首末日期）。投影在历史 post-process 事务内更新，筛选查询前对缺失维度惰性补算。
- 筛选条件覆盖：市场、类型、关键词、历史数据状态、数据截至日（近 7/30/90 天预设 + 指定日期）、历史长度、收益/风险/风险收益指标（含下行波动率、收益回撤比）、币种、上市状态、是否可回测。
- 结果表支持列配置（默认列 + 交易所/点位数/来源/下行波动率/收益回撤比等可选列）、排序、分页、CSV 导出、数据质量徽标、逐行刷新历史/加入候选池/加入集合。
- 筛选条件可保存复用（`research_saved_filters` CRUD）。
- 候选比较：归一化收益曲线、回撤曲线、年度收益矩阵、指标对比（可按指标排序）、相关系数矩阵、平均相关性、CSV 导出、多选加入集合（有目标集合则逐项追加，否则等权新建）。历史数据复用资产详情 API 客户端计算。
- 候选池实时显示数量、币种分布、共同区间预估、平均相关性，可等权一键创建集合（尾项吸收舍入误差，权重合计恰为 1）。

## 3. 研究集合与权重编辑

- 集合字段：名称/描述/标签、基准币种（默认 CNY）、初始资金（默认 1,000,000）、再平衡规则、历史区间策略（共同区间 / 自定义区间）、可选基准资产、无风险利率、预留 `transaction_cost_rate`。
- 集合操作：新建、从筛选/候选池创建、从集合复制、从计划复制、复制到计划草稿、归档/恢复/硬删除、导入/导出 JSON。
- 权重编辑：直接输入、等权、按类别/市场等权、剩余分配、锁定后归一化、拖拽排序、禁用但保留、现金资产、逐资产 adjust_policy/point_type。
- 实时汇总：权重合计、币种/市场/资产类型暴露、单资产最大权重、缺历史与缺 FX 数量、共同区间预估。
- 权重校验统一 `1e-6` 容差。

## 4. readiness 与批量数据更新

- `GET /collections/{id}/readiness` 为回测唯一准入门禁。阻断条件：无启用资产、权重非法、缺历史、同步中/同步失败无旧数据、非正点位、多数据源混杂、FX 缺失/同步中/缺口超限、共同区间为空或 < 1 年、有效估值日不足、基准缺历史。警告条件：区间 < 3 年、数据过期（股票/ETF 7 天、场外基金 10 天）、停更资产、超容忍前值填充、滞后拖累共同终点、同步失败用旧数据、权重/市场/币种集中度、高相关对。现金资产免历史检查；inactive 不按过期阻断。
- 「更新组合数据」批量创建/复用 `asset_history_sync` 与 `fx_rate_sync` worker 任务（active 任务返回 `existed`），前端按资产逐行轮询 `GET /tasks/{id}`，失败给出错误码与建议，全部终态后自动刷新 readiness，支持单资产重试与强制刷新。

## 5. 回测引擎与不可变 run

- 引擎为纯函数（`engine_version = research_backtest_v1`）：自然日估值 + 前值填充（默认容忍 7 天、场外基金 14 天，不在上市前/末点后填充）；FX 转换（CNY 基准直连，其他经 CNY 交叉）且 FX 参与共同区间与缺口检查；再平衡支持 monthly/quarterly/yearly/buy_hold/fixed/threshold。
- 指标口径：CAGR（365.25/days）、有效估值日样本标准差 ×√252、负数回撤、Sharpe/Calmar 不可用时为 null、回撤持续期（当前/历史最长）、年度表（不完整年份标记、年内回撤与波动、年初/年末权重偏离）、月度收益、最好/最差年份与月份、正收益月份占比、单期贡献累计、方差分解风险贡献、回撤期贡献、相关性矩阵。
- run 生命周期：readiness 门禁 → 冻结 input snapshot（参数 + 各序列摘要）→ `source_hash` / `input_hash` → 同 `input_hash` 的 succeeded/active run 直接复用 → 事务内建 `jobs` 行（`research_backtest`）+ run 占位 → worker 执行时重载冻结输入并校验 source hash（漂移即失败）→ points/years/months + summary/data_quality 单事务落库。
- **序列摘要的最小闭包（td/100 Finding 1 修复）**：资产、基准与 FX 序列摘要不止取窗口内点位——若窗口起点当天无真实点位，则把窗口前最后一个点（forward-fill 锚点）一并纳入哈希，锚点日期以 `anchor_date` 写入快照并进入 `source_hash`。锚点之前的点位不影响哈希。由此保证：只改锚点价格必然产生新 run（不复用旧结果），且 freeze 后锚点漂移会被执行前校验拦截。

## 6. 结果展示与导出

- 总览指标卡（含 tooltip）：累计收益、CAGR、年化波动率、最大回撤、Sharpe、Calmar、最好/最差年份与月份、正收益月份占比、当前与历史最长回撤持续期。
- 收益/回撤双图共享时间轴、hover 联动（显示各资产贡献与权重）、最大回撤区间高亮、线性/对数切换、月度/日度切换、单资产归一化曲线显隐、空数据错误态。
- 年度收益表、月度热力图、滚动指标（12/36 个月收益、12 个月波动率、滚动回撤，由前端从 points 派生）、资产贡献表、相关性矩阵。
- 数据质量面板：共同区间及其成因、各序列原始/可用区间、填充统计、FX 区间、每序列 source / point_type / points_hash。
- 底部：运行输入快照查看、CSV/JSON 导出、与任意集合的 run 对比、复制参数生成新集合。

## 7. 与 FIRE 计划联动

- 从计划复制：持仓 current amount 折算权重（同资产多分组合并），基准币种继承计划。
- 复制到计划：校验 asset_class/region 完整性（缺失时对话框内补齐后重试），按初始资金折算目标金额，生成持仓草稿并跳转计划持仓校正流程；不直接写 `plan_holdings`。

## 8. 数据模型与 API

- migration `0025_research.sql`：`research_collections`、`research_collection_items`、`research_saved_filters`、`research_backtest_runs`（含 succeeded 部分唯一索引 `uq_research_backtest_runs_success_input`）、`research_backtest_points/years/months`、`research_asset_metrics`。
- API 全部挂 `/api/v1/research`，与 td/099 §5.3 路由表一一对应（assets、saved-filters、collections 及 items/normalize-weights/readiness/sync-history/backtests/runs、runs 详情/points/export.csv、copy-to-plan，另有 `GET /runs` 最近运行）。
- job 类型 `research_backtest` 纳入 worker 分发与 admin 控制台。

## 9. 测试

- Go：引擎单测（区间/填充/FX/各再平衡/全部指标/年度/回撤持续期/贡献）、readiness 全条件、sync-history 创建/复用/跳过、hash 稳定性与幂等（含锚点闭包回归：改锚点必换 hash、锚点前点位不影响 hash、锚点漂移被 source 校验拦截）、run 事务；`internal/api` 集成测试覆盖 td/099 §11.2。
- Web：Vitest 覆盖 §11.3 全部场景（导航、首页、筛选、候选池与比较、权重校验、锁定归一化、readiness、任务轮询、run 状态、图表、移动端标签、CSV/JSON 导入导出）。
- 手工验收（§11.4）依赖真实行情与 sidecar，由用户执行。
