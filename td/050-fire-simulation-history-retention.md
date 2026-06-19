# FIRE 模拟历史回溯与保留策略改造方案

## 背景

当前 FIRE 模拟页面只展示最近一次 Monte Carlo 模拟结果，用户无法在页面上回看此前的模拟过程。产品需要保留最近 7 次 Monte Carlo 模拟，达到上限后自动清理最早的一次；同时，压力测试和敏感性测试应归属于某一次 Monte Carlo 模拟，而不是独立散落在计划维度下。

这里的“一次模拟流程”定义为一次 Monte Carlo 模拟。压力测试、敏感性测试属于这一次 Monte Carlo 模拟的附属分析；用户可以在同一次 Monte Carlo 模拟下重复运行压力测试和敏感性测试，但每种附属分析只保留最新一次结果。

## 当前状态

- Monte Carlo 模拟结果已经落库，不是纯前端临时结果。
- 主结果写入 `simulation_runs`。
- 代表路径索引写入 `simulation_path_index`。
- 分位数时间序列写入 `simulation_quantile_series`。
- 压力测试和敏感性测试结果写入 `analysis_results`。
- `simulation_runs` 当前按计划查询时没有 7 次保留上限，服务层也没有创建新模拟后的自动裁剪逻辑。
- `analysis_results` 当前只有 `plan_id` 和 `type`，没有明确归属到某个 `simulation_run_id`。
- FIRE 模拟页面当前默认取 `simulations[0]` 作为最新模拟结果，没有历史模拟下拉选择入口。
- 压力测试和敏感性测试列表当前按计划维度取最新结果，无法区分这些结果属于哪一次 Monte Carlo 模拟。

## 问题判断

现有实现的缺陷不是“模拟结果没有落库”，而是落库模型不完整：

- 缺少最近 7 次的保留策略。
- 缺少附属分析与 Monte Carlo 模拟之间的归属关系。
- 缺少前端历史模拟选择和历史结果渲染能力。
- 压力测试、敏感性测试如果继续按计划维度展示，会在用户切换历史模拟时显示错误的附属分析结果。

## 推荐方案

### 1. 数据模型调整

新增 migration，为 `analysis_results` 增加 `simulation_run_id` 字段：

```sql
ALTER TABLE analysis_results
ADD COLUMN simulation_run_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_analysis_results_run_type_created
ON analysis_results(simulation_run_id, type, created_at DESC);
```

说明：

- DDL 必须放在 migrations 中，不允许在业务代码运行时动态修表。
- 当前项目未上线时，如果选择重建 DB，也可以同步更新 `0001_init.sql` 的初始 schema，但迁移文件仍应保留，保证本地已有库可升级。
- `simulation_run_id` 用于表达压力测试、敏感性测试归属于哪一次 Monte Carlo 模拟。
- 对历史旧数据，`simulation_run_id=''` 可以作为兼容值；新创建的附属分析必须写入真实 run id。

### 2. Monte Carlo 模拟保留最近 7 次

在 `SimulationService.Create` 创建新的 `simulation_runs` 后，同一事务内按 `plan_id` 裁剪历史模拟：

- 按 `created_at DESC, id DESC` 查询当前计划的全部 simulation runs。
- 保留最新 7 条。
- 删除第 8 条及更早的 `simulation_runs`。
- 同步删除被裁剪 run 对应的 `analysis_results`。
- `simulation_path_index` 和 `simulation_quantile_series` 依赖 `run_id`，应通过外键级联或显式删除一起清理。

推荐实现位置：

- `repository.SimulationRepo` 增加 `PruneByPlan(ctx, tx, planID, keep int) ([]string, error)`。
- `repository.AnalysisRepo` 增加 `DeleteBySimulationRunIDs(ctx, tx, runIDs []string) error`。
- `SimulationService.Create` 在创建 pending run 后调用 prune，`keep=7`。

这样可以保证第 8 次模拟一创建，最早的一次就被清理，不需要额外定时任务。

### 3. 附属分析绑定到选中的模拟

压力测试、敏感性测试创建接口增加 `simulation_run_id` 入参：

```json
{
  "simulation_run_id": "simrun_xxx",
  "runs": 1000,
  "seed": "7"
}
```

服务端处理规则：

- 如果传入 `simulation_run_id`，必须校验该 run 存在且 `run.plan_id` 等于 URL 中的 `plan_id`。
- 压力测试和敏感性测试使用该 run 的 `input_snapshot_json` 作为冻结输入，而不是重新从当前计划配置构造输入。
- 如果未传入 `simulation_run_id`，为了兼容旧调用，可以默认使用该计划最新的一次 simulation run；如果计划下没有 simulation run，则返回明确错误，提示先运行 Monte Carlo 模拟。
- 创建同一 `simulation_run_id + type` 的新附属分析前，删除旧的同类型 `analysis_results`，保证每次 Monte Carlo 模拟下压力测试只保留最新一次，敏感性测试只保留最新一次。

推荐实现位置：

- `CreateStressTestRequest` 和 `CreateSensitivityTestRequest` 增加 `SimulationRunID string`。
- `StressService.Create` / `SensitivityService.Create` 通过 `SimulationRepo.GetByID` 加载 run 并校验 plan。
- 新增内部方法从 `simulation_runs.input_snapshot_json` 还原 `simulation.InputSnapshot`，并重新计算或复用 input hash。
- `AnalysisRepo` 增加 `DeleteBySimulationRunAndType(ctx, tx, runID, typ string) error`。
- 创建新 analysis pending 前调用删除方法，实现“只保留最新一次结果”。

### 4. 查询接口调整

压力测试和敏感性测试列表支持按 `simulation_run_id` 查询：

```http
GET /api/v1/plans/:plan_id/stress-tests?simulation_run_id=simrun_xxx
GET /api/v1/plans/:plan_id/sensitivity-tests?simulation_run_id=simrun_xxx
```

规则：

- 传入 `simulation_run_id` 时，仅返回该 run 下对应类型的最新结果。
- 未传入时可继续返回计划维度列表，用于兼容旧页面或 dashboard 摘要。
- 返回结构增加 `simulation_run_id` 字段，便于前端确认数据归属。

### 5. FIRE 模拟页面历史查看

页面新增“历史模拟”下拉选择：

- 下拉数据来自 `listSimulations(planId)`。
- 后端列表最多返回最近 7 次。
- 默认选中最新一次模拟。
- 新建模拟成功后，将选中项切换到新返回的 `run_id`。
- 用户切换历史模拟时，页面所有 Monte Carlo 结果、代表路径、压力测试、敏感性测试都基于选中的 `simulation_run_id` 重新查询和渲染。

下拉展示建议：

- 展示模拟时间、模拟次数、成功率、状态。
- 示例：`2026-06-19 14:32 · 10000 次 · 成功率 86.4%`。
- pending / running 的模拟显示状态，不展示未完成结果。

### 6. 前端附属分析行为

压力测试和敏感性测试按钮基于当前选中的 Monte Carlo 模拟运行：

- 没有选中模拟时禁用按钮，并提示先运行 Monte Carlo 模拟。
- 当前选中模拟未成功完成时禁用按钮。
- 点击运行压力测试时，body 传入 `simulation_run_id: selectedRun.id`。
- 点击运行敏感性测试时，body 传入 `simulation_run_id: selectedRun.id`。
- 查询压力测试结果时使用 `listStressTests(planId, selectedRun.id)`。
- 查询敏感性测试结果时使用 `listSensitivityTests(planId, selectedRun.id)`。

这样用户切换历史模拟后，不会看到其他模拟流程下的压力/敏感性结果。

## 验收逻辑

- 连续运行 8 次 Monte Carlo 模拟后，数据库和页面只保留最近 7 次。
- 第 8 次运行后，最早一次 simulation run、路径索引、分位数序列、附属分析结果均被清理。
- FIRE 模拟页面默认展示最新一次模拟，并可通过下拉切换最近 7 次历史模拟。
- 切换历史模拟后，成功率、分位数曲线、代表路径、压力测试、敏感性测试均渲染所选 run 的数据。
- 对同一次 Monte Carlo 模拟连续运行多次压力测试，只保留最新一次压力测试结果。
- 对同一次 Monte Carlo 模拟连续运行多次敏感性测试，只保留最新一次敏感性测试结果。
- 对不同 Monte Carlo 模拟分别运行压力测试，切换历史模拟时只显示对应 run 的压力测试。
- 压力测试和敏感性测试使用所选 Monte Carlo run 的冻结快照，即使当前计划配置已变化，也不会用最新计划配置污染历史分析。
- 未传 `simulation_run_id` 的旧接口调用仍能得到明确兼容行为；没有任何历史模拟时返回“请先运行 Monte Carlo 模拟”的业务错误。

## 测试建议

- Repository 测试覆盖 `PruneByPlan`：构造 8 条 run，确认只剩最新 7 条。
- Repository 测试覆盖删除被裁剪 run 的 `analysis_results`。
- Service 测试覆盖压力测试传入非本计划 `simulation_run_id` 时返回错误。
- Service 测试覆盖同一 run 下重复创建 stress/sensitivity 后只保留最新一条。
- API 测试覆盖 `?simulation_run_id=` 查询只返回对应 run 的结果。
- 前端测试覆盖历史模拟下拉默认选中最新 run、切换 run 后重新请求 paths/stress/sensitivity。
- 前端测试覆盖未完成模拟下压力/敏感性按钮禁用。
