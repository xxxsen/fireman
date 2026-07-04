# 结构偏差与规模偏差分拆（调仓工作台）

- 更新：2026-06-14

> 本文保留“结构偏差 / 规模偏差分拆”的核心算法与展示语义。页面命名和用户路径已在后续迭代中更新，当前正式信息架构请参考 [008-计划设置、调仓工作台与持仓校正](./008-plan-settings-holdings-preview.md) 与 [020-web-ui-information-architecture-and-accessibility.md](./020-web-ui-information-architecture-and-accessibility.md)。

## 问题

计划总资产（配置快照）与持仓市值增值/缩水后，旧逻辑以计划规模为唯一基准，会把等比例增值判为「全面超配」、等比例缩水判为「全面欠配」，建议全体减配/增配——数学自洽但不符合「先调结构、再处理规模」的心智。

## 核心概念

| 概念 | 实现字段 | 含义 |
| --- | --- | --- |
| **结构偏差** | `structural_*` | 在**当前持仓合计**下，权重是否偏离目标；**驱动主调仓建议** |
| **规模偏差** | `scale_gap_minor` | 持仓合计 − 计划基准规模；仅提示，可一键同步基准 |

```text
计划基准 450w  →  FIRE / 录入校验用
持仓合计 500w  →  真实市值
规模偏差 +50w  →  独立状态条，不生成全体减仓建议
```

## 后端变更

- `HoldingTargetLine` 增加 `structural_*` 与 `plan_gap_*` 双轨字段。
- `RebalanceLine.action` / `suggested_trade_minor` 改为**结构**建议；`plan_scale_action` 供折叠区。
- `RebalanceSummary` 增加 `configured_total_minor`、`holdings_total_minor`、`scale_gap_minor`、`plan_scale_actionable_count`。
- Dashboard `holdings_gap_minor` 符号改为 `持仓合计 − 计划基准`（正 = 规模超出）。
- `rebalance_actionable_count` / `actionable_count` 均为结构可行动数量。

## 前端变更

### 调仓工作台 `/plans/{id}/rebalance`

1. **规模状态条**（`|scale_gap| > 1 元`）：规模超出/缺口文案 + 去持仓校正 + 暂不处理。
2. **结构偏差汇总表**：现状占比、结构还差、建议均基于 `structural_*`。
3. 空状态：无持仓 / 结构与规模均一致 / 结构无建议但规模有偏。

### 联动页面

- **组合总览**：规模超出/缺口/一致；偏离列表按结构排序。
- **持仓校正**：承担真实持仓结构与金额的统一编辑入口。
- **计划设置**：「计划基准规模」标签与持仓差额提示。

## 验收场景（摘要）

| 场景 | 期望 |
| --- | --- |
| A1 450w→500w 等比例 | 结构全 hold；规模条「超出」；主表无减配 |
| B1 450w→400w 等比例 | 结构全 hold；规模条「缺口」；主表无增配 |
| C1 450w 结构对齐 | 无规模条；绿色「结构与规模均与目标一致」 |

## 非目标

- 自动提醒同步基准
- 修改 FIRE 引擎以持仓合计为起点
- `new_cash` 模式语义（仍用 plan_scale）

---

## 持仓校正与调仓计划

用户路径与提交流程摘要见 [007-asset-refresh-rebalance-plan.md](./007-asset-refresh-rebalance-plan.md)、[008-计划设置、调仓工作台与持仓校正](./008-plan-settings-holdings-preview.md) 与 [020-web-ui-information-architecture-and-accessibility.md](./020-web-ui-information-architecture-and-accessibility.md)。

### 持仓校正 `/plans/{id}/asset-refresh`

- 多步向导：确认范围 → 录入当前资产 → 确认提交
- `POST /api/v1/plans/{id}/asset-refresh`：**单事务**更新 holdings，可选同步 `total_assets_minor`，写入 `asset_refresh_events` audit
- 入口：调仓工作台、组合总览（规模偏差带 `?reason=scale`）、规模状态条

### 调仓计划草稿

- 表：`rebalance_drafts` / `rebalance_draft_lines` / `rebalance_draft_events`（DB 持久化，禁止 localStorage 存草稿）
- 每 plan 至多一条 `status=draft`（部分唯一索引）
- 创建时冻结 structural 基准；编辑期不重算 frozen 目标/还差
- 分阶段暂存、撤销、资金池；commit 与 holdings 更新同事务
- 页面：`/plans/{id}/rebalance/plan/{draftId}`；调仓工作台「创建/继续调仓计划」
