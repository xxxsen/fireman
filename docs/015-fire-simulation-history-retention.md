# FIRE 模拟历史保留与附属分析绑定（td/050 实现）

## 1. analysis_results 增加 simulation_run_id
- `migrations/0014_analysis_results_simulation_run.sql`（增量迁移，遵循 `docs/013` 顺序迁移策略）：`analysis_results` 增加 `simulation_run_id TEXT NOT NULL DEFAULT ''` 列，并建立 `(simulation_run_id, type, created_at DESC)` 索引。历史行 `simulation_run_id=''` 视为未归属/legacy，直至被裁剪。

## 2. Monte Carlo 仅保留最近 7 次
- `SimulationService.SimulationRetentionLimit = 7`。
- 创建模拟时在同一事务内 `pruneOldRuns`：`SimulationRepo.PruneByPlan(keep=7)` 删除最旧 run（含其 path index），并 `DeleteBySimulationRunIDs` 级联删除被裁剪 run 的附属分析。
- `SimulationService.ListByPlan` 固定 `LIMIT 7`，列表接口最多返回 7 条。

## 3. 附属分析绑定 run + 冻结快照 + 仅保留最新
- stress / sensitivity `Create`：`ResolveAnalysisRun(planID, simulation_run_id)` 解析目标 run（空则取最近一次），读取该 run 的 `InputSnapshot` 作为冻结快照参与计算，不再实时重算组合。
- 写入前 `DeleteBySimulationRunAndType(runID, type)`，保证每个 run 每类附属分析只保留最新一条。
- `AnalysisResult` 持久化 `simulation_run_id`，并在视图（`StressTestView` / `SensitivityTestView`）回传。
- 过期判断（`result_stale`）：以该附属分析所属 run 快照中的 `config_hash` 与当前计划 `config_hash` 比较（`analysisResultStale` + `SimulationService.RunConfigHash`）；不再用 run 的 `input_hash` 与 config hash 跨体系比较。`simulation_run_id` 为空的 legacy 行按"不可判定"处理，不标记过期。

## 4. stress / sensitivity 查询支持 simulation_run_id
- 列表接口读取 `?simulation_run_id=`：非空走 `ListByRun`（`AnalysisRepo.ListBySimulationRun`），为空回退 `ListByPlan`。
- `ListByRun` 先 `SimulationService.EnsureRunInPlan(planID, runID)` 校验 run 归属当前计划，跨计划 run id 返回 `simulation_not_found`，避免泄漏其他计划的结果。

## 5. FIRE 模拟页历史模拟下拉
- `analysis/page.tsx` 新增历史模拟下拉（仅在存在模拟时显示），选项用 `simulationOptionLabel`（`created_at` 为毫秒，格式化为日期时间 + 成功率/进行中）。
- `selectedRunId` 状态：默认选中列表第一条（最新）；新建模拟成功后，将该 run 作为 pending 项写入 `["simulations", planId]` query cache 并选中，使其立即出现在下拉中；任务终态时 `invalidateAll` 重新拉取列表，替换为已落库的 run。
- 模拟结果展示以所选 run 的 `summary_json.success_probability` 是否为 number 判定（`simCompleted`），不再以单独的 job 查询状态作为硬阻断；job 状态仅用于 pending/running 进度展示。
- “最新结果”区块改名“模拟结果”，随所选 run 切换展示。

## 6. 前端附属分析基于选中 run
- `listStressTests` / `listSensitivityTests` 携带 `simulation_run_id`；`createStressTest` / `createSensitivityTest` 提交 `simulation_run_id`。
- stress / sensitivity 查询 `enabled` 与请求均绑定 `selectedRun?.id`；切换历史 run 时自动重新拉取对应附属分析。
- 当无模拟或所选 run 未完成时，禁用压力测试/敏感性分析按钮并给出提示（`attachDisabled` / `attachHint`）。
