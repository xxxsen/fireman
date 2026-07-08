# td/103 研究组合自动调优实现 Review

## Review 结论

当前实现已经覆盖了 td/103 的主体框架：候选权重生成、三类目标排序、optimization run 数据表、创建/查询 API、worker 执行入口、前端“寻找最优组合”入口、配置弹窗和结果页均已落地。

但实现尚未达到可验收状态。主要问题集中在前端质量门禁失败、普通回测 UI 回归、`max_candidate_count` 未按请求配置生效，以及管理后台 job 类型遗漏。结论：**不应视为 td/103 完整实现，需返工后再验收**。

## Findings

### 1. 前端 lint 失败，当前代码无法通过既有质量门禁

严重级别：High

证据：

- `web/components/research/OptimizationConfigDialog.tsx:47-52`
- `web/components/research/BacktestPanel.tsx:232`

执行结果：

```text
cd web && npm run lint

OptimizationConfigDialog.tsx:49:7
Error: Calling setState synchronously within an effect can trigger cascading renders

BacktestPanel.tsx:232:30
warning  'step' is defined but never used
```

影响：

- `npm run lint -- --max-warnings=0` 失败会阻断前端 CI。
- 当前实现不能进入可发布状态。

修复方案：

- 删除 `OptimizationConfigDialogProps.onWeightStepChange` 及父组件中对应未使用回调。
- 删除 `useEffect` 中关闭弹窗时同步 `setWeightStep/setTopK` 的写法。
- 将默认值重置改为更简单的 mount/key 策略：由父组件在打开弹窗时通过 `key={optDialogOpen ? "open" : "closed"}` 或递增 `dialogVersion` 重新挂载 `OptimizationConfigDialog`；组件内部只用 `useState(0.05)` 和 `useState(20)` 初始化，不用 effect 重置。

验收逻辑：

```bash
cd web && npm run lint
```

必须 0 error、0 warning。

### 2. 普通回测禁用原因被调优就绪状态覆盖，已有测试失败且用户看不到普通回测失败原因

严重级别：High

证据：

- `web/components/research/BacktestPanel.tsx:197-205`
- `web/components/research/BacktestPanel.test.tsx:183-188`

执行结果：

```text
cd web && npm run test:ci

BacktestPanel > disables the run button with the reason when blocked
Unable to find an element by: [data-testid="run-disabled-reason"]
```

原因：

当前渲染逻辑是：

```tsx
{disabledReason && !optDisabledReason && <p data-testid="run-disabled-reason">...}
{optDisabledReason && <p data-testid="opt-disabled-reason">...}
```

当普通回测不可运行，同时自动调优 readiness 还在检查或不可运行时，普通回测原因会被隐藏。td/103 只要求新增自动调优入口，不应破坏普通回测已有解释。

影响：

- 既有 BacktestPanel 测试失败。
- 用户点击普通“运行回测”被禁用时，看不到普通回测为什么不可运行。

修复方案：

- 普通回测原因和自动调优原因分别渲染，互不覆盖。
- 推荐将两个按钮及各自原因分成两个相邻的小区域：
  - `运行回测` 下只展示 `disabledReason`。
  - `寻找最优组合` 下只展示 `optDisabledReason`。
- 更新测试 mock 新增 optimization API，并新增断言：普通回测和自动调优同时 disabled 时两个原因都可见。

验收逻辑：

```bash
cd web && npm run test:ci -- components/research/BacktestPanel.test.tsx
cd web && npm run test:ci
```

必须全部通过。

### 3. `max_candidate_count` 请求参数未生效，后端只按硬上限 20000 校验

严重级别：Medium

证据：

- `internal/service/research_optimization.go:31-35`
- `internal/service/research_optimization.go:39-48`
- `internal/service/research_optimization_service.go:293-300`
- `internal/service/research_optimization_service.go:169-176`
- `internal/service/research_optimization_service.go:364`

问题：

td/103 定义 `max_candidate_count` 是“本次允许评估的最大候选数，默认 20000，不能超过服务端硬上限”。当前请求会进入 `OptimizationConfig.MaxCandidateCount`，但 readiness 和 create 都只使用 `OptimizationHardMaxCandidate`。如果调用方传入：

```json
{ "weight_step": 0.05, "max_candidate_count": 100 }
```

而实际候选数为 1000，当前实现仍会创建并执行任务。

影响：

- 用户配置的候选数量上限无效。
- 前端或未来调用方无法用该参数控制单次任务规模。
- `input_hash` 会包含 `max_candidate_count`，但执行行为不受它控制，形成审计语义不一致。

修复方案：

- 将 `evaluateOptimizationReadiness` 改为接收完整 `OptimizationConfig`，而不是只接收 `weightStep`。
- 计算候选数后使用 `config.MaxCandidateCount` 校验：

```go
if out.CandidateCount > config.MaxCandidateCount {
    block(ResearchReadinessIssue{
        Reason: "candidate_count_exceeds_limit",
        Message: fmt.Sprintf("候选数量 %d 超过上限 %d，请增大步长或减少资产",
            out.CandidateCount, config.MaxCandidateCount),
    })
}
```

- `GetOptimizationReadiness` 没有传 `max_candidate_count` 时使用默认配置。
- `CreateOptimization` 必须用 normalize 后的 config 做同一套 readiness 校验。

验收逻辑：

- 后端新增测试：候选数量大于请求 `max_candidate_count` 时 `CreateOptimization` 返回 `research_optimization_not_ready`。
- 后端新增测试：候选数量小于等于请求上限时可创建。
- 执行：

```bash
go test ./internal/service -run 'Test.*Optimization'
go test ./internal/api ./internal/service
```

### 4. 管理后台 job 类型白名单漏掉 `research_optimization_backtest`

严重级别：Medium

证据：

- `internal/repository/jobs.go:16-17`
- `internal/service/admin_service.go:514-519`
- `internal/service/admin_service.go:534-536`

问题：

实现新增了 job type：

```go
JobTypeResearchOptimization = "research_optimization_backtest"
```

但 admin job 类型白名单仍只有：

```go
repository.JobTypeSimulation
repository.JobTypeStress
repository.JobTypeSensitivity
repository.JobTypeResearchBacktest
```

错误提示也仍然只列出：

```text
simulation, stress, sensitivity, research_backtest
```

影响：

- 管理后台按 `type=research_optimization_backtest` 查询会返回 invalid_request。
- 运维排查自动调优任务时无法使用现有 job type 过滤能力。

修复方案：

- 将 `repository.JobTypeResearchOptimization` 加入 `adminJobTypes`。
- 更新错误提示文案，包含 `research_optimization_backtest`。
- 补 admin service/api 测试，覆盖该类型可过滤。

验收逻辑：

```bash
go test ./internal/service -run TestAdmin
go test ./internal/api -run Admin
```

并手工调用：

```http
GET /api/v1/admin/jobs?type=research_optimization_backtest
```

应返回 200。

### 5. 新增自动调优前端查询未在既有 BacktestPanel 测试中 mock，测试隔离被破坏

严重级别：Medium

证据：

- `web/components/research/BacktestPanel.tsx:101-110`
- `web/components/research/BacktestPanel.test.tsx:18-21`

问题：

`BacktestPanel` 新增了：

```tsx
getOptimizationReadiness(detail.id)
getLatestOptimization(detail.id)
```

但测试只 mock 了 `createBacktest`。这使测试运行时真实调用导入的 API 函数，造成不可控异步状态，并间接触发 Finding 2 的失败形态。

影响：

- 单元测试不再隔离外部 API。
- 后续测试结果容易受 fetch/mock 环境影响。

修复方案：

- 在 `BacktestPanel.test.tsx` 中 mock：
  - `getOptimizationReadiness`
  - `getLatestOptimization`
  - `createOptimization`
- 默认返回 ready 的 optimization readiness 和 `null` latest optimization。
- 增加自动调优按钮/弹窗/跳转的基本测试。

验收逻辑：

```bash
cd web && npm run test:ci -- components/research/BacktestPanel.test.tsx
```

必须稳定通过。

## 文档功能点覆盖核对

- 启用资产最多 10 个：已实现。
- 锁定资产权重保持不变：已实现候选生成与测试。
- 未锁定正权重资产可被调优：已实现候选生成与测试。
- 0 权重启用资产参与调优：已实现。
- 一次输出最高收益、最低回撤、Calmar 三组结果：已实现。
- 默认 5% 步长，用户可调整：已实现。
- 只展示结果，不写回集合：已实现。
- 新增 optimization run 表、API、worker：已实现。
- 权重表状态提示：已实现。
- 前端配置弹窗和结果页：已实现。
- 创建任务时按请求 `max_candidate_count` 限制：缺失。
- 前端质量门禁与测试：未通过。
- 管理后台 job 类型纳入：缺失。

## 已执行验证

通过：

```bash
go test ./internal/service ./internal/api ./internal/jobs ./internal/repository
```

失败：

```bash
cd web && npm run lint
cd web && npm run test:ci
```

额外检查：

```bash
cd web && npx tsc --noEmit
```

该命令失败，但错误集中在既有 plan/rebalance 测试类型问题，未作为本次 td/103 缺陷记录。

## 最终建议

先修复 Findings 1、2、3、4、5，再执行完整验收：

```bash
go test ./...
cd web && npm run lint
cd web && npm run test:ci
```

全部通过后，td/103 才可视为完整实现。当前不建议输出用户文档到 `docs/`，因为功能尚未达到可发布状态。
