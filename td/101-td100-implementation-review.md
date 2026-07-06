# 101 td/100 实施 Review

## Review 范围与方法

- Review 对象：`td/100-td099-implementation-review.md` 中 Finding 1。
- 本轮只做 review，不修改业务代码。
- 核对范围：
  - `internal/service/research_service.go` 的 source snapshot 摘要与 hash 口径；
  - `internal/service/research_service_test.go` 的锚点 hash 与 run 复用回归；
  - `docs/024-portfolio-research.md` 与 `docs/002-implemented-features.md` 的正式文档输出。
- 已执行回归：
  - `go test ./...`：通过；
  - `cd web && npm run test:ci`：通过（76 个测试文件 / 550 条用例）；
  - `cd web && npm run build`：通过。

**结论：td/100 已完整实现，未发现 P0/P1/P2 缺陷或实现缺失。组合研究已整理输出到 `docs/024-portfolio-research.md`，并在 `docs/002-implemented-features.md` 中加入入口索引。**

---

## Finding 复核

### Finding 1（P1）`source_hash` 与 `input_hash` 漏掉窗口起点前的 forward-fill 锚点

**状态：已修复。**

实现核对：

- `researchSnapshotSeries` 新增 `AnchorDate`，用于记录窗口起点前的 forward-fill 锚点；
- `summarizeResearchSeries` 统一服务资产、基准资产和 FX 序列：
  - 若窗口起点当天存在真实点位，则摘要从窗口起点开始；
  - 若窗口起点当天无真实点位且存在前置点位，则摘要从窗口起点前最后一个点位开始，并写入 `anchor_date`；
  - 锚点之前的点位不会进入摘要，符合“实际估值最小闭包”要求；
  - 摘要包含锚点日期、点位日期和值，最终进入 `source_hash`；
- `computeResearchInputHash` 继续以 `source_hash` 为输入，因此锚点变化会传导到 `input_hash`；
- worker 执行前重建 snapshot 并校验 `source_hash`，锚点在 freeze 后漂移会被拦截。

验收覆盖：

- `TestResearchSourceHashIncludesForwardFillAnchor` 覆盖：
  - 修改 forward-fill 锚点会改变 `source_hash` / `input_hash`；
  - 修改锚点之前的无关点位不会改变 hash；
  - 窗口起点当天有真实点位时不会错误记录 anchor。
- `TestResearchBacktestAnchorChangeCreatesFreshRun` 覆盖：
  - 已成功 run 之后只修改前置锚点，不会复用旧 run；
  - 新 run 的 `source_hash` / `input_hash` 均变化；
  - freeze 后再修改锚点，执行前 source 校验返回 `source data changed`。

---

## 文档归档核对

- `docs/024-portfolio-research.md` 已把 td/099 的正式能力整理为可长期维护的模块文档，覆盖信息架构、筛选器、集合、readiness、回测、结果展示、计划联动、数据模型/API 与测试范围；
- `docs/002-implemented-features.md` 已更新 migration 范围到 `0001～0025`，并加入组合研究页面入口与 `docs/024` 链接。

## 结论

本轮 review 未发现需要再次修复的问题。td/099/td/100 对应功能可以视为完成，后续只保留真实行情与浏览器人工验收作为发布前确认项。
