# td/041 实施 Review

## 结论

当前实现已修复 `td/041` 指出的两个首屏永久 loading 问题，但**仍未达到** [`docs/011-web-ux-friendliness.md`](/home/sen/work/fireman/docs/011-web-ux-friendliness.md:1) 所宣称的“已完整实施并稳定”状态。

本轮 review 确认了 1 个明确 bug、1 个实现/文档收口问题。

## Findings

### P1 · 调仓草稿页对 `holdings` 查询失败静默降级，会把“接口失败”误判成“没有现金持仓”

- 位置：
  - [`web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx:77)
  - [`web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx:109)
  - [`web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx:223)
  - [`web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx:553)
  - [`web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/rebalance/plan/[draftId]/page.tsx:612)
- 现象：
  - 页面现在只对 `plan` / `draft` 首屏失败进入 `ErrorState`，`holdings` 不在错误态判断里。
  - `cashHolding` 直接从 `holdings.data?.holdings ?? []` 推导；当 `holdings` 请求失败时，这里会退化成空数组。
  - 后续预览/提交逻辑会把“拿不到持仓数据”当成“计划中没有现金持仓”，从而禁用“确认提交”或显示“请先添加 CNY 现金”的提示。
- 影响：
  - 这不是纯展示问题，而是把后端失败误导成业务事实，直接影响用户对“未分配资金如何处理”的判断。
  - 用户会被错误地引导去补现金持仓或接受组合规模下降，而真实问题其实是 `holdings` 查询失败。
- 修复方案：
  - 将 `holdings` 纳入页面关键查询四态判断。首屏进入调仓草稿前，必须保证 `plan`、`draft`、`holdings` 三个查询都成功；其中任一失败且无可用数据时，统一展示 `ErrorState`，并在 `onRetry` 中同时重试失败查询。只有三者都可用时才允许进入预览、现金 sweep 和提交逻辑。
- 验收逻辑：
  1. mock `getHoldings` 首次 reject，`getPlan` / `getRebalanceDraft` 正常返回。
  2. 页面应展示 `ErrorState`，而不是进入调仓草稿主界面。
  3. 不得出现“计划中尚无现金持仓”“接受组合规模下降”等业务提示。
  4. 点击重试后，在 `getHoldings` 成功时页面恢复正常，预览中正确出现现金持仓分配逻辑。

### P1 · `analysis` 仍未对关键查询实现显式四态，`docs/011` 继续提前收口

- 文档位置：
  - [`docs/011-web-ux-friendliness.md`](/home/sen/work/fireman/docs/011-web-ux-friendliness.md:8)
  - [`docs/011-web-ux-friendliness.md`](/home/sen/work/fireman/docs/011-web-ux-friendliness.md:14)
- 代码证据：
  - [`web/app/plans/[id]/analysis/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.tsx:303) 同时发起 `paramsQ`、`holdingsQ`、`simsQ`、`stressQ`、`sensQ` 等关键查询。
  - 但页面主体只直接消费这些查询的 `data`，没有任何页面级 `isLoading` / `isError` / `ErrorState` 分支；例如：
    - `paramsQ` 失败时，`runs` 会静默回退到 `10000`（[`web/app/plans/[id]/analysis/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.tsx:312)）。
    - `holdingsQ` 失败时，`snapshotWarningLabels` 退化为空数组，页面不会提示用户“持仓快照数据不可用”（[`web/app/plans/[id]/analysis/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.tsx:447)）。
    - `simsQ` / `stressQ` / `sensQ` 失败时，页面会表现成“暂无结果”或只剩按钮，但没有任何错误提示（[`web/app/plans/[id]/analysis/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.tsx:471) 起）。
- 影响：
  - `analysis` 已做按钮与视觉迁移，但失败路径仍是静默降级，和 `docs/011` 写明的“每个关键查询显式区分四态”不一致。
  - 文档继续标注“已完整实施并稳定”，会掩盖分析页仍未收口的失败路径问题。
- 修复方案：
  - 在 `analysis` 中把关键查询按职责拆成显式状态：
    - 页面进入所必需的数据（至少 `paramsQ`、`holdingsQ`）在无缓存且失败时展示页面级 `ErrorState`。
    - 结果列表类查询（`simsQ`、`stressQ`、`sensQ`）在失败时展示模块级 `Alert` / `ErrorState`，不得静默退化成“暂无结果”。
    - `docs/011` 仅在上述失败路径补齐并完成验证后，才保留“已完整实施并稳定”的表述。
- 验收逻辑：
  1. mock `getParameters` 或 `getHoldings` 首次 reject，页面应进入明确错误态，而不是默认显示可运行的分析面板。
  2. mock `listSimulations` / `listStressTests` / `listSensitivityTests` reject，页面对应模块必须显示失败提示，而不是静默显示空结果。
  3. 补 Vitest 覆盖上述失败路径。
  4. 失败路径补齐后，再更新 `docs/011` 为最终完成态文档。

## 验证记录

- `cd web && npm run lint`：通过。
- `cd web && npm run test:ci`：通过，48 个测试文件 / 208 个用例全绿。
- `cd web && npm run build`：通过。
