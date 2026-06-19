# FIRE 模拟历史保留与附属分析绑定（td/050 实现）

## 1. analysis_results 增加 simulation_run_id
- `0001_init.sql`：`analysis_results` 增加 `simulation_run_id TEXT` 列与 `(simulation_run_id, type)` 索引（迁移为基线 schema 的一部分，遵循 `docs/013` schema 策略）。

## 2. Monte Carlo 仅保留最近 7 次
- `SimulationService.SimulationRetentionLimit = 7`。
- 创建模拟时在同一事务内 `pruneOldRuns`：`SimulationRepo.PruneByPlan(keep=7)` 删除最旧 run（含其 path index），并 `DeleteBySimulationRunIDs` 级联删除被裁剪 run 的附属分析。
- `SimulationService.ListByPlan` 固定 `LIMIT 7`，列表接口最多返回 7 条。

## 3. 附属分析绑定 run + 冻结快照 + 仅保留最新
- stress / sensitivity `Create`：`ResolveAnalysisRun(planID, simulation_run_id)` 解析目标 run（空则取最近一次），读取该 run 的 `InputSnapshot` 作为冻结快照参与计算，不再实时重算组合。
- 写入前 `DeleteBySimulationRunAndType(runID, type)`，保证每个 run 每类附属分析只保留最新一条。
- `AnalysisResult` 持久化 `simulation_run_id`，并在视图（`StressTestView` / `SensitivityTestView`）回传。

## 4. stress / sensitivity 查询支持 simulation_run_id
- 列表接口读取 `?simulation_run_id=`：非空走 `ListByRun`（`AnalysisRepo.ListBySimulationRun`），为空回退 `ListByPlan`。

## 5. FIRE 模拟页历史模拟下拉
- `analysis/page.tsx` 新增历史模拟下拉（仅在存在模拟时显示），选项用 `simulationOptionLabel`（`created_at` 为毫秒，格式化为日期时间 + 成功率/进行中）。
- `selectedRunId` 状态：默认选中列表第一条（最新）；新建模拟成功后切换到新返回的 `run_id` 并失效 `simulations` 查询使其立即出现在下拉中。
- “最新结果”区块改名“模拟结果”，随所选 run 切换展示。

## 6. 前端附属分析基于选中 run
- `listStressTests` / `listSensitivityTests` 携带 `simulation_run_id`；`createStressTest` / `createSensitivityTest` 提交 `simulation_run_id`。
- stress / sensitivity 查询 `enabled` 与请求均绑定 `selectedRun?.id`；切换历史 run 时自动重新拉取对应附属分析。
- 当无模拟或所选 run 未完成时，禁用压力测试/敏感性分析按钮并给出提示（`attachDisabled` / `attachHint`）。
