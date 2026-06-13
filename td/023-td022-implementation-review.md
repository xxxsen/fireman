# Fireman `td/022` 实施复审报告

- 复审日期：2026-06-13
- 复审对象：当前工作区对 `td/022-td021-first-batch-implementation-review.md` 评审项的修复结果
- 复审范围：Web 壳层导航高亮、Tooltip 基元合并与视口约束、Web lint 门禁
- 约束：本次只 review，不修改业务代码

## 1. 结论

本轮已经关闭 `td/022` 中的全部 3 个问题：

1. `web-lint` 现在已经通过，`td/021` 第一批的质量门禁闭环已满足。
2. `/plans/new` 现在会正确高亮全局导航中的“计划”模块。
3. `MetricHelp` 与 `InlineTooltip` 已收敛到统一的 `Tooltip` 基元，并补上了基础的视口约束定位测试。

本次在复审范围内 **未发现新的实现缺陷或实现缺失问题**。

| 级别 | 数量 | 结论 |
| --- | ---: | --- |
| P0 | 0 | 未发现数据破坏问题 |
| P1 | 0 | 未发现发布阻断缺陷 |
| P2 | 0 | 未发现次级缺陷 |

`td/022` 对应评审项已完成。本次不迁移 `td/` 文档到 `docs/`，原因是本轮产物为本地 review 文档，不属于公开功能文档。

## 2. 已关闭项确认

### 已关闭-1 Web lint 门禁未闭环

确认结果：已关闭。

依据：

- `web/package.json` 的 `lint` 仍以 `--max-warnings=0` 执行；
- 本轮实测 `cd web && npm run lint` 返回 0；
- `td/022` 中列举的 `react-hooks/set-state-in-effect` 等失败项已不再阻断当前工作区。

### 已关闭-2 `/plans/new` 不高亮“计划”导航

确认结果：已关闭。

依据：

- `web/components/layout/AppShell.tsx` 已将模块判定改为：
  - `/`
  - `/plans/new`
  - `/plans/{id}/...`
  统一归入“计划”模块；
- `web/components/layout/AppShell.test.tsx` 已新增覆盖：
  - 首页高亮
  - 新建计划页高亮
  - 计划详情页高亮
  - `/assets`、`/settings` 不误高亮

### 已关闭-3 Tooltip 体系未合并且缺少视口约束

确认结果：已关闭。

依据：

- `web/components/ui/Tooltip.tsx` 已新增统一 Tooltip 基元；
- `web/components/ui/MetricHelp.tsx` 与 `web/components/ui/InlineTooltip.tsx` 已改为复用 `Tooltip`；
- `web/lib/tooltip-position.ts` 已抽出统一定位计算；
- `web/lib/tooltip-position.test.ts` 已覆盖：
  - 390px 视口内水平约束
  - 左边缘 / 右边缘回退
  - 底部空间不足时上翻
- `web/components/ui/Tooltip.test.tsx` 与 `InlineTooltip.test.tsx` 已补组件级验证。

## 3. 验证记录

已通过：

- `cd web && npm test -- --run`
- `cd web && npm run build`
- `cd web && npm run lint`

补充说明：

- 本轮复审基于代码审阅与自动化验证；未单独进行 125% / 150% 浏览器缩放下的人工视觉验收。
- 以上残余风险不构成当前轮次的实现缺陷结论。
