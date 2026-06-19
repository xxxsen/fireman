# 011 · Web 前端用户友好度规范与实现

> 设计来源：`td/040-web-ux-friendliness-refactor.md`（已完整实施并稳定）。
> 本文自洽描述 Fireman Web 端在状态反馈、组件复用、术语提示、配色 token 与布局上的统一约定与落地结果。仅涉及 `web/` 前端，不改变任何后端 API、数据库以及金额 / 权重 / 调仓 / 模拟计算口径。

## 1. 目标与范围

为保证页面在「加载 / 错误 / 空 / 正常」四种状态下都有一致、可预期、可恢复的体验，所有页面统一复用 `web/components/ui/*` 与 `web/app/globals.css` 中的设计 token，不引入新的第三方 UI 库。

退出标准：连续一轮 review 无任何 P0/P1/P2（依据 `agents.md` §8 与 `.cursor/rules/review.mdc`）。

## 2. 四态约定

每个数据驱动页面对每个关键查询都必须显式区分四态，禁止 `isLoading || !data` 这种把错误吞进永久加载的写法：

- **加载态**：`LoadingState`（或骨架 `Skeleton`）。
- **错误态**：`ErrorState`，需提供 `onRetry`（重试当前查询）与/或 `backHref`（返回上级），原始报文用 `technicalDetail` 折叠展示；错误文案统一经 `queryErrorMessage` 归一。
- **空态**：`EmptyState`，给出明确的下一步指引。
- **正常态**：渲染内容。

页面头部统一使用 `PageHeader`（标题 + 描述 + 操作区）。

## 3. 组件复用规范

- **按钮**：一律使用 `Button`（`primary | secondary | ghost | danger` + `pending`），禁止裸 `<button>` 承担主要交互。提交类操作必须有独立的 `pending` / `disabled`，防止重复提交；多个并行操作各自维护忙碌态，不得共享一个全局 `busy` 把整页按钮全禁用。
- **二次确认**：统一使用 `ConfirmDialog`（见 §4），禁止原生 `window.confirm`（仅保留浏览器“离开页面”守卫这类无法用自定义弹窗替代的场景）。
- **模态**：统一使用共享 `Dialog` / `Drawer`，不自造模态。
- **提示条**：使用 `Alert`（`info | success | warning | danger`）。同一页面多条横幅按优先级折叠，只显示最高优先级一条：**阻断错误 > 警告 > 进行中 > 成功 > 说明**。
- **变更保存**：表单类页面使用 `SaveBar`（粘性底栏，含 dirty / saving / error），并适配 `--safe-area-bottom`。

### 标杆页

`web/app/assets/page.tsx` 为四态 + 桌面表 / 移动卡片 + token 的范本，其他页面对齐其结构。

## 4. ConfirmDialog

新增 `web/components/ui/ConfirmDialog.tsx`（基于 `Dialog`）：

- Props：`open / onClose / onConfirm / title / description / confirmLabel / cancelLabel / variant('primary' | 'danger') / pending`。
- 底部按钮用 `Button`：取消为 `secondary`，确认按 `variant`；`pending` 时禁用并显示“处理中…”。
- 危险操作（删除、强制刷新、取消草稿等）使用 `variant="danger"`。

配套测试 `ConfirmDialog.test.tsx` 覆盖：打开/关闭、确认回调、pending 禁用与文案、danger 变体。

全站原 9 处 `window.confirm` 已替换为 `ConfirmDialog`：scenarios 删除、settings 恢复备份、assets/[id] 强制刷新与删除、assets 列表删除、调仓草稿（应用推荐 / 提交超额 / 取消草稿）、执行页（完成 / 取消 / 跳过单行）。

## 5. 配色 token

禁止在 `web/`（测试除外）出现硬编码 Tailwind 色阶（`slate-/emerald-/amber-/sky-/blue-/red-/gray-/zinc-/stone-` 等）。统一改用 `globals.css` 中的语义 token：

| 用途 | token 类 |
| --- | --- |
| 画布 / 卡片 / 次级面 | `bg-canvas` / `bg-surface` / `bg-surface-muted` |
| 主文字 / 次要文字 | `text-ink` / `text-ink-muted` |
| 边框 | `border-line` |
| 主品牌 | `bg-brand` / `text-brand` |
| 正向 / 危险 / 警告 / 信息 | `text-positive` / `text-danger` / `text-warning` / `text-info`（及对应 `bg-*`） |

落地范围覆盖所有页面与共享组件（含 `Tooltip`、`MetricHelp`、`SaveBar`、`StaleBanner`、`InlineTooltip`、`RebalanceFundPoolBar`、图表 `WealthPathChart` / `SensitivityCharts`、`AllocationSettings`、`AssetClassHoldingPicker`、`WizardHoldingRow`、`CashFlowEditor` 等）。校验方式：对 `web/`（排除 `*.test.tsx`）grep 上述色阶应为零命中。

## 6. 术语提示（MetricHelp / 术语表）

财务与模拟相关指标旁挂 `MetricHelp`，文案集中维护在 `web/lib/terms.ts`（`TERMS` 字典 + `helpForTerm`）。未知 key 时 `MetricHelp` 安全降级为不渲染。

### 6.1 孤儿术语 key（已记录归档）

下列 key 已在 `terms.ts` 定义，但当前未通过 `MetricHelp` / `helpForTerm` 挂载到任何界面。它们描述的是有效概念，保留以备后续界面挂载，**不作删除**；此处登记以满足“无未登记孤儿 key”的要求：

`p_quantiles`、`max_drawdown`、`annual_return`、`annual_volatility`、`weight_within_group`、`simulation_snapshot_sync`、`holdings_sum`、`holdings_market_value`、`scale_gap_under`、`plan_scale_gap_row`、`structural_rebalance`、`deviation_amount`、`rebalance_mode_full`、`rebalance_mode_new_cash`、`asset_refresh`、`asset_refresh_vs_rebalance_plan`、`current_amount_vs_target`、`portfolio_gap_row`。

> 说明：上述名称中部分与 API 数据字段同名（如 `max_drawdown`、`annual_return`、`holdings_sum`），数据字段仍在使用；此处指的是它们作为“术语表条目”尚未被挂载为帮助提示。

## 7. 布局与响应式

- 粘性底栏（`SaveBar`、调仓草稿底栏）限制在主内容区宽度内、不遮挡侧栏，并适配 `--safe-area-bottom`。
- 宽表（如调仓草稿明细）在移动端提供卡片视图；桌面表通过 `data-testid` 标识以便测试定位。

## 8. 测试与门禁

- 改动页补 / 改 Vitest（沿用 `*.test.tsx` 约定），覆盖：错误态渲染、mutation `onError` 提示、ConfirmDialog 二次确认与 pending、按钮禁用防重复提交。
- 交付门禁：`make web-lint`、`make web-test`、`make web-build` 全部通过。当前结果：lint 0 警告、test 48 文件 / 206 用例全绿、build 成功。
