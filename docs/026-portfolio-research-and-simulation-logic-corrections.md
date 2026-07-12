# 组合研究与 FIRE 模拟计算逻辑修正

- 状态：已实施
- 当前版本：`research_backtest_v4`、`research_optimizer_v6`、FIRE `3.5.0`（本文早期章节保留历次版本变更记录）

## 1. 目的与范围

本文记录一次跨组合研究、自动调优和 FIRE 模拟的计算正确性修正。覆盖的完整链路为：

- 普通回测：集合 readiness -> 创建任务 -> 回测引擎 -> 结果展示；
- 寻找最优组合：调优 readiness -> 候选枚举 -> worker 回测 -> Top K -> 原子应用；
- 运行模拟：simulation readiness -> 冻结快照 -> Monte Carlo worker -> 汇总 -> 路径重放。

各模块的完整产品契约分别见：

- [024-portfolio-research.md](./024-portfolio-research.md)
- [025-research-portfolio-auto-optimization.md](./025-research-portfolio-auto-optimization.md)
- [019-fire-simulation-forward-engine-and-plan-controls.md](./019-fire-simulation-forward-engine-and-plan-controls.md)

本文聚焦跨模块修正、共同不变量和回归要求。

## 2. 研究回测 v2

### 2.1 版本与结果复用

- 新回测使用 `ResearchEngineVersion=research_backtest_v2`；
- engine version 参与 input hash，v2 不复用 v1 结果；
- 已完成的历史 run 继续只读，不因读取或导出而重算。

### 2.2 阈值再平衡

对任一资产 `i`：

```text
deviation_i = abs(current_weight_i - target_weight_i)
need_rebalance = exists i: deviation_i >= rebalance_threshold
```

阈值为 0 表示关闭 threshold 再平衡。普通回测和 FIRE 模拟均采用“达到阈值即触发”的 `>=` 口径。

### 2.3 基准覆盖与数据质量

基准不参与组合共同窗口的推导，但选定后必须完整覆盖最终回测窗口：

```text
benchmark_usable_start <= window_start
benchmark_usable_end   >= window_end
```

usable 范围同时考虑基准资产和所需 FX。基准资产与基准 FX 使用和组合资产一致的非正值、连续缺口及前值填充检查，并禁止在最后一个真实点之后填平尾部。

readiness 使用稳定阻断码：

| 阻断码 | 含义 |
| --- | --- |
| `benchmark_window_not_covered` | 基准起点或终点未覆盖回测窗口 |
| `benchmark_gap_exceeded` | 基准资产在窗口内的连续缺口超限 |
| `fx_gap_exceeded` | 基准或组合所需 FX 的连续缺口超限 |

现金基准可在没有价格序列时按零收益参与比较，但其现金身份和数据质量事实必须进入结果。

### 2.4 精确贡献归因

单期资产贡献与组合收益：

```text
c_{i,t} = w_{i,t-1} * r_{i,t}
r_{p,t} = sum_i(c_{i,t})
NAV_t = NAV_{t-1} * (1 + r_{p,t})
```

累计收益贡献使用链接贡献：

```text
cumulative_contribution_i
  = sum_t((NAV_{t-1} / NAV_0) * c_{i,t})

sum_i(cumulative_contribution_i) = NAV_T / NAV_0 - 1
```

最大回撤峰值 `p` 到谷底 `q` 的贡献：

```text
drawdown_contribution_i
  = sum_(t=p+1..q)((NAV_{t-1} / NAV_p) * c_{i,t})

sum_i(drawdown_contribution_i) = NAV_q / NAV_p - 1
```

风险贡献直接使用有效估值日的实际贡献序列：

```text
risk_contribution_i = cov({c_{i,t}}, {r_{p,t}}) / var({r_{p,t}})
```

组合方差大于 0 时，风险贡献之和为 1；方差为 0 时，所有风险贡献为 `null`。结果页 tooltip 明确展示三类贡献的加总口径。

## 3. 自动调优 v2

### 3.1 冻结身份与执行输入

`OptimizationEngineVersion=research_optimizer_v2`。快照对每个资产冻结：

```text
item_id
asset_key
weight
weight_locked
adjust_policy
point_type
source summary/hash
```

worker 按 `item_id` 恢复条目身份和锁定权重。候选及最终结果必须满足：

- 锁定权重与快照一致，容差 `1e-12`；
- 所有权重有限、非负，总和为 1，容差 `1e-12`；
- 未选中的可调条目显式返回 0；
- 每个结果条目都具有非空 `item_id` 和 `asset_key`。

### 3.2 唯一候选枚举规则

设：

```text
remaining = 1 - locked_sum
full_parts = floor(remaining / weight_step)
residual = remaining - full_parts * weight_step
```

候选枚举遵循以下规则：

1. `residual <= 1e-12` 时，对非空可调资产子集枚举 `full_parts` 的正整数 composition；
2. `residual > 1e-12` 时，为每个非空子集选择 residual 接收资产，再枚举完整步长的弱 composition；
3. 按 canonical weight vector 去重；
4. `full_parts == 0 && residual > 0` 时，每个可调资产分别得到一个独占剩余权重的候选；
5. `abs(remaining) <= 1e-12` 时只生成一个候选，全部可调资产权重为 0。

`CountCandidates` 与 `GenerateCandidates` 调用同一枚举内核。可运行网格的预估数必须精确等于生成数；超过硬限制时计数在 `limit+1` 提前结束，由 readiness 阻断，避免无意义的大规模枚举。

以下边界均可运行：

- 剩余权重小于步长；
- 锁定权重恰为 100%；
- 只有一个可调资产。

无法生成候选时返回 `candidate_count_zero`。

### 3.3 候选回测和稳定排序

候选回测携带与普通回测相同的基准输入。基准摘要进入候选 summary，但不参与优化目标评分。

Top K 使用确定排序键：

```text
primary: objective score DESC
secondary: CAGR DESC
tertiary: abs(max_drawdown) ASC
final: canonical weight vector ASC
```

canonical vector 按 `item_id` 排序并使用固定 12 位小数。完全相同的候选比较结果为相等，进入 tracker 前按权重向量去重。

### 3.4 原子应用结果

结果页通过单一接口应用排名结果：

```http
POST /api/v1/research/optimizations/{optimization_id}/apply
```

请求携带 `objective`、`rank` 和预览时的 `expected_collection_updated_at`。服务端在一个事务内完成：

1. 校验 optimization 已成功且排名结果存在；
2. 校验 collection id 和 `updated_at`；
3. 校验结果条目与冻结快照、当前 collection item 的身份完全一致；
4. 写入正权重条目并启用、锁定，其他条目禁用并清零；
5. 恢复 optimization 冻结的回测窗口；
6. 更新一次 collection `updated_at` 后提交。

任一步失败均整体回滚。并发修改返回 HTTP 409 `research_collection_changed`；结果身份过期返回 HTTP 409 `research_optimization_result_stale`。前端不做逐条写入，也不乐观伪造成功状态。

## 4. FIRE 模拟 3.2.0

### 4.1 参数准入

保存计划和创建模拟均校验：

```text
0 <= transaction_cost_rate < 1
rebalance_frequency in {monthly, quarterly, annual}
```

非法参数返回 `parameters_invalid`。前端使用相同范围并在提交前阻断，而不是依赖失焦修正。

### 4.2 聚合现金流动性

3.2.0 新快照固定 `aggregate_cash_liquidity=true`，全部同币种 `is_cash=true` 槽位组成一个现金池：

- 储蓄按现金 target weight 的归一化比例分配；target 合计为 0 时按余额比例，余额也为 0 时进入冻结顺序中的第一个现金槽；
- 提款先按余额比例消耗现金，不收交易费；
- 现金不足时清空现金池，再按非现金资产余额比例卖出缺口，交易费仅基于非现金卖出额；
- 调仓仍按双边交易额收费。

每月和每年的账本恒等式为：

```text
end_wealth
  = start_wealth
  + income
  - net_spending
  - tax
  - transaction_cost
  + investment_gain_loss
```

旧快照字段缺失或为 false 时保留原单现金槽语义，保证历史代表路径可重放。

### 4.3 外币现金边界

FIRE 计划只允许与计划基准币种相同的现金。当前基准币种为 CNY，因此 USD/HKD 现金：

- 在计划资产选择器中隐藏；
- 在持仓写入时拒绝；
- 在 simulation readiness 和快照创建时以 `foreign_cash_not_supported` 阻断。

组合研究仍允许跨币种现金回测，不受此限制。

### 4.4 同类资产相关性

- 相同 `asset_key` 的重复暴露固定 `rho=1`；
- 相同 `asset_class + region`、但 asset key 不同的资产使用冻结月度历史，并向同类型先验 1 收缩。

```text
lambda = strength_months / (common_months + strength_months)
rho = (1-lambda) * rho_hist + lambda * 1
```

共同月份少于 24 或零方差时回退到 1，并记录 `correlation_prior_only`。旧 run 已冻结 FactorModel，不重建历史矩阵。

### 4.5 事实型失败状态

3.2.0 只记录账本可直接证明的失败状态：

| 状态 | 含义 |
| --- | --- |
| `insufficient_funds` | 当月 gross withdrawal 无法支付，记录 failure month |
| `wealth_depleted` | 月末总资产小于等于 0，记录 failure month |
| `terminal_floor_not_met` | 完成全部月份但期末低于 floor，无 failure month |
| `other` | 不可分类的兼容兜底 |

不再根据失败月份或累计通胀推断“支出冲击”“高通胀”等伪因果。旧快照继续使用旧状态分类，以保持历史 summary 和路径详情一致。

## 5. Web 入口契约

“运行模拟”仅在以下条件全部满足时启用：

```text
parameters loaded
holdings loaded
readiness query succeeded
readiness.ready == true
runs is an integer in [1000, 100000]
no active simulation job
create mutation not pending
```

readiness 加载中和失败时均禁用按钮；失败状态提供显式重试。模拟次数以字符串 draft 校验，通过后才转换为整数请求值。

组合研究页面同步展示基准覆盖阻断原因；调优入口允许单一可调资产和锁定 100% 的固定候选；应用结果只发送一次 apply 请求。

## 6. 保持不变的计算口径

除下述 v4 交易成本升级外，以下已发布契约保持不变：

- 研究 Sharpe 仍为 `(CAGR - risk_free_rate) / annual_volatility`；
- 研究初始资金只用于把交易成本四舍五入为 minor unit，不改变无现金流收益率口径；
- FIRE 月内顺序仍为收入 -> 支出/税 -> 调仓/交易费 -> 月收益 -> 通胀推进，月 0 不初始调仓；
- FIRE 成功仍以名义期末资产与 `terminal_wealth_floor_minor` 比较；
- Student-t drift、波动率缩放、FX 复合和分位数插值公式保持不变。

历史 research v1 和 optimizer v1 结果仍可读取；simulation 3.1.0 及更早快照继续按冻结字段和版本门控重放。新任务只使用本文件列出的当前版本契约。

### 6.1 研究交易成本 v4 / 优化 v5

`research_backtest_v4` 在有效估值日收盘收益之后、恢复目标权重之前按单边换手收费：

```text
turnover = 0.5 * sum_i(abs(weight_before_i - target_weight_i))
cost = round(nav_before_rebalance_minor * turnover * transaction_cost_rate)
nav_after_rebalance = nav_before_rebalance - cost
```

首次建仓和 `buy_hold` 不收费；月/季/年再平衡发生在该周期最后一个有效估值日，
周末和节假日不产生独立费用点。扣费后的 period return 进入 CAGR、回撤、波动率、
CVaR、基准比较和贡献归因。summary 冻结 `total_turnover`、
`total_transaction_cost_minor` 和相对同调仓路径不计费终值的
`transaction_cost_drag`。`research_optimizer_v5` 及 `research_optimizer_v6` 的所有候选复用同一公式和有效日历，
因此费用会参与候选排序。旧回测 v3、优化 v4 及更早结果保持只读，页面明确标记未计成本。

## 7. 正确性约束与回归范围

自动化测试覆盖以下关键不变量：

| 范围 | 覆盖内容 |
| --- | --- |
| 普通回测 | 55/45 阈值边界、基准早起/晚止/缺口/现金、累计收益与回撤贡献加总、风险贡献加总、零方差 null |
| 自动调优 | 锁定身份经真实 worker 保留、剩余权重小于步长、锁定 100%、单一可调资产、稳定并列排序、基准摘要、事务回滚、CAS 冲突 |
| FIRE | 交易费率边界、多现金提款和储蓄、账本恒等、外币现金阻断、同类历史相关性、重复标的 rho=1、事实失败状态、历史与当前版本回放 |
| Web | readiness loading/error/retry、模拟次数整数边界、调优边界入口、单次 apply、CAS 提示、基准 blocker、失败状态文案 |

真实 optimization fixture 会创建任务、执行 worker、读取 Top K 并原子应用 rank 1，直接验证候选数一致、A=20% 锁定不变、item identity、基准摘要、冻结窗口和最终 100% 权重。

工程门禁：

```bash
make build
make test
make install-golangci-lint
make lint-go
make web-install
make web-lint
make web-test
make web-build
make integration-test
```
