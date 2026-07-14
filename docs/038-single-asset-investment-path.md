# 单资产投入路径历史实验

## 1. 能力边界

“单资产投入路径实验”位于“数据研究”模块，与“组合研究”并列，用本地冻结历史回答两类资金问题：

- `income_dca`：工资结余按月才产生，比较固定月投与完全相同外部现金流留在零收益现金中的结果；
- `existing_capital`：本金在计划起点已经全部存在，比较一次性投入、最多三个固定分批月数，以及可选的静态资产/现金和阈值再平衡。

该能力不搜索最佳参数，不给出资产或策略推荐，不预测未来收益或 FIRE 成功率，也不修改研究集合、FIRE 计划、持仓和调仓草稿。现金是引擎内部的基准币种零收益账本，不是第二个可选风险资产。

页面入口：

```text
/research/investment-paths
/research/investment-paths/runs/{run_id}
```

主导航只暴露“数据研究”，研究区内部使用二级侧边导航在“单资产实验”和“组合研究”之间切换。单资产实验不是研究集合的子能力，不要求先建立组合。

计算引擎版本为 `single_asset_investment_path_v2`。

## 2. 输入与策略

公共输入包括一个 active 非现金资产、精确的 `adjust_policy/point_type` 历史身份、基准币种、历史评估范围、`12..360` 月窗口、`1..28` 月内计划日和统一交易成本率。

资产从完整的本地市场资产目录搜索，不以“已经存在本地历史”作为可选条件。选择后页面读取该资产的默认回报分析维度；若没有可用历史，用户可直接创建或复用标准 `asset_history_sync` 任务。页面跟踪任务直到终态，同步期间禁止 readiness 和 run 创建，完成后自动刷新历史状态。任务创建、默认复权/点位规则、同维度 active task 去重和失败信息均与资产详情页共用；现金资产不会出现在该实验的候选结果中。

工资型定投输入：

- 起点额外本金可以为零；
- 固定月投入必须为正；
- 起始月月投仍会发生，额外本金与该月月投同时进入现金账本；
- 固定生成 `income_dca` 与 `income_cash_baseline`，不存在提前投入未来工资的一次性基准。

存量资金输入：

- 全部本金在计划起点作为唯一外部现金流进入账户；
- `lump_sum` 始终存在；
- `phase_in_Nm` 把本金在月份 `0..N-1` 等额分配，整数余数归最后一笔；
- 启用阈值对照时同时生成同权重 `static_W` 与 `threshold_W_T`，用于区分保留现金和再平衡的影响。

一个 run 最多六条策略、600 个月度滚动起点和 8,000,000 strategy path-days。

## 3. 时间、行情与成交

滚动起点是评估范围内每个自然月的计划日。窗口终点为计划起点加固定月数的 end-exclusive 日期；主窗口为空时使用最晚的完整窗口。

“完整窗口”要求首个可交易点距窗口起点、最后可交易点距窗口终点以及窗口内部相邻可交易点的缺口都不超过该资产的容忍范围。资产历史开始前的计划日不能成为滚动起点，不能把上市或净值历史开始前的长期计划投入先积压为现金、再在首个历史点集中买入。

计划日与成交日严格分离：

- 外部现金流在计划日进入现金账本；
- 计划日没有资产真实点时，待买预算留在现金中；
- 待买预算只在计划日当日或之后的第一个资产真实点合并成交；
- 前值填充和 FX 单独更新可用于估值，不能制造资产成交点；
- 资产与基准币种不同，估值日使用资产点和 FX 点的联合日历，按研究模块相同的 CNY 路由与有界前值填充换算；
- 资产/FX 缺口超过容忍范围的窗口不能运行。

复权点位产生的是内部合成单位：

```text
synthetic_units = net_purchase_minor / base_currency_adjusted_price
asset_value_minor = round(synthetic_units * current_base_currency_adjusted_price)
```

API 和页面只展示金额，不把合成单位描述为真实股数或份额。

## 4. 账本、费用与收益

每日事件顺序为：先估值昨日持仓，再按现金流发生前单位净值接收外部现金流，然后在资产真实点执行到期买入，最后执行阈值检查并记录日终。

每个日终点必须满足：

```text
account_value_minor = asset_value_minor + cash_value_minor
cash_value_minor >= 0
```

买入预算的费用为：

```text
fee_minor = round(gross_budget_minor * transaction_cost_rate)
net_purchase_minor = gross_budget_minor - fee_minor
```

首次建仓、月投和分批买入均计费。静态/阈值初始配置和阈值恢复使用双资产单边换手口径；偏离等于阈值时触发。扣费后净买入不为正会阻断整个 run。

结果同时保留三套不能混用的口径：

- XIRR：使用实际外部现金流日期的资金加权年化收益；无业务有效根时为 `null` 并带稳定原因；
- 单位化 TWR 与回撤：外部现金流按流入前单位净值增发账户单位，新增本金本身不改变单位净值；
- 本金浮亏体验：用账户价值减累计投入计算最大本金缺口、低于本金最长天数和首次恢复日期。

累计损益率不是年化收益。滚动窗口比例只表示本次冻结历史中完整配对窗口的计数，不是未来胜率。

结果页为累计投入、期末资产、投资损益、XIRR、时间加权年化、单位净值最大回撤、低于本金时间、交易成本、滚动窗口和分位数提供就地说明。账户价值图必须显示日期横轴、基准币种金额纵轴和单位，并支持鼠标、触摸及键盘查看单日账户价值与累计投入。

## 5. 不可变 run 与任务

顺序迁移 `0002_single_asset_investment_path.sql` 新增：

- `research_investment_path_runs`：不可变输入、来源身份、摘要和完成时间；
- `research_investment_path_points`：只保存主窗口逐日路径；
- `research_investment_path_trades`：只保存主窗口真实交易；
- `research_investment_path_windows`：保存全部策略和月度起点摘要。

生命周期只读取关联 `worker_tasks`。task type 为 `single_asset_investment_path_backtest`，result key 前缀为 `single_asset_investment_path_run:`。任务成功提交时 `progress_current` 与 `progress_total` 相等；结果页对终态显示完成状态，不继续展示执行中阶段文案。

创建前重新执行 readiness。同一 input hash 的 active/complete run 复用；同资产不同输入的 active task 返回冲突。Worker 重新读取当前本地历史并比较冻结 source/input hash，来源漂移返回 `investment_path_source_changed`。结果表、run 完成时间和 task complete 在同一事务中发布；失败或取消不写部分结果。

## 6. API

```text
POST /api/v1/research/investment-paths/readiness
POST /api/v1/research/investment-path-runs
GET  /api/v1/research/investment-path-runs
GET  /api/v1/research/investment-path-runs/{run_id}
GET  /api/v1/research/investment-path-runs/{run_id}/points?strategy_key=...
GET  /api/v1/research/investment-path-runs/{run_id}/trades?strategy_key=...
GET  /api/v1/research/investment-path-runs/{run_id}/windows?strategy_key=...
GET  /api/v1/research/investment-path-runs/{run_id}/export.csv
```

资产选择和缺失历史补齐复用市场资产接口：

```text
GET  /api/v1/market-assets
GET  /api/v1/market-assets/by-key?asset_key=...
POST /api/v1/market-assets/history-sync
```

`strategy_key` 必须属于 run 的冻结策略集合。列表分页上限为 1000；CSV 只读取已持久化窗口摘要和主窗口交易，不重新读取行情或计算。

## 7. 验证不变量

测试必须覆盖：研究区二级导航；无历史资产可选择、同步任务创建失败和 active task 锁定；现金资产排除；资金来源 union、金额和日期边界；周末计划日排队；现金基线同流；首次费用；分批预算余数；阈值 `>=`；XIRR 已知值和无根；单位净值回撤；账户恒等；滚动分位数和配对计数；外币 FX 日历；来源漂移；active 复用/冲突；取消不发布；迁移升级幂等；API 与 CSV。

完整交付门禁：

```bash
make ci
make integration-test
```
