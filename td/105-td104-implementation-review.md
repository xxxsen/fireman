# td/104 返工实现 Review

## Review 结论

td/104 中列出的 5 个问题均已进行返工：

- 前端 lint 失败项已修复。
- 普通回测禁用原因不再被自动调优状态覆盖。
- `max_candidate_count` 已接入 optimization readiness 和创建校验。
- 管理后台 job 类型白名单已加入 `research_optimization_backtest`。
- `BacktestPanel` 测试已补齐自动调优 API mock 和基础断言。

目标验证通过：

```bash
cd web && npm run lint
cd web && npm run test:ci -- components/research/BacktestPanel.test.tsx
go test ./internal/service -run 'Test.*Optimization|TestAdmin'
go test ./internal/api ./internal/service
cd web && npm run test:ci
```

但当前仓库仍未达到完整可验收状态：`go test ./...` 稳定失败于数据库迁移测试。该问题来自 td/103 新增 `0026_research_optimization.sql` 后，既有迁移总数断言仍停留在 25。

因此本次不应输出用户文档到 `docs/`。需要先修复下面的阻断问题。

## Findings

### 1. 新增 0026 migration 后，数据库迁移幂等测试仍断言 25 条 migration 记录

严重级别：High

证据：

- `migrations/0026_research_optimization.sql:1`
- `internal/db/db_test.go:173-180`

失败命令：

```bash
go test ./internal/db -run TestMigrate_AppliesInitialSchemaAndIsIdempotent -count=1
```

失败信息：

```text
expected 25 migration records after idempotent re-run, got 26
```

原因：

td/103 新增了 migration：

```text
0026_research_optimization.sql
```

但 `TestMigrate_AppliesInitialSchemaAndIsIdempotent` 中仍硬编码：

```go
if migrationCount != 25 {
    t.Errorf("expected 25 migration records after idempotent re-run, got %d", migrationCount)
}
```

现在完整迁移后 schema_migrations 记录数应为 26。

影响：

- `go test ./...` 稳定失败。
- 数据库迁移链的全量 CI 无法通过。
- td/104 最终建议中的完整验收命令尚未满足。

修复方案：

- 将 `internal/db/db_test.go` 中该断言从 25 更新为 26。
- 同步更新错误信息中的期望值。
- 不改 migration 文件本身；`0026_research_optimization.sql` 已被迁移系统识别并成功应用。

验收逻辑：

```bash
go test ./internal/db -run TestMigrate_AppliesInitialSchemaAndIsIdempotent -count=1
go test ./...
```

两条命令均必须通过。

## td/104 Finding 复核

### Finding 1：前端 lint 失败

状态：已修复。

复核：

- `OptimizationConfigDialog` 删除了关闭弹窗时同步 setState 的 effect。
- 删除了未使用的 `onWeightStepChange` props 和父组件回调。
- `cd web && npm run lint` 通过。

### Finding 2：普通回测禁用原因被覆盖

状态：已修复。

复核：

- `BacktestPanel` 将普通回测按钮和自动调优按钮拆成两个独立区域。
- `run-disabled-reason` 和 `opt-disabled-reason` 可同时展示。
- `BacktestPanel.test.tsx` 新增 “同时展示两个禁用原因” 测试。

### Finding 3：`max_candidate_count` 未生效

状态：已修复。

复核：

- `evaluateOptimizationReadiness` 改为接收完整 `OptimizationConfig`。
- 候选数量使用 `config.MaxCandidateCount` 校验。
- `CreateOptimization` 使用 normalize 后的 config 执行同一套 readiness 校验。
- 新增 `TestOptimizationReadiness_MaxCandidateCountBlocks` 和 `TestOptimizationReadiness_MaxCandidateCountAllows`。

### Finding 4：管理后台 job 类型白名单缺失

状态：已修复。

复核：

- `adminJobTypes` 已加入 `repository.JobTypeResearchOptimization`。
- invalid_request 文案已加入 `research_optimization_backtest`。
- 新增 `TestAdminListJobs_ResearchOptimizationType`。

### Finding 5：BacktestPanel 测试 mock 不完整

状态：已修复。

复核：

- `BacktestPanel.test.tsx` 已 mock `getOptimizationReadiness`、`getLatestOptimization`、`createOptimization`。
- 默认 optimization readiness 返回 ready，latest optimization 返回 null。
- 新增自动调优按钮 ready/not-ready 测试。

## 已执行验证

通过：

```bash
cd web && npm run lint
cd web && npm run test:ci -- components/research/BacktestPanel.test.tsx
go test ./internal/service -run 'Test.*Optimization|TestAdmin'
go test ./internal/api ./internal/service
cd web && npm run test:ci
go test ./internal/marketdata -run TestTrailingReturnsScalingIsSubQuadratic -count=1
```

失败：

```bash
go test ./...
go test ./internal/db -run TestMigrate_AppliesInitialSchemaAndIsIdempotent -count=1
```

说明：

- `go test ./...` 中 `internal/marketdata` 的 `TestTrailingReturnsScalingIsSubQuadratic` 曾出现一次性能阈值抖动，但单独复跑通过，未作为本次实现缺陷记录。
- `internal/db` migration count 失败可稳定复现，应作为阻断项修复。

## 最终建议

当前 td/104 返工本身已完成，但 td/103/td104 组合后的仓库仍未通过全量 Go 验收。先修复 migration count 断言，再执行：

```bash
go test ./...
cd web && npm run lint
cd web && npm run test:ci
```

全部通过后，再整理自动调优功能文档输出到 `docs/`。
