# 100 td/099 实施 Review

## Review 范围与方法

- 方案文档：`td/099-asset-selection.md`。
- 只做实现 review，不修改业务代码。
- 核对范围：
  - 数据模型与迁移：`migrations/0025_research.sql`、`internal/repository/research.go`；
  - 后端服务：组合研究 readiness、筛选、集合、回测、任务执行、复制到计划草稿；
  - API：`/api/v1/research/*`；
  - 前端：`/research`、`/research/screener`、集合详情、回测结果页；
  - 配套测试：Go service/API/repository tests 与 Web component/page tests。
- 已执行回归：
  - `go test ./...`：通过；
  - `cd web && npm run test:ci`：通过（76 个测试文件 / 550 条用例）；
  - `cd web && npm run build`：通过。

**结论：td/099 已完成主要产品与工程链路，但存在 1 个 P1 正确性问题。当前不满足整理输出到 `docs/` 的条件；应先修复下方 Finding，再进入下一轮 review。**

---

## 实现覆盖核对

| 模块 | 状态 | 证据 |
| --- | --- | --- |
| 信息架构与页面入口 | 已实现 | 侧边栏新增组合研究入口；前端新增 `/research`、`/research/screener`、`/research/collections/*`、`/research/collections/*/runs/*` |
| 资产筛选器与保存筛选条件 | 已实现 | API 覆盖 `/research/assets`、`/research/saved-filters`；前端筛选、保存、复用筛选条件已接入 |
| 候选比较 | 已实现 | 支持候选池、归一化曲线、回撤、年度矩阵、指标、相关系数与 CSV 导出 |
| 研究集合 CRUD 与权重编辑 | 已实现 | 后端集合/条目 CRUD、归一化权重、复制集合能力完整；前端支持集合详情、权重与禁用状态编辑 |
| readiness 与批量同步 | 已实现 | 后端检查历史、FX、权重、共同窗口、缺口、过期等阻断/告警；`sync-history` 复用 worker task |
| 组合回测与任务队列 | 基本已实现 | `research_backtest` job 已接入 worker；回测结果不可变存储，支持复用 succeeded/active run |
| 指标、曲线、导出 | 已实现 | 组合点位、资产贡献、年度收益、风险指标、CSV 导出均有实现 |
| 复制到 FIRE 计划 | 已实现 | 生成计划持仓草稿，不直接改写现有计划持仓，符合 td/099 边界 |
| 数据指标预计算 | 已实现 | 历史后处理事务内更新 `research_asset_metrics`，筛选器复用本地指标 |

---

## Findings

### Finding 1（P1 · 正确性 / 审计）`source_hash` 与 `input_hash` 漏掉窗口起点前的 forward-fill 锚点

**位置**：

- `internal/service/research_service.go`：`CreateBacktest` 通过 `computeResearchInputHash` 复用已有 successful/active run；
- `internal/service/research_service.go`：`summarizeAssetSeries`、`summarizeFXSeries` 只汇总 `winLo <= trade_date <= winHi` 的点位；
- `internal/service/research_backtest.go`：`preparedSeries.valueAt(day)` 使用 `last observation <= day` 的前值填充口径。

**问题说明**：

td/099 要求“每次回测生成不可变结果，结果与当时的权重、参数、资产点位 source hash、FX source hash 绑定，后续行情刷新不会改写旧结果”。当前实现的哈希口径没有完整覆盖实际参与估值的数据。

回测引擎在窗口起点 `winLo` 当天估值时，会用 `winLo` 之前最后一个点位作为 forward-fill 锚点；但 `summarizeAssetSeries` 和 `summarizeFXSeries` 会过滤掉 `trade_date < winLo` 的点位。结果是：如果某资产或 FX 在共同窗口起点当天没有真实点位，修改窗口起点之前的最后一个点位会改变回测结果，但 `source_hash` / `input_hash` 不变。

这会导致两类错误：

1. `CreateBacktest` 可能错误复用旧的 succeeded run，返回与当前行情事实不一致的结果；
2. job 执行前后的 `source_hash` 校验无法发现这类数据漂移，削弱不可变回测的审计保证。

**修复方案**：

统一修改 source snapshot 的序列摘要口径：对资产、基准资产和 FX 序列，纳入“实际估值会使用的最小闭包”。

具体规则：

1. 对每条已按日期排序的序列，定位 `winLo` 前后边界：
   - 若存在 `trade_date <= winLo` 的点位，则取其中最后一个作为 anchor；
   - 若不存在 `trade_date <= winLo` 的点位，则从第一个 `winLo <= trade_date <= winHi` 的点位开始；
   - 之后连续纳入所有 `trade_date <= winHi` 的点位。
2. `summarizeAssetSeries` 和 `summarizeFXSeries` 使用同一套选择规则；基准资产继续复用资产序列摘要。
3. `computeResearchSourceHash` 必须写入 anchor 的日期与值。现有 `FirstDate` 可表示实际纳入摘要的首个点位日期；如果需要区分共同窗口起点和锚点日期，则新增 `AnchorDate`，但哈希中必须包含 anchor。
4. 不改变回测数学口径，只修正冻结和复用时的 source 摘要口径。

**验收逻辑**：

1. 新增 service 单元测试：构造两个资产共同窗口，其中资产 A 在 `common_start` 当天无点位，但在 `common_start` 前一天有点位且会被 forward-fill 使用。先生成 snapshot/hash，再只修改该前置锚点价格，重新生成 snapshot/hash；断言 `source_hash` 与 `input_hash` 均发生变化。修复前该测试应失败。
2. 新增 API 或 service 集成测试：第一次回测成功后，只修改窗口起点前的 forward-fill 锚点，再次创建回测；断言不会复用旧 run（`reused=false`），而是创建新的 run，或在 freeze 后执行前发生数据漂移时被 source 校验拦截。
3. 保持现有回归通过：`go test ./...`、`cd web && npm run test:ci`、`cd web && npm run build`。

---

## 结论与下一步

1. 先修复 Finding 1，并补充上述“窗口前锚点参与哈希”的回归测试；
2. 修复后重新执行 Go 与 Web 全量回归；
3. 下一轮 review 若无 P0/P1/P2 finding，再将 td/099 的最终产品与工程说明整理输出到 `docs/`。
