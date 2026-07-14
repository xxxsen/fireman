# 组合研究（Portfolio Research）

- 状态：已实施
- 当前回测版本：`research_backtest_v4`
- 定位：在不修改 FIRE 计划的前提下，维护研究集合、准备本地历史数据、运行确定性历史回测与自动调优；只有用户核对替换预览并明确确认后，研究集合才会原子写入目标计划。

## 1. 当前用户路径

| 路由 | 页面与职责 |
| --- | --- |
| `/research` | 集合列表、归档区、最近回测、需要处理的集合、从计划复制、JSON 导入 |
| `/research/collections/new` | 创建空研究集合 |
| `/research/collections/{id}` | 编辑参数、资产、权重与分类，检查数据状态，启动回测或自动调优，导出 JSON，预览应用到计划 |
| `/research/collections/{id}/runs` | 查看该集合的历史回测运行 |
| `/research/collections/{id}/runs/{runId}` | 查看不可变回测结果、数据质量、冻结输入与 CSV 导出 |
| `/research/collections/{id}/optimizations/{optimizationId}` | 查看自动调优进度、四类 Top K 结果并选择一项应用 |

组合研究当前不提供独立资产筛选器、候选池或候选比较页面。添加资产统一在集合详情页完成，数据来自本地市场资产目录；对应的基础接口为 `GET /api/v1/research/assets`。

## 2. 研究集合

集合保存以下稳定配置：

- 名称、描述、标签、状态；
- 基准币种、初始资金；
- 再平衡规则与阈值；
- 共同区间或自定义回测区间；
- 可选基准资产、无风险利率、交易成本率；
- CVaR 置信度与持有期，默认 `95% / 20 个有效收益日`。

集合条目保存资产、启用状态、目标权重、权重锁定状态、复权口径、点位类型、资产类别、地区和备注。普通回测只使用启用且正权重的资产，并要求权重合计在 `1e-6` 容差内等于 100%；自动调优有独立的准入规则，允许未锁定条目暂时为零权重或集合尚未配平。

集合可通过以下方式建立或维护：

- 创建空集合后搜索并添加资产；
- 从现有 FIRE 计划或研究集合复制；
- 导入版本化 JSON；
- 归档、恢复或明确执行硬删除。

当前 JSON 导入/导出格式包含集合基本参数、标签和条目，不包含历史 run、worker task 或 CVaR 置信度/持有期。

## 3. 数据准备与 readiness

研究计算只读取本地市场历史与 FX 数据，不在回测引擎中访问外网。`GET /api/v1/research/collections/{id}/readiness` 是普通回测的唯一准入门禁，主要检查：

- 启用资产和权重是否合法；
- 所需资产历史与 FX 是否存在、是否仍在同步、是否包含不可用点位或超限连续缺口；
- 共同可用区间或自定义区间是否有效，是否至少一年且包含足够有效估值日；
- 基准资产及其 FX 能否完整覆盖最终窗口；
- 当前 CVaR 置信度和持有期是否拥有足够场景样本。

短于三年的区间、数据过期、集中度、高相关性和容忍范围内的前值填充等事实作为 warning 展示。现金资产不要求价格历史；非现金资产不会在真实末点之后填平尾部。

“更新组合数据”通过统一任务体系创建或复用 `asset_history_sync` 与 `fx_rate_sync` 任务。前端跟踪任务终态并刷新 readiness，失败时保留稳定错误码和重试入口。

## 4. 确定性回测

`research_backtest_v4` 是无数据库依赖的纯计算引擎。服务层先冻结集合、资产历史、FX、基准与配置，再将冻结输入交给 worker。核心口径为：

- 将资产点位转换到集合基准币种，FX 缺口参与窗口和数据质量判断；
- 在共同可用窗口内使用有界前值填充，场外基金与其他资产采用各自容忍天数；
- 支持 `monthly`、`quarterly`、`yearly`、`buy_hold`、`fixed`、`threshold` 六种再平衡规则；阈值策略在任一资产偏离达到 `>=` 阈值时触发，阈值为 0 时不触发；
- v4 在有效估值日收益之后、恢复目标权重之前按单边换手扣除交易成本，并以初始资金换算到 minor unit 后确定性舍入；
- 基于同一条扣费后的 NAV/收益路径计算累计收益、CAGR、波动率、最大回撤、Sharpe、Calmar、年度/月度结果、贡献归因、相关性、换手与交易成本拖累；
- 使用 `empirical_cvar_v1` 对重叠持有期收益计算 VaR、CVaR 和最差损失；样本不足直接由 readiness 阻断，不自动放宽口径。

贡献归因必须满足：资产累计贡献之和等于组合累计收益，峰谷回撤贡献之和等于最大回撤，非零方差时风险贡献之和等于 1。

详细的版本演进、交易成本时序和 CVaR 公式分别见 [026-portfolio-research-and-simulation-logic-corrections.md](./026-portfolio-research-and-simulation-logic-corrections.md) 与 [030-research-cvar-optimization.md](./030-research-cvar-optimization.md)。

## 5. 不可变 run 与任务状态

创建回测时，服务端冻结输入快照并计算 `source_hash` 与 `input_hash`：

1. `source_hash` 描述实际使用的资产、基准和 FX 数据；窗口起点依赖前值填充时，起点前最后一个锚点也进入摘要和哈希。
2. `input_hash` 同时覆盖计算配置和引擎版本；版本变化不会复用旧结果。
3. 相同集合与 `input_hash` 的已完成或活动 run 可直接复用。
4. 新 run 与 `research_backtest` worker task 在同一事务内创建。
5. worker 执行前重新校验冻结来源；来源漂移时失败，不以新数据悄悄替换快照。
6. 结果与任务终态通过统一 finalize 流程收敛；取消中的任务不得发布成功结果。

`research_backtest_runs` 不单独持久化生命周期状态。API 视图以关联 `worker_tasks` 的状态为准，并统一暴露 pending、running、complete、failed、canceled 等状态。任务跟踪、恢复与取消契约见 [031-unified-worker-task-architecture.md](./031-unified-worker-task-architecture.md)、[034-async-task-tracking-and-recovery.md](./034-async-task-tracking-and-recovery.md) 和 [035-worker-task-cancellation.md](./035-worker-task-cancellation.md)。

## 6. 结果与导出

回测详情展示：

- 累计收益、CAGR、波动率、最大回撤、Sharpe、Calmar；
- VaR、CVaR、最差持有期损失和对应样本口径；
- NAV、收益、回撤、年度收益、月度热力图与滚动指标；
- 资产累计贡献、回撤贡献、风险贡献和相关性矩阵；
- 换手、交易成本、基准比较与数据质量；
- 引擎版本、冻结输入快照和来源摘要。

CSV 导出使用 run 的不可变结果，不重新运行计算。历史旧版本 run 继续只读展示；旧版本没有的指标显示为不可用，不根据当前引擎补算。

## 7. 自动调优

集合详情页的“寻找最优组合”使用独立 readiness 和 `research_optimization_backtest` task。当前优化器为 `research_optimizer_v6`，在同一冻结窗口、有效收益日、CVaR 口径和扣费回测引擎上枚举离散权重，产出：

- 最高 CAGR；
- 最低最大回撤；
- 最高 Calmar；
- 最低 CVaR。

调优结果只在用户确认后原子应用回研究集合，同时同步权重、启用/锁定状态、回测窗口和当前版本支持的 CVaR 口径。完整规则见 [025-research-portfolio-auto-optimization.md](./025-research-portfolio-auto-optimization.md) 与 [030-research-cvar-optimization.md](./030-research-cvar-optimization.md)。

## 8. 与 FIRE 计划联动

从计划复制时，当前持仓金额按资产合并后折算为研究权重，并继承计划基准币种。

从研究集合应用到计划必须先调用：

```http
POST /api/v1/research/collections/{id}/plan-preview
```

预览会验证正权重资产、`asset_class`、`region`、本地资产有效性和权重，并按目标计划当前 `total_assets_minor` 生成完整大类/地区配置、目标持仓、删除清单、舍入调整、`config_version` 与 replacement hash。用户确认后调用：

```http
POST /api/v1/research/collections/{id}/apply-to-plan
```

服务端在一个事务内重新构建并校验替换结果，完整替换 allocation 与 holdings，创建组合快照，并把计划 `config_version` 恰好增加一次。计划版本或集合内容在预览后变化时返回冲突，要求重新预览。旧 `copy-to-plan` 接口固定返回 410，不能作为写入入口。

## 9. 数据模型与 API 边界

组合研究业务表位于 `migrations/0001_init.sql`，当前运行时使用：

- `research_collections`、`research_collection_items`；
- `research_backtest_runs`、`research_backtest_points`、`research_backtest_years`、`research_backtest_months`；
- `research_optimization_runs`；
- `research_asset_metrics`；
- 统一的 `worker_tasks` 及其尝试、finalize 记录。

基线 schema 仍保留 `research_saved_filters` 旧表，但当前没有页面、API、service 或 repository 读写该表；它不代表资产筛选器仍是可用功能。若未来彻底删除该表，应通过新的顺序 migration 处理旧数据库升级，而不是修改运行时代码进行 schema repair。

组合研究 API 全部挂在 `/api/v1/research`，覆盖本地资产搜索、集合及条目 CRUD、readiness、历史同步、回测、run 查询与导出、自动调优，以及计划预览/应用。不存在 saved filters、候选池或候选比较 API。

## 10. 验证不变量

实现和后续修改必须持续覆盖：

- 相同冻结输入和版本生成相同哈希与结果，来源漂移不会复用旧 run；
- 普通回测权重、共同窗口、FX、基准覆盖和 CVaR 样本门禁一致；
- 交易成本、贡献归因和 CVaR 使用同一条实际组合路径；
- run 状态与 worker task 终态收敛，取消后不发布结果；
- 调优候选共享有效日历和计算口径，应用结果为单事务；
- 应用到计划必须经过 replacement hash 与 `config_version` 双重并发校验；
- 前端不暴露已移除的筛选器或 saved filters 入口。

仓库级验证使用：

```bash
make ci
```
