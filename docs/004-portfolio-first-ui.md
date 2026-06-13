# 组合优先计划界面

- 更新：2026-06-11

## 主导航

计划内页面收敛为四个入口：

| 路由 | 用途 |
| --- | --- |
| `/plans/{id}/overview` | 查看总资产、持仓合计、未分配差额、大类/地区配置和主要偏离 |
| `/plans/{id}/rebalance` | 按大类与地区查看配置缺口、筛选调仓建议、导出 CSV、记录快照 |
| `/plans/{id}/holdings` | 按大类和地区维护组内权重与真实持仓金额 |
| `/plans/{id}/settings` | 管理场景与权重、FIRE 参数、模拟/压力/敏感性分析 |

旧 `dashboard`、`targets`、`instruments`、`parameters`、`scenarios`、`analysis`
入口保留兼容跳转。

## 日常主路径

```text
组合总览
  → 调仓工作台
  → 持仓管理
  → 记录调仓后快照
  →（可选）计划设置 / FIRE 模拟
```

FIRE 模拟不再阻断计划创建。向导默认只创建计划，用户可在确认组合时勾选后台运行
10000 次模拟；创建后立即进入组合总览并显示任务进度。

## 数据扩展

- Dashboard API 增加 `region_bars`，汇总国内/国外目标与当前全组合权重。
- 计划列表增加 `rebalance_actionable_count` 与 `holdings_gap_minor`。
- 调仓汇总由前端基于 targets API 聚合，不新增数据库结构。
