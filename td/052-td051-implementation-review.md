# td/051 实施 Review

## Review 结论

`td/051` 原有的 7 项问题均已实现对应修复：

- 路径详情 `MonthRecord` / `YearRecord` 已统一为 snake_case JSON 契约，并补 API 断言。
- 压力测试、敏感性测试改为比较所属 Monte Carlo run 快照中的 `config_hash`，不再将 `input_hash` 与配置 hash 跨体系比较。
- 首次模拟创建后会写入 pending run 到 React Query cache，任务完成后刷新列表，结果展示不再依赖独立 job 查询。
- 按 `simulation_run_id` 查询附属分析前会校验 run 属于当前计划。
- Tooltip 已改为 body portal，并根据鼠标 state 持续更新位置。
- 敏感性曲线与热力图补齐尺寸、坐标轴和百分比 tooltip。
- `docs/015` 已修正 migration 和首次模拟刷新行为的说明。

但仍有一个 P1 并发流程缺陷：重复启动同一 Monte Carlo run 的压力/敏感性测试时，旧任务没有被取消。当前实现不能判定为完整通过。

## Findings

### 1. P1 重复运行附属分析时，旧 queued/running job 未取消且可能失败或浪费 worker

位置：

- `internal/service/stress_service.go:93`
- `internal/service/sensitivity_service.go:94`
- `internal/repository/analysis.go:116`
- `internal/jobs/analysis_runner.go:61`

问题：

- 为实现“同一次 Monte Carlo 模拟下每种附属分析只保留最新一次结果”，当前代码在创建新 stress/sensitivity job 前直接执行 `DeleteBySimulationRunAndType`。
- 该删除只删除 `analysis_results` 记录，没有取消旧的 `jobs` 记录。
- 如果旧 job 仍在 `queued`，worker 后续领取它时，`AnalysisRunner.runAnalysis` 会通过 `GetByJobID` 查不到 analysis record 并将 job 标记为 `stress_failed` / `sensitivity_failed`。
- 如果旧 job 已在 `running`，它会继续占用计算资源；运行结束后更新已删除的 analysis record，用户不可见该结果，但 job 仍可能被标为成功。
- 这与“用户可以无限次运行压力、敏感性测试，但仅保留最新一次结果”的流程不一致，且会产生无意义失败任务、资源消耗和错误事件。

修复方案：

- 在同一数据库事务内，先查询当前 `simulation_run_id + analysis_type` 对应的旧 `analysis_results.job_id`。
- 对旧 job 执行取消：
  - queued job 调用 `JobRepo.CancelQueuedWithError`，错误码固定为 `superseded_by_newer_analysis`。
  - running job 设置 `cancel_requested=1`；runner 的现有 `cancelCheck` 会在计算过程中终止，并由 worker 落为 canceled。
- 旧 job 取消请求写入成功后，再删除旧 `analysis_results`，最后创建新 job 和新的 pending analysis record。
- 将该逻辑收敛为 `AnalysisRepo`/`JobRepo` 协作的单个 service 内部方法，供 stress 与 sensitivity 共用，避免两处取消顺序分叉。

验收逻辑：

- 同一 run 的压力测试 A 处于 queued 时启动压力测试 B：A 状态变为 `canceled`，错误码为 `superseded_by_newer_analysis`；B 正常完成；列表只返回 B。
- 同一 run 的敏感性测试 A 处于 running 时启动敏感性测试 B：A 收到 cancel request 并最终为 `canceled`；B 正常完成；列表只返回 B。
- 重复运行不会出现因为 `analysis_results` 被提前删除导致的 `stress_failed` / `sensitivity_failed`。
- 不同 run 或不同分析类型互不取消。
- 后端测试覆盖 queued 与 running 两种旧 job 状态，以及 stress/sensitivity 两种类型。

## 已验证项

- 路径详情 JSON 契约测试新增并通过，`monthly` / `yearly` 字段为 snake_case。
- 已验证新完成的 stress/sensitivity 在未编辑计划时不标记 stale。
- 已验证跨计划 `simulation_run_id` 不返回其他计划的附属分析。
- 已验证首次模拟完成后无需手动刷新即可显示结果。
- 已验证 tooltip 跟随鼠标位置更新，敏感性曲线坐标轴与百分比 tooltip 已覆盖测试。
- `go test ./internal/db ./internal/repository ./internal/service ./internal/api ./internal/simulation ./internal/jobs` 通过。
- `cd web && npm run test:ci -- plans/[id]/analysis/page.test.tsx plans/[id]/analysis/[run_id]/paths/[path_no]/page.test.tsx components/ui/Tooltip.test.tsx components/charts/SensitivityCharts.test.tsx tooltip-position.test.ts` 通过。
- `cd web && npm run lint` 通过。
- `cd web && npm run build` 通过。

## 文档状态

`docs/015-fire-simulation-history-retention.md` 已随实现更新。由于仍存在 P1 并发任务缺陷，本轮不新增“完整验收”文档。
