# td/052、td/053 实施 Review

## Review 结论

`td/052` 已补上“重新运行附属分析时先取消旧任务”的主流程，`td/053` 已实现资料库分类编辑、计划持仓冻结、刷新语义收敛、向导统一宽度及已选资产置顶。定向 Go/前端测试、前端 lint 和生产构建均通过。

但本轮仍有 2 项 P1 和 2 项 P2：运行中的附属分析在取消与成功收尾交错时仍可能被标为成功；抓取中的资产允许编辑分类但后台任务完成后会写回旧分类；外部 AKShare 候选不会随下拉关闭；本地候选容器也未固定为 10 行高度。因此不能判定为完整实施，不新增 `docs` 落地文档。

## Findings

### 1. P1 被替换的 running 附属分析在完成竞态中仍可被标记为成功

位置：

- `internal/service/analysis_supersede.go:31-48`
- `internal/repository/jobs.go:145-148`
- `internal/jobs/analysis_runner.go:69-83`
- `internal/jobs/worker.go:291-299`
- `internal/repository/jobs.go:188-194`

问题：

- 新实现对 running 旧任务只写入 `cancel_requested=1`，随后立即删除其 `analysis_results` 记录并创建新任务。
- worker 在 `AnalysisRunner.runAnalysis` 完成计算后只检查一次取消状态；检查通过后到 `Complete`、再到 `Worker.finish(...succeeded...)` 之间没有条件化终态更新。
- 若旧任务在取消请求写入前已通过第 73 行的检查并完成结果写入，而 supersede 事务随后设置 `cancel_requested=1`、删除旧结果；worker 最后会执行无条件的 `UPDATE jobs SET status='succeeded' WHERE id=?`。旧任务最终显示为 `succeeded + cancel_requested=1`，违反“被新一次分析替换的 running job 最终为 canceled”的约束。
- 当前 `TestSensitivityRerunRequestsCancelOfRunningJob` 只断言取消请求已写入，没有驱动 worker 走到终态，无法覆盖该竞态。

唯一修复方案：

- 为附属分析任务增加“取消优先”的原子终态收敛，不再让 worker 用通用无条件 `Finish` 写成功。
- `JobRepo` 新增条件化方法：
  - `RequestCancelRunningWithErrorTx` 仅在 `status='running'` 时设置 `cancel_requested=1`，并写入固定错误码 `superseded_by_newer_analysis` 与说明。
  - `FinishRunningIfNotCanceled` 仅在 `status='running' AND cancel_requested=0` 时写入 `succeeded`；返回是否实际更新。
  - `FinishCanceledIfRequested` 仅在 `status='running' AND cancel_requested=1` 时写入 `canceled`，保留 supersede 错误码。
- `supersedePriorAnalysis` 对 queued 任务继续立即取消；对 running 任务调用上述带错误码的取消请求。
- `executeAnalysisJob` 的成功分支调用 `FinishRunningIfNotCanceled`。若未成功更新，必须调用 `FinishCanceledIfRequested` 并发布 canceled 事件，不能回退到通用 `finish(succeeded)`。
- 该条件化终态逻辑仅用于 stress/sensitivity；其他 job 类型沿用现有行为，避免扩大改动面。

验收逻辑：

- 让旧 stress/sensitivity worker 在“最后一次 cancelCheck 已通过、尚未写入终态”处阻塞；此时启动同 run、同 type 的新分析，再放行旧 worker。旧 job 最终必须为 `canceled`，`error_code=superseded_by_newer_analysis`，绝不能为 `succeeded`。
- 旧任务的分析记录被移除，新任务正常创建并成为该 `simulation_run_id + type` 唯一可查询结果。
- queued 旧任务仍直接变为 canceled；不同 run、不同 type 的任务互不影响。
- 新增 worker/integration 测试覆盖上述同步屏障竞态，不能只验证 `cancel_requested=true`。

### 2. P1 抓取中的资产可编辑分类，但抓取完成会覆盖用户刚保存的值

位置：

- `web/app/assets/[id]/page.tsx:431-443`
- `internal/service/instrument_classification.go:41-65`
- `internal/jobs/instrument_fetch_runner.go:193-208`
- `internal/jobs/instrument_fetch_persist.go:14-43`
- `internal/repository/instrument.go:224-238`

问题：

- 详情页只排除系统资产，`pending_fetch` 状态同样展示“编辑分类”；服务端也没有状态限制。
- 异步导入任务的 `jobs.payload_json` 保存的是导入时的 `UserAssetClass/UserRegion`。抓取完成后，runner 从该旧 payload 生成分类，并通过 `UpdateAfterFetchTx` 无条件写回 `instruments.asset_class`、`instruments.region`。
- 因此用户在抓取期间保存“权益 / 国外”后，任务成功完成时会把分类静默恢复为导入时的值。页面此前已提示“分类已更新”，实际数据却被后台覆盖，且不触发版本冲突。

唯一修复方案：

- 将分类编辑限定为无进行中抓取的资产：`UpdateClassification` 在加载资产后调用现有抓取状态判断；`status='pending_fetch'` 或存在同资产进行中的 fetch job 时返回 `instrument_fetch_in_progress`。
- 前端仅在非系统且非 `pending_fetch` 时显示“编辑分类”；若用户通过旧客户端直接调用接口，也必须由服务端拒绝。
- `fetch_failed` 仍允许编辑：重试抓取会从当前 `instruments` 重新构造 payload，因此能正确带入用户最新分类。
- 在页面抓取中提示中增加“抓取完成后可编辑大类和地区”，避免用户误以为该能力缺失。

验收逻辑：

- `pending_fetch` 资产详情不显示“编辑分类”；直接 PATCH 返回 `instrument_fetch_in_progress`，数据库分类不变化。
- 抓取完成后，资产变为 active，可编辑并保存分类；再次刷新或重试不会覆盖该值。
- `fetch_failed` 资产编辑分类后重试抓取，完成后的 `asset_class`、`region` 保持用户编辑值。
- 补 API/integration 测试覆盖 pending 拒绝、完成后可编辑、失败后编辑再重试三种路径。

### 3. P2 点击外部后，AKShare 外部候选仍留在页面上

位置：

- `web/components/plans/AssetClassHoldingPicker.tsx:79-91`
- `web/components/plans/AssetClassHoldingPicker.tsx:312-355`
- `web/components/plans/AssetClassHoldingPicker.tsx:362-395`

问题：

- 外部点击处理只执行 `setOpen(false)`。
- 资料库候选由 `open && ...` 控制，会消失；但 `externalCandidates`、解析错误、解析/导入状态的渲染条件没有依赖 `open`，所以输入基金代码触发 AKShare 候选后，点击 picker 外部仍会显示外部候选列表。
- 这不满足 `td/053` “点击 picker 外任意位置后候选列表关闭”的验收条件，现有测试只覆盖资料库候选，遗漏外部候选。

唯一修复方案：

- 统一实现 `closeDropdown()`：设置 `open=false`，并清空外部候选和解析错误；外部点击、Escape、成功选择均调用该函数。保留输入词本身，用户重新聚焦时可继续检索。
- 所有属于候选层的 UI（外部候选、解析中的状态、解析错误、空结果提示）均以 `open` 为前置渲染条件，避免异步解析在关闭后回填到页面。
- 为解析 effect 保持现有 cleanup；当 `open=false` 时取消未完成请求并不再更新候选状态。

验收逻辑：

- 输入可解析的基金代码，出现 `wizard-external-results` 后点击 picker 外部或按 Escape，资料库候选、外部候选、解析错误和空结果提示全部消失。
- 重新聚焦输入框后可基于保留的输入词重新显示候选；点击外部候选仍可完成录入并添加资产。
- 增加前端测试覆盖外部候选的外部点击关闭和 Escape 关闭。

### 4. P2 本地候选容器未固定为 10 行高度

位置：

- `web/components/plans/AssetClassHoldingPicker.tsx:316-344`
- `td/053-asset-library-and-wizard-ui-refactor.md:211-222`

问题：

- 组件虽以 `PAGE_SIZE=10` 请求资产，但候选容器使用 `max-h-80`，它只是最大高度而非固定高度；候选行也没有固定高度，名称、历史标签换行时会改变行高。
- 当前容器在内容少于 10 条时缩短，在内容较多且行高约 40px 时最多只显示约 8 行，无法满足“下拉高度以 10 个资产数据高度为准”的既定要求。

唯一修复方案：

- 定义单一候选行高度 token（例如 `--asset-picker-row-height: 48px`），每条本地候选按钮使用固定高度、单行 flex 布局，名称/标签超出时截断。
- 候选滚动容器使用严格 `height: calc(var(--asset-picker-row-height) * 10)`，不使用 `max-height`；加载状态和哨兵置于该滚动区内，不改变可视高度。
- 外部 AKShare 候选维持独立自然高度，不混入本地 10 行列表约束。

验收逻辑：

- 无论第一页有 1、5、10 条或滚动追加更多条，本地候选下拉的可视高度恒等于 10 个标准候选行高度。
- 名称过长、历史状态标签较多时单行截断，不使任一候选行增高。
- 滚动加载下一页不改变下拉高度，且已选资产区域与页面其他内容不发生跳动。
- 增加组件测试/浏览器断言验证容器固定高度和长名称不换行。

## 已验证项

- `td/052`：queued 旧附属分析会以 `superseded_by_newer_analysis` 取消；running 旧任务会写入取消请求；同一 run/type 的查询只保留新记录。
- `td/053`：资料库分类 API 包含枚举校验、系统资产保护和乐观锁；既有 `plan_holdings` 在结构性保存后保持冻结分类。
- `td/053`：详情页已移除用户可见的强制刷新/24 小时文案，手工刷新 API 统一跳过节流；来源名称可将 `ak.fund_open_fund_info_em:累计净值走势` 转为可读文案。
- `td/053`：四个向导步骤卡片和底部操作区均使用同一宽容器；已选资产已移动到搜索框上方；本地候选支持外部点击和 Escape 关闭。

## 验证记录

- `go test ./internal/repository ./internal/service ./internal/api ./internal/jobs ./internal/simulation` 通过。
- `cd web && npm run test:ci -- app/assets/[id]/page.test.tsx app/plans/new/page.test.tsx components/plans/AssetClassHoldingPicker.test.tsx lib/format.test.ts` 通过，54 个测试通过。
- `cd web && npm run lint` 通过。
- `cd web && npm run build` 通过。
- `git diff --check` 通过。

## 文档状态

`td/052`、`td/053` 尚未完整实施：存在上述 P1/P2。当前不新增或改写 `docs` 落地文档，待问题修复并复审通过后再整理。
