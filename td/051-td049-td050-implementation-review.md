# td/049、td/050 实施 Review

## Review 结论

本轮实现已覆盖 `td/049` 与 `td/050` 的主要结构性改造：组合总览地区配置拆分、计划地区配比从场景模板中收敛到计划维度、额外现金流 UI/业务引用清理、FIRE 模拟最近 7 次保留、附属分析绑定 `simulation_run_id`、前端历史模拟下拉和文档沉淀。

但当前实现未达到可验收状态。以下问题会直接影响 FIRE 模拟结果展示、附属分析过期判断、历史 run 隔离和 tooltip 可用性，需要修复后再进入验收。

## Findings

### 1. P1 路径详情接口返回字段与前端字段不一致，导致月度/年度数据空白或 NaN

位置：

- `internal/simulation/path.go:40`
- `internal/simulation/path.go:52`
- `web/app/plans/[id]/analysis/[run_id]/paths/[path_no]/page.tsx:105`

现象：

- 用户点击代表路径 `P50`、`P75` 后，页面显示月度行数 420、年度行数 35，但表格内容为空或年度字段为 `NaN`。
- 示例路径：`/plans/plan_3ec22bfe-fa8d-42d7-b0a4-372e0f5f7039/analysis/simrun_813a3ffa-c3b2-4099-8e90-b3cc0e43f81c/paths/4960`。

原因：

- 后端 `MonthRecord`、`YearRecord` 未声明 JSON tag，Go 默认输出 `MonthOffset`、`TotalWealthMinor`、`StartWealthMinor` 等 PascalCase 字段。
- 前端类型和渲染读取的是 `month_offset`、`total_wealth_minor`、`start_wealth_minor` 等 snake_case 字段。
- 行数组长度存在，所以按钮显示数量正确；字段值取不到，所以单元格为空或格式化为 `NaN`。

修复方案：

- 为 `MonthRecord` 和 `YearRecord` 的所有 API 输出字段补齐 snake_case JSON tag。
- 保持前端 `PathMonthRecord` / `PathYearRecord` 类型不变，不做前端兼容双字段读取，避免 API 契约继续漂移。
- 后端增加路径详情 API 测试，直接断言响应 JSON 中存在 `monthly[0].month_offset`、`monthly[0].total_wealth_minor`、`yearly[0].start_wealth_minor`，且不存在 PascalCase 字段。

验收逻辑：

- 打开代表路径详情页，月度表格显示 420 行且每行月份、资产、收入、支出、税费、回撤均有有效值。
- 切换年度表格，年度 35 行不再出现 `NaN`。
- `go test ./internal/api ./internal/simulation` 通过。
- `cd web && npm run test:ci -- plans/[id]/analysis/[run_id]/paths/[path_no]/page.test.tsx` 通过，并新增真实 snake_case 契约断言。

### 2. P1 压力/敏感性测试会被误判为“配置已变化，结果已过期”

位置：

- `internal/service/stress_service.go:178`
- `internal/service/sensitivity_service.go:180`

现象：

- 运行压力测试结束后，页面直接提示“配置已变化，结果已过期”。
- 敏感性测试未跑完时刷新页面，也会提示“配置已变化，结果已过期”。

原因：

- 当前 `ResultStale` 判断为 `rec.InputHash != currentHash`。
- `rec.InputHash` 在 td/050 后保存的是 Monte Carlo run 的 `input_hash`，这是完整模拟输入 hash。
- `currentHash` 是计划配置 hash。
- 两者不是同一 hash 体系，即使配置没有变化，也天然不相等，所以会误报过期。

修复方案：

- 附属分析的过期判断必须基于所属 Monte Carlo run 的 `InputSnapshot.ConfigHash` 与当前计划 `ConfigHash` 比较。
- `StressService.toView` 与 `SensitivityService.toView` 不再使用 `rec.InputHash != currentHash`。
- 在 `toView` 中通过 `rec.SimulationRunID` 加载对应 simulation run，解析 `input_snapshot_json` 的 `config_hash` 后与 currentHash 比较。
- 如果 `rec.SimulationRunID` 为空的 legacy 数据，按兼容策略处理为不可判定：不显示 stale，或返回单独的 legacy 标记；不要继续用 input hash 与 config hash 比较。

验收逻辑：

- 刚完成压力测试后，如果计划未编辑，不显示 `StaleBanner`。
- 刚完成敏感性测试后，如果计划未编辑，不显示 `StaleBanner`。
- 修改计划参数或持仓导致 config hash 变化后，再查看旧 stress/sensitivity，才显示“结果已过期”。
- 刷新页面不会让 running/pending 的附属分析误报过期。
- 后端测试覆盖：同一 run 的 `input_hash != config_hash` 但 `snapshot.config_hash == currentHash` 时，`result_stale=false`。

### 3. P1 首次运行模拟完成后，页面不会稳定自动展示新结果

位置：

- `web/app/plans/[id]/analysis/page.tsx:362`
- `web/app/plans/[id]/analysis/page.tsx:381`
- `web/app/plans/[id]/analysis/page.tsx:440`

现象：

- 首次运行 Monte Carlo 模拟，任务结束后页面不自动展示模拟结果。

原因：

- `startMut.onSuccess` 只设置 `selectedRunId=res.run_id`，但没有立即让 `simulations` 查询包含这个新 run。
- `selectedRun` 的计算是 `simulations.find(selectedRunId) ?? simulations[0]`。首次运行时列表为空，直到 terminal 后异步 refetch 完成前 `selectedRun` 仍为空。
- `simCompleted` 还依赖单独的 `simJobQ.data?.status === "succeeded"`，如果 job 查询缓存或 refetch 时序未命中，结果区不会渲染。
- `docs/015` 写了“新建模拟成功后失效 simulations 查询使其立即出现在下拉中”，但当前代码没有在 `startMut.onSuccess` 失效或写入 pending run。

修复方案：

- `createSimulation` 成功后，立即将 `{id: run_id, job_id, plan_id, runs, summary_json:{}, created_at}` 作为 pending run 写入 `["simulations", planId]` query cache，并选中该 run。
- `useJobStatus.onComplete` 后显式 `await qc.invalidateQueries({queryKey:["simulations", planId]})`，再 `await qc.invalidateQueries({queryKey:["job", jobID]})`，确保新 run 和 succeeded job 状态同步到当前页面。
- `simCompleted` 优先以所选 run 的 `summary_json.success_probability` 是否为 number 判断结果是否可展示；job 状态只用于 pending/running 展示，不作为已经落库结果的硬阻断。

验收逻辑：

- 一个没有历史模拟的计划首次点击运行模拟，任务完成后自动显示“模拟结果”、成功率、分位数曲线和代表路径按钮。
- 不需要手动刷新页面。
- 页面历史模拟下拉自动包含新 run，并选中新 run。
- 前端测试覆盖“初始 simulations=[]，createSimulation 返回 run_id，job complete 后展示模拟结果”。

### 4. P1 按 simulation_run_id 查询附属分析时未校验 run 归属 plan

位置：

- `internal/service/stress_service.go:144`
- `internal/service/sensitivity_service.go:146`

问题：

- `ListByRun(planID, runID)` 只校验 `planID` 存在，然后直接按 `simulation_run_id` 查询 `analysis_results`。
- 如果传入另一个计划的 `simulation_run_id`，接口可能返回其他计划的 stress/sensitivity 结果。
- 这破坏了 td/050 的“附属分析归属于某一次 Monte Carlo 模拟且属于当前计划”的边界。

修复方案：

- `ListByRun` 先通过 `SimulationRepo.GetByID(runID)` 加载 run。
- 校验 `run.PlanID == planID`，不匹配时返回 `simulation_not_found`。
- 校验通过后再查询 `AnalysisRepo.ListBySimulationRun`。
- stress 和 sensitivity 共用同一校验逻辑，避免两边实现漂移。

验收逻辑：

- 计划 A 的 `GET /plans/A/stress-tests?simulation_run_id=runB` 返回 404/业务错误，不返回计划 B 的结果。
- 计划 A 的 `GET /plans/A/sensitivity-tests?simulation_run_id=runB` 同样返回错误。
- 后端 API 测试覆盖跨 plan run id 查询。

### 5. P2 组合总览 `?` tooltip 仍存在严重偏移

位置：

- `web/components/ui/Tooltip.tsx:79`
- `web/components/ui/Tooltip.tsx:139`

现象：

- 组合总览页 `?` hover 后，tooltip 仍不在 `?` 图标附近。

原因：

- 当前 `followCursor` 只在 `onMouseEnter` / `onMouseMove` 更新 ref，但定位计算只在 `open` 等依赖变化时执行一次；鼠标移动不会触发重新布局。
- tooltip DOM 仍渲染在 trigger wrapper 内，虽然使用 `position: fixed`，但在复杂页面布局或存在 transform/contain 的祖先场景下仍容易出现定位参考系异常。
- `clickToggle` 与 focus/hover 混用时，也可能回退到 trigger rect 定位，造成用户看到的位置与鼠标位置不一致。

修复方案：

- `Tooltip` 内容使用 portal 渲染到 `document.body`。
- `followCursor` 模式下用 React state 保存最新 `{clientX, clientY}`，而不是仅写 ref；`onMouseMove` 时触发位置重算。
- hover 模式只按鼠标定位；键盘 focus 模式才回退到 trigger rect。
- 保留边界翻转和视口夹紧逻辑。

验收逻辑：

- 组合总览所有 `?` hover 后 tooltip 出现在鼠标附近 8-12px 范围。
- 鼠标移动时 tooltip 跟随更新，不出现横跨页面的偏移。
- 视口右侧/底部 hover 时 tooltip 自动翻转且不溢出屏幕。
- Tab focus 到 `?` 时仍按触发器附近展示，满足键盘可访问性。

### 6. P2 敏感性测试图表信息表达不足

位置：

- `web/components/charts/SensitivityCharts.tsx:45`
- `web/components/charts/SensitivityCharts.tsx:48`
- `web/components/charts/SensitivityCharts.tsx:75`

现象：

- 敏感性测试完成后，曲线图高度偏小，看起来扁平。
- 横轴、纵轴没有说明，用户不知道横坐标是扰动参数、纵坐标是成功率。
- hover 内容太简略；100% 成功率场景显示为 `1` 时观感异常。

修复方案：

- `ParameterCurvesChart` 单图高度从 `180` 提升到 `280`，并配置 `grid` 留出坐标轴名称空间。
- xAxis 增加 `name: "参数扰动"`，yAxis 增加 `name: "成功率"`，y 轴范围固定 `[0,1]` 并用百分比 formatter。
- tooltip 自定义 formatter：展示参数名称、扰动标签、成功率百分比、相对基准变化值；如果后端未返回基准差值，则前端用曲线中基准点或第一点计算。
- heatmap 同样补充坐标轴名称：横轴为支出扰动，纵轴为收益扰动，tooltip 展示完整中文标签和百分比。

验收逻辑：

- 敏感性曲线高度明显增加，不再扁平。
- 用户可以直接看到横轴/纵轴含义。
- hover 任意点显示“参数、扰动、成功率、相对基准变化”。
- 100% 成功率显示为 `100%`，不显示裸数字 `1`。

### 7. P3 docs/015 与实际实现存在描述不一致

位置：

- `docs/015-fire-simulation-history-retention.md:3`
- `docs/015-fire-simulation-history-retention.md:21`
- `migrations/0001_init.sql:305`

问题：

- `docs/015` 写 `0001_init.sql` 中 `analysis_results` 已增加 `simulation_run_id` 与索引，但当前 `0001_init.sql` 的 `analysis_results` 仍无该列；实际新增列在 `migrations/0014_analysis_results_simulation_run.sql`。
- `docs/015` 写“新建模拟成功后切换到新返回的 run_id 并失效 simulations 查询使其立即出现在下拉中”，但当前代码仅设置 `selectedRunId`，未在创建成功时失效或写入 query cache。

修复方案：

- 以最终实现为准修正文档：`simulation_run_id` 通过 `0014` migration 增量加入；如决定当前未上线要同步基线 schema，则补齐 `0001_init.sql` 后再保留 `0014` 作为已有库升级路径。
- 文档中的首次运行刷新策略改为与修复后的实现一致。

验收逻辑：

- `docs/015` 对 schema 变更位置和前端刷新策略的描述与代码一致。
- 新建库迁移测试和已有库增量迁移测试均通过。

## 已验证项

- `go test ./internal/db ./internal/repository ./internal/service ./internal/api ./internal/simulation` 通过。
- `cd web && npm run test:ci -- plans/[id]/analysis/page.test.tsx plans/[id]/analysis/[run_id]/paths/[path_no]/page.test.tsx tooltip-position.test.ts` 通过。
- `cd web && npm run lint` 通过。
- `cd web && npm run build` 通过。

## 未完整验收原因

当前自动化测试没有覆盖真实路径详情 JSON 字段契约、首次模拟完成后的异步 refetch 时序、附属分析 stale hash 语义、跨计划 run id 查询隔离和组合总览真实 hover 定位。因此即使测试与构建通过，仍不能视为 td/049、td/050 已完整实现。
