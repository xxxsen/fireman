# Fireman `td/021` 第一批实施复审报告

- 复审日期：2026-06-13
- 复审对象：当前工作区未提交的 Web UI 重构改动
- 对照基线：`td/021-uiux-refine-plan.md` Phase 1、Phase 2、以及“第一批建议修改”
- 说明：仓库中的 `td/012` 是上一轮 review 文档，不是本批 UI 实施方案；当前代码改动实际对应 `td/021` 第一批，因此本次按 `td/021` 复审
- 约束：本次只 review，不修改业务代码

## 1. 结论

当前实现已经完成了第一批中的大部分骨架工作：

1. 设计 token、基础输入样式、`Button` / `PageHeader` / `EmptyState` / `ErrorState` / `Dialog` / `Drawer` 已落地；
2. `AppShell`、`PlanContextBar`、`PlanTabs`、首页、资产资料库已经切到新的页面骨架；
3. `npm test -- --run` 与 `npm run build` 均已通过。

但本批改动 **还不能判定为完整闭环**。存在 1 个发布阻断实现缺口、2 个次级问题：

| 级别 | 数量 | 结论 |
| --- | ---: | --- |
| P0 | 0 | 未发现数据破坏问题 |
| P1 | 1 | `td/021` 明确要求的 `make web-lint` 通过条件未满足 |
| P2 | 2 | 存在 1 个导航态 bug，1 个 tooltip 体系未闭环问题 |

因此当前批次 **不建议按“已完成 `td/021` 第一批”归档**。

## 2. 已完成项确认

本次确认已完成：

- `web/app/globals.css` 已引入设计 token、背景、输入态与基础动效；
- `web/components/ui/` 已新增第一批所需的核心基础组件，并补充 Vitest；
- `web/components/layout/AppShell.tsx`、`PlanContextBar.tsx`、`PlanTabs.tsx` 已完成第一轮壳层重构；
- `web/app/page.tsx` 与 `web/app/assets/page.tsx` 已补齐加载、空状态、错误状态和移动端列表模式；
- `web/app/page.test.tsx`、`web/app/assets/page.test.tsx` 已覆盖首页/资产库的主要状态与主要操作唯一性。

## 3. P1 问题

### P1-1 当前实现未满足 `td/021` 的 lint 闭环要求

性质：实现缺失。

定位：

- `td/021-uiux-refine-plan.md:759-760`
- `web/package.json:9`
- `web/eslint.config.mjs:1-22`

问题：

`td/021` 第 12 节明确要求：**每个 Phase 必须保持 `make web-test`、`make web-lint` 和 `make web-build` 通过**。当前工作区虽然已经满足：

- `npm test -- --run`
- `npm run build`

但 `npm run lint` 仍失败，实测输出为 `13 problems (8 errors, 5 warnings)`，其中阻断项主要集中在：

- `web/app/plans/[id]/analysis/page.tsx`
- `web/app/plans/[id]/asset-refresh/page.tsx`
- `web/app/plans/[id]/holdings/page.tsx`
- `web/app/plans/[id]/parameters/page.tsx`
- `web/app/plans/[id]/rebalance/page.tsx`
- `web/app/plans/new/page.tsx`
- `web/components/plans/AllocationSettings.tsx`

主因是启用了 `eslint-config-next/core-web-vitals` 之后，现有页面存在多处 `react-hooks/set-state-in-effect`、`react-hooks/exhaustive-deps` 和 `@typescript-eslint/no-unused-vars` 告警。

这意味着第一批 UI 重构虽然完成了壳层和基础组件迁移，但 **没有把 `td/021` 约定的质量门禁一起闭环**。

闭环方案：

1. 对上述页面中“把 query / props 同步写回本地 state”的 `useEffect` 逐一收敛，改成以下单一路径：
   - 能直接从 query / props 推导的值，改为渲染期推导或 `useMemo`；
   - 仅用于表单初始值且需要用户后续编辑的，改为显式初始化流程，避免在 effect 里同步 `setState`；
   - 仍需 effect 的场景，改为外部订阅/副作用，不在 effect 体内同步写派生状态。
2. 清理 `exhaustive-deps` 与未使用变量告警，确保当前启用的 `eslint-config-next` 规则集下无残留告警。
3. 以 `make web-lint` 为准重新收口，不以“测试通过”替代 lint 闭环。

验收逻辑：

1. `make web-lint` 或 `cd web && npm run lint` 必须返回 0。
2. 上述 7 个文件中不再出现 `react-hooks/set-state-in-effect`、`react-hooks/exhaustive-deps`、`@typescript-eslint/no-unused-vars` 失败项。
3. `make web-test` 与 `make web-build` 继续通过，证明修复未破坏当前交互与构建。

## 4. P2 问题

### P2-1 `/plans/new` 未被识别为“计划”模块，导致全局导航丢失选中态

性质：Bug。

定位：

- `web/components/layout/AppShell.tsx:15-23`

问题：

`AppShell` 通过：

```ts
const inPlan = pathname.startsWith("/plans/") && !pathname.startsWith("/plans/new");
```

来判断是否高亮全局导航中的“计划”。这会导致：

- `/` 时高亮“计划”
- `/plans/{id}/...` 时高亮“计划”
- **`/plans/new` 时反而不高亮任何导航项**

而 `td/021` 的壳层目标是让用户在任意页面都能清楚判断自己处于哪个模块；新建计划页属于明确的“计划”主路径，当前无选中态会打断模块定位。

修复方案：

把全局导航的模块匹配从“是否进入已有计划详情”改为“是否处于计划模块路由”：

1. `计划` 导航项应匹配 `/`、`/plans/new`、`/plans/{id}/...`；
2. `PlanTabs` 仍只在 `/plans/{id}` 布局内展示，不影响详情页子导航边界。

验收逻辑：

1. 访问 `/` 时，“计划”保持高亮。
2. 访问 `/plans/new` 时，“计划”必须高亮。
3. 访问 `/plans/{id}/overview` 时，“计划”仍保持高亮，且 `PlanTabs` 正常显示。
4. 访问 `/assets`、`/settings` 时，不会误高亮“计划”。

### P2-2 Tooltip 体系未完成合并，且当前定位方式无法满足窄屏不溢出要求

性质：实现缺失。

定位：

- `td/021-uiux-refine-plan.md:750`
- `td/021-uiux-refine-plan.md:817-819`
- `web/components/ui/MetricHelp.tsx:20-79`
- `web/components/ui/InlineTooltip.tsx:18-70`

问题：

`td/021` Phase 1 明确把“tooltip 合并”列为范围项，人工验收又要求：

- tooltip 不超出视口；
- 390 × 844 与 150% 缩放下仍可用。

但当前实现仍保留两套独立逻辑：

1. `MetricHelp` 自己维护 open/close/timer/定位；
2. `InlineTooltip` 再复制一套几乎相同的 open/close/timer/定位逻辑。

同时，两者的定位都还是固定绝对定位：

- `MetricHelp`：`left-1/2 -translate-x-1/2 w-60`
- `InlineTooltip`：`right-0 w-64`

这类固定宽度 + 静态对齐方式，在靠近屏幕边缘、窄屏、缩放或长文案场景下，仍然有明显的视口溢出风险，不满足 `td/021` 的验收目标。

闭环方案：

1. 抽出一个统一的 Tooltip 基元，收敛：
   - open / close 行为；
   - hover / focus / click 触发；
   - 延时关闭；
   - 可访问属性。
2. 在该基元中加入视口约束定位，确保提示框能根据触发点自动向左/向右/居中调整，不依赖固定 `left-1/2` 或 `right-0`。
3. `MetricHelp` 与 `InlineTooltip` 全部改为复用该基元；若 API 足够统一，可直接合并为单一组件。

验收逻辑：

1. 仓库中不再保留两套重复的 tooltip 状态机与定位实现。
2. 在 390 × 844 视口、125% / 150% 浏览器缩放下，tooltip 始终完整显示在视口内，不产生整页横向滚动。
3. `MetricHelp` 与 `InlineTooltip` 现有用例继续通过，并新增靠左、靠右触发点的边缘定位测试。

## 5. 验证记录

已通过：

- `cd web && npm test -- --run`
- `cd web && npm run build`

未通过：

- `cd web && npm run lint`
  - 结果：`13 problems (8 errors, 5 warnings)`

补充说明：

- 当前 review 只针对 `td/021` 第一批实施范围，不评价 Phase 3 之后尚未进入改造的业务页。
- 本文档为本地 review 结果，不应迁移到 `docs/`。
