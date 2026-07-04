# FIRE 前瞻模拟引擎、结果展示与计划控制

## 目的

Fireman 的当前 FIRE 模拟以 `forward` 模式为默认：收益率来自历史样本向长期先验收缩后的前瞻分布，风险来自冻结的月度因子、相关性和厚尾抽样。本文描述引擎、计划参数和 UI 展示的已实现约定；系统 profile identity 与 provenance 见 [016-simulation-assumption-profile-integrity.md](./016-simulation-assumption-profile-integrity.md)。

## 收益率来源

计划支持三类收益假设选择：

| 模式 | 说明 |
| --- | --- |
| `blended_prior` | 默认模式；历史几何收益在 log 空间向长期先验收缩 |
| `historical_cagr` | 旧模型回放路径，保留历史兼容 |
| `custom` | 用户为具体资产设置年化收益/波动率 override |

资产级 override 是计划配置的一部分，会进入 config hash；新增、修改或删除 override 会使旧模拟 run stale。

## 校准与风险模型

前瞻校准在模拟快照阶段完成并冻结：

- 收益率：历史几何收益与 profile prior 按样本深度和 prior strength 混合；
- 波动率：使用历史月度波动率，并受 profile 的上下限约束；
- 现金：forward run 中现金也使用校准后的确定性收益，不再跳过；
- FX：原币资产的本币收益和 FX 因子分别校准，再在路径中复合；
- 相关性：资产因子与 FX 因子必须有完整 prior；缺失不会默认为 0；
- PSD：相关矩阵需要可修复且修复幅度受阈值控制；
- 厚尾：forward run 使用 profile 冻结的 Student-t df 与 tail truncation。

引擎版本会冻结在 input snapshot 中。旧 run 使用其原始 engine version 和输入字段回放，不能被新 profile 或新计划参数隐式改变。

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
