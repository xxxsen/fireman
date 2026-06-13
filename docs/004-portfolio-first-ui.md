# 组合优先计划界面

- 更新：2026-06-14

> 本文记录第一阶段“组合优先”UI 收拢思路。当前正式页面职责与用户路径请优先参考 [008-计划设置、持仓预览与资产变更](./008-plan-settings-holdings-preview.md)。

## 主导航

当前计划内页面已进一步收敛为三个入口：

| 路由 | 用途 |
| --- | --- |
| `/plans/{id}/overview` | 查看总资产、持仓合计、大类/地区配置、主要偏离与关键入口 |
| `/plans/{id}/rebalance` | 作为“持仓预览”查看当前持仓、目标结构，并进入资产变更或调仓计划 |
| `/plans/{id}/settings` | 切换当前计划使用的 FIRE 方案、编辑计划参数、运行模拟 |

旧 `holdings`、`scenarios`、`dashboard`、`targets`、`instruments`、`parameters`、`analysis`
入口保留兼容跳转。

## 日常主路径

```text
组合总览
  → 持仓预览
  → 资产变更
  → 调仓计划
  →（可选）计划设置 / FIRE 模拟
```

FIRE 模拟不再阻断计划创建。向导默认只创建计划，用户可在确认组合时勾选后台运行
10000 次模拟；创建后立即进入组合总览并显示任务进度。

## 说明

本阶段文档中的以下旧说法已不再代表当前正式实现：

- `调仓工作台` 现已更名为 `持仓预览`
- 独立 `持仓管理` 页已下线并重定向
- `场景配置` 已从计划页内拆到全局 `/scenarios`

## 数据扩展

- Dashboard API 增加 `region_bars`，汇总国内/国外目标与当前全组合权重。
- 计划列表增加 `rebalance_actionable_count` 与 `holdings_gap_minor`。
- 调仓汇总由前端基于 targets API 聚合，不新增数据库结构。
