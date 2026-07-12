# 全栈审查修复与调仓入口收敛

## 目的

一次跨 web / golang / sidecar 的代码审查修复：纠正模拟引擎的两处统计口径错误，收敛多处非原子写入与竞争，统一 Web 端格式化、校验与共享组件，删除冗余实现，并将调仓入口从「调仓计划（draft）+ 调仓执行」双链路收敛为「调仓执行」单链路。

## 模拟引擎正确性

### Guardrail 提现策略跨年复利

- `WithdrawalPlanner` 的 guardrail 周年调整以**上一年支出 + 通胀**为基数（Guyton-Klinger 语义），±10% 上调/下调跨年累积，floor/ceiling 以通胀基线为参照截断。
- 引擎版本随语义变更升至 `3.1.0`。
- **回放按快照版本冻结语义**：`1.0.0` / `2.0.0` / `3.0.0` 快照回放时走 `legacyAnnualResetGuardrail`（每周年重置到通胀基线后做单次 ±10% 与截断），由 `GuardrailUsesLegacyAnnualReset(engine_version)` 在 `RunPath` 内门控。`RunPath` 是主模拟、压力测试、敏感性分析与路径详情再生的唯一入口，历史 run 的路径详情与已存 summary 保持一致。
- 回归防护：`guardrail_replay_test.go` 以真实 3.0.0 引擎代码跑出的 golden 断言逐位复现，并断言同一快照在当前版本下必须偏离 golden（防门控泄漏）。

### medianInt64

偶数长度取中间两元素均值（整数截断语义），不再偏取上位元素。

## 数据一致性

### 计划设置单事务保存

- `PUT /api/v1/plans/:plan_id/settings`：计划名、资产配置目标、FIRE 参数在**一个事务**内保存，`config_version` 只做**一次** CAS（`BumpVersionTx`）；任一校验或写入失败整体回滚，客户端可用原版本号重试。
- 前端参数页单次调用 `updatePlanSettings`，不再链式调用三个接口、不再客户端推算版本号。
- 保存前校验计划名 trim 后非空（后端对空名是 patch 语义静默忽略）。

### 调仓执行 Complete 全事务化

`completeExecutionTx` 的读取链路（execution 行、lines、holdings、快照构建）全部走事务内 Tx 变体，检查与提交不再分离。并发回归：Sell×Complete 竞争（事件流回放对账）、双 Complete 恰一成功。

### 分析页任务状态重建

分析页从持久化记录（simulations / stress / sensitivity 列表）重建运行中任务的进度与取消入口，刷新不丢失：

- sim 只考察**最新** run（`simulations[0]`）：新 run 创建时会 supersede 取消旧任务，旧的无 summary run 是已终局的失败，不再被重新挂接、不再反复弹出历史失败横幅；
- stress / sensitivity 按 `status ∈ {queued, running}` 过滤；
- 用户刚启动的任务优先，列表重建不覆盖。

### sidecar 两段式 claim

`TaskDB.claim_next` 先做**无事务只读探测**（`SELECT 1 … WHERE status='pending'`），队列为空直接返回，不再每个轮询周期无条件 `BEGIN IMMEDIATE` 抢 SQLite RESERVED 写锁与 Go 进程竞争；探测命中后进入写事务，事务内复查保持原 CAS 语义（探测到但被其他 worker 抢走时返回 `None`，下轮重试）。pytest 用 `set_trace_callback` 断言空队列零写事务、有任务正常 claim、被抢走时优雅返回。

## Web 一致性

| 项 | 说明 |
| --- | --- |
| 格式化统一 | 金额/日期一律走 `web/lib/format.ts`；`format-guard.test.ts` 扫描生产 tsx 禁止裸 `toLocaleString`（图表 tooltip 白名单） |
| 资产选择器统一 | 共享核心 `MarketAssetSearchPicker` + 弹窗封装 `MarketAssetPickerDialog`（防抖搜索、市场/类型过滤、无限分页、身份冲突提示），持仓校正与向导复用 |
| 壳路由统一 | `ParametersContent` / `AnalysisContent` 归位 `web/components/plans/settings/`；`holdings` / `scenarios` / `parameters` / `analysis` 壳页面统一 Server Component `redirect`（holdings 透传 query） |
| 共享参数校验 | `web/lib/plan-validation.ts`（年龄双口径、金额为正），向导与参数页共用，非法输入不发请求 |

## 冗余清理与唯一事实来源

| 项 | 说明 |
| --- | --- |
| factor model 装配 | 删除 `BuildFactorModel` / `CorrelationPriorLookup` / `buildRawCorrelation`；`AssembleFactorModel` 为 `AssembleFactorModelDetailed` 的薄委托 |
| instrument type | `internal/service/instrument_type.go` 是标签与排序优先级的唯一事实来源，API 响应携带 `instrument_type_label` / `instrument_type_priority`，前端不再维护副本 |
| `rebalanceToTarget` | 50 次迭代死循环改为等价闭式（首轮交易量计费后按目标权重分配）；随机对拍 + 性质测试 + RunPath golden 钉死逐位等价，无需 bump 引擎版本 |
| `CanonicalJSON` 契约 | `input_hash` 稳定性依赖 struct 声明序 + map key 排序；契约测试反射遍历 `InputSnapshot` 类型树，禁止 interface 字段与非 string key map |

## 调仓入口收敛

「调仓计划（draft）」链路整体下线，全栈删除（前端页面/API 客户端/类型/词条、后端 handler/service/repository/domain、集成测试）：

- 调仓工作台仅剩两个入口：**持仓校正**（asset-refresh，事实修正）与**调仓执行**（executions，向目标结构收敛）；
- `POST /plans/:plan_id/asset-refresh` 迁至主 handlers，行为不变；
- `rebalance_drafts` / `rebalance_draft_lines` / `rebalance_draft_events` 三表已从 `migrations/0001_init.sql` 完整基线移除；
- 共享助手（`amountToleranceMinor` / `formatWan` / `findCashSweepHolding` / `maxStructuralGapWeight`）迁至执行链路文件。

用户路径见 [007-asset-refresh-rebalance-plan.md](./007-asset-refresh-rebalance-plan.md)，执行模型见 [018-rebalance-planning-and-execution.md](./018-rebalance-planning-and-execution.md)，历史背景见 [005-structural-scale-rebalance.md](./005-structural-scale-rebalance.md)。

## 回放兼容性契约（汇总）

历史 run 的可复现性由快照冻结字段与版本门控共同保证：

- 采样器：由快照冻结的 `RandomFactorModel` / `FactorModel` 字段选择（独立 vs 联合），与版本字符串无关；
- 尾部参数：快照冻结的 `TailStudentTDf` / `TailReturnFloor` / `TailReturnCeil`，legacy 快照回退常量；
- 现金收益：`DeterministicCashReturn` 冻结于快照；
- guardrail 语义：按 `engine_version` 门控（≤3.0.0 走周年重置，≥3.1.0 走跨年复利）。

## 验证

```bash
go vet ./... && go test ./...
cd web && npm test && npm run lint
cd sidecars/market-provider && uv run pytest
```
