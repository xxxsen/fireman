# td/042 实施 Review

## 结论

当前实现已经修复 `td/042` 明确提出的两类静默失败问题：

- 调仓草稿页已将 `holdings` 纳入首屏错误态。
- 分析页已为 `paramsQ` / `holdingsQ` 失败和三类结果列表失败补充错误提示。

但本轮变更仍存在 2 个行为缺陷，因此 [`docs/011-web-ux-friendliness.md`](/home/sen/work/fireman/docs/011-web-ux-friendliness.md:1) 还不应视为最终完成态文档。

## Findings

### P1 · 资产变更页忽略 active rebalance execution 查询失败，可能在未知执行状态下开放资产变更

- 位置：
  - [`web/app/plans/[id]/asset-refresh/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:85)
  - [`web/app/plans/[id]/asset-refresh/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:303)
  - [`web/app/plans/[id]/asset-refresh/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:340)
- 现象：
  - 页面会查询 `activeExecution`，用于在存在进行中调仓执行时阻止资产变更。
  - 当前错误态判断只包含 `plan` / `holdings` / `targets` / `instruments`，没有包含 `activeExecution.isError`。
  - 如果 `getActiveRebalanceExecution` 首次失败且无缓存，`activeExecution.data` 为空，页面会继续渲染资产变更主流程，等同于把“无法确认是否有进行中执行”当成“没有进行中执行”。
- 影响：
  - 资产变更和调仓执行是互斥工作流。active execution 状态不可用时继续开放资产变更，可能绕过既有并发保护，造成用户在调仓执行未完成时修改持仓事实。
- 修复方案：
  - 将 `activeExecution` 纳入资产变更页关键查询四态。首屏进入资产变更流程前，必须确认 active execution 查询成功；若 `activeExecution.isError && !activeExecution.data`，展示 `ErrorState`，`onRetry` 重试 `activeExecution.refetch()`，`technicalDetail` 使用 `queryErrorMessage(activeExecution.error)`。只有 active execution 查询成功且确认为无执行时，才允许进入资产变更主流程。
- 验收逻辑：
  1. mock `getActiveRebalanceExecution` 首次 reject，其他查询正常返回。
  2. 页面应展示 `ErrorState`，不得出现“1. 说明”或资产变更表单。
  3. 不得允许提交资产变更。
  4. 点击重试后，mock 返回 `{ execution: null }` 时页面恢复主流程；mock 返回进行中执行时页面显示阻断提示并链接到执行页。

### P1 · 分析页在参数/持仓仍加载时就允许运行模拟，可能使用默认模拟次数启动任务

- 位置：
  - [`web/app/plans/[id]/analysis/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.tsx:321)
  - [`web/app/plans/[id]/analysis/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.tsx:330)
  - [`web/app/plans/[id]/analysis/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.tsx:480)
  - [`web/app/plans/[id]/analysis/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.tsx:490)
- 现象：
  - 页面已补 `paramsQ` / `holdingsQ` 的错误态，但没有补首屏加载态。
  - 首次渲染时 `paramsQ.data` 为空，`runs` 会回退到默认值 `10000`。
  - 同一帧页面已经显示“运行模拟”按钮，按钮只受 `startMut.isPending || jobBusy` 控制，不会等待 `paramsQ` / `holdingsQ` 加载完成。
- 影响：
  - 如果用户在参数查询返回前点击“运行模拟”，会用默认 `10000` 次启动模拟，而不是计划里实际配置的 `simulation_runs`。
  - 这违反 `docs/011` 对关键查询加载态的约定，也会产生真实计算口径偏差。
- 修复方案：
  - 在 `AnalysisContent` 中增加首屏关键数据加载态：当 `paramsQ.isLoading || holdingsQ.isLoading || !paramsQ.data || !holdingsQ.data` 且尚无错误态时，渲染 `LoadingState`，不展示运行按钮。`runs` 只在 `paramsQ.data` 可用后计算并传给 mutation；页面级错误态保持当前实现。
- 验收逻辑：
  1. mock `getParameters` 延迟 pending，渲染页面。
  2. 页面应显示加载态，不出现“运行模拟”按钮。
  3. mock 返回 `simulation_runs: 20000` 后，页面显示“运行模拟”。
  4. 点击运行后断言 `createSimulation` 收到 `{ runs: 20000 }`，而不是默认 `10000`。

## 验证记录

- `cd web && npm run lint`：通过。
- `cd web && npm run test:ci`：通过，48 个测试文件 / 218 个用例全绿。
- `cd web && npm run build`：通过。
