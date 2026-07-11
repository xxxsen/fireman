# FIRE 前瞻模拟引擎、结果展示与计划控制

## 目的

Fireman 的当前 FIRE 模拟以 `forward` 模式为默认：收益率来自历史样本向长期先验收缩后的前瞻分布，风险来自冻结的月度因子、相关性和厚尾抽样。本文描述引擎、计划参数和 UI 展示的已实现约定；系统 profile identity 与 provenance 见 [016-simulation-assumption-profile-integrity.md](./016-simulation-assumption-profile-integrity.md)。

## 收益率来源

计划支持三类收益假设选择：

| 模式 | 说明 |
| --- | --- |
| `blended_prior` | 默认模式；历史几何收益在 log 空间向长期先验收缩 |
| `historical_cagr` | 使用冻结历史 CAGR 与独立因子抽样；新任务仍使用当前引擎版本 |
| `custom` | 用户为具体资产设置年化收益/波动率 override |

资产级 override 是计划配置的一部分，会进入 config hash；新增、修改或删除 override 会使旧模拟 run stale。

## 校准与风险模型

前瞻校准在模拟快照阶段完成并冻结：

- 收益率：历史几何收益与 profile prior 按样本深度和 prior strength 混合；
- 波动率：使用历史月度波动率，并受 profile 的上下限约束；
- 现金：forward run 中现金也使用校准后的确定性收益，不再跳过；
- FX：原币资产的本币收益和 FX 因子分别校准，再在路径中复合；
- 相关性：资产因子与 FX 因子必须有完整 prior；缺失不会默认为 0；
- 同一 `asset_key` 的重复暴露固定 `rho=1`；同资产大类和地区但不同标的使用冻结月度历史，并向同类 prior `rho=1` 收缩。共同月份不足 24 或零方差时回退 prior 并记录 `correlation_prior_only`；
- PSD：相关矩阵需要可修复且修复幅度受阈值控制；
- 厚尾：forward run 使用 profile 冻结的 Student-t df 与 tail truncation。

引擎版本会冻结在 input snapshot 中并参与输入哈希；新建模拟使用当前版本，结果可追溯到其冻结参数、因子模型和市场快照。

## 3.3 现金池、退休稳定收入与失败状态

当前引擎版本为 `3.3.0`，input snapshot 写入 `aggregate_cash_liquidity=true`：

- 所有 `is_cash=true` 持仓组成现金池。工作期储蓄按现金目标权重归一分配；现金目标合计为 0 时按余额比例，余额也为 0 时进入第一个现金槽；
- 提款按余额比例先使用全部现金，不收交易费；现金不足时仅按非现金资产余额比例卖出，交易费只基于非现金卖出额；
- 计划只允许与基准币种一致的现金。当前计划基准币种为 CNY，因此 USD/HKD 现金在选择器中隐藏，并在持仓写入、simulation readiness 和快照创建三处以 `foreign_cash_not_supported` 阻断；
- `transaction_cost_rate` 必须在 `[0,1)`，`rebalance_frequency` 仅允许 `monthly/quarterly/annual`；
- 路径只记录账本可证明的失败状态：`insufficient_funds`、`wealth_depleted`、`terminal_floor_not_met`、`other`，不再根据失败月份或累计通胀推断因果。

计划参数还包含 `annual_retirement_income_minor` 与
`annual_retirement_income_growth_rate`。前者是 FIRE 后可用于生活的税后稳定净收入，
如养老金、净租金或长期副业收入；它不应重复包含在工作期年储蓄中。其月度时点固定为：

- `month < retirement_month`：只计入按年度增长的 `annual_savings_minor`；
- `month >= retirement_month`：从 FIRE 月开始计入稳定收入，周年按增长率调整；
- 稳定收入先进入现金池，再支付当月支出，剩余部分参与当月收益并进入月度、年度收入账本。

两个字段属于 config hash 和冻结 snapshot。修改任一字段会使旧 run stale；3.2.0
及更早 snapshot 缺少字段时按零收入回放，保持历史结果不变。

3.2.0 之前的快照缺少该现金池标志，重放时继续使用最后一个现金槽和旧失败标签；版本判断集中在 simulation 包，不由页面推断。

分析页只有在参数和持仓加载完成、readiness 请求成功且 ready、模拟次数是 `[1000,100000]` 内整数、没有活动模拟任务时才允许运行。readiness 加载或失败以及次数为空/小数/越界都会禁用按钮。

## 真实购买力与情景对比

模拟同时保留名义财富和以起点购买力计的真实财富：

- 月度真实分位序列独立持久化；
- 路径详情展示名义/真实财富切换；
- 每条路径记录累计通胀；
- 情景对比使用同一个冻结计划输入和 shared seed，只切换 scenario，便于比较 conservative/baseline/optimistic 差异。

## Pxx 与路径展示

分析页的 Pxx 语义统一以终值分位路径为准：

- 汇总卡片展示主要终值分位；
- 不再同时展示容易误读的 terminal quantiles 网格；
- 代表路径按 P10/P50/P90 等顺序稳定展示；
- 路径年度表保留年末回撤，并新增年末收益率；
- 年末配置明细按需通过 tooltip/查看控件展开，避免宽表挤压。

## 新建计划向导与参数校验

新建计划向导将目标、持仓和确认流程收敛为更少步骤，并把高级 FIRE 参数默认折叠：

- 预计 FIRE 时长以输入为主，预设只是回填建议；
- guardrail、随机通胀、退休年龄、结束年龄等高级参数在提交前校验；
- 地区目标为 0% 时，不能保留或新增该地区持仓；
- `end_age` 必须大于 `retirement_age`；
- guardrail 下限不能大于上限；
- 固定通胀等高风险输入需要明确确认。

服务端和前端共享同一组边界认知：前端负责即时反馈，服务端负责最终拦截，保证无效计划不会创建后才在模拟阶段失败。

## 持久化与 stale 规则

会影响模拟输入的计划字段都会进入 config hash，包括：

- 收益假设选择、profile id/version、scenario；
- 资产级收益/波动率 override；
- 现金流、通胀、guardrail、年龄和 FIRE 时长；
- 当前持仓、目标配置、资产分类和历史快照。

计划参数变更后，旧 run 标记为 stale；历史 run 的 input snapshot、月度收益序列和 profile provenance 保持冻结。

## 验证重点

- 固定 seed 的 P50 回归基线。
- 现金收益、FX 校准、相关性缺失阻断、PSD 修复阈值和 tail truncation freeze。
- 情景对比共享 seed，且只改变 scenario。
- 计划参数 round-trip 不丢失收益假设字段。
- 高级 FIRE 参数边界在 API 和向导两侧都被拦截。
- 多现金槽提款/储蓄、非现金卖出交易费与月/年账本恒等式。
- 同类不同标的历史相关性收缩、精确重复标的 `rho=1`。
- 事实型失败状态与期末 floor 状态。
