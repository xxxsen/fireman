# TD 061 / TD 062 实施评审

## 文档信息

| 项 | 内容 |
| --- | --- |
| 编号 | td/063 |
| 评审对象 | td/061-fire-simulation-forward-return-calibration、td/062-td060-implementation-review 的当前实施 |
| 评审日期 | 2026-06-20 |
| 结论 | **不通过：存在 1 项 P0、5 项 P1，不能归档至 `docs/`，也不能用于重新计算目标 P50 case。** |
| 评审原则 | 仅评审；未修改业务代码、迁移或测试代码。 |

## 1. 执行结论

实现已具备 profile 数据模型、迁移、相关性因子模型、真实购买力序列、分析页面和 TD 062 大部分向导修复。
但核心链路尚未闭合：通过主入口“新建计划向导”创建的计划不会保留 `blended_prior`，而是回退到
`historical_cagr`。因此目前重新运行用户关注的 90% 纳指 QDII / 10% 现金 case，仍可能沿用历史
16.9564% CAGR；TD 061 的前瞻校准不能作为实际结果的依据。

此外，现金收益和 FX 前瞻校准未接入引擎，用户无法编辑自定义 profile，相关性缺失会静默被当作 0，
以及 TD 062 要求的 guardrail 上下限关系仍未校验。这些不是展示层问题，会直接改变模拟输入、风险分布或
允许产生无效计划。

结论：**不得将 TD 061、TD 062 的实施说明归档到 `docs/`；必须完成本报告 P0/P1 后重新评审。**

## 2. 已确认完成项

| 范围 | 结果 | 证据 |
| --- | --- | --- |
| 假设集的版本化基础设施 | 已实现 | migrations `0016`、canonical JSON/hash、system profile、profile 生命周期 API |
| 前瞻收益的 log 空间收缩纯函数 | 已实现 | `assumptions.Profile.CalibrateForwardReturn` 和对应单测 |
| 多资产月度联合 Student-t 因子模型 | 主体已实现 | 月度收益快照、相关性收缩、PSD 投影、冻结 `FactorModel`、联合抽样 |
| 真实购买力结果 | 已实现 | 路径按本路径累积通胀折现，另表持久化真实分位序列，分析/路径页面可切换 |
| TD 062 R1（0% 地区持仓） | 已实现 | 前端以 `region_enabled` 统一筛选、删除不启用地区持仓，并覆盖国内/国外镜像测试 |
| TD 062 R2 的大部分数值范围与税基校验 | 已实现 | `validateParameters` 已统一进入创建、更新和模拟前校验 |

## 3. 阻断性问题

### R0：计划参数 API 丢弃收益假设字段，主向导实际回退为历史 CAGR

**严重性：P0 / 核心结果正确性。**

`repository.PlanParameters` 与 Web 类型都已有 TD 061 字段，但 JSON DTO `PlanParametersAPI` 没有
`return_assumption_mode`、`assumption_selection_mode`、profile id/version、scenario 或 custom JSON。
因此 Go 的 JSON 绑定忽略向导送来的字段，`ParametersFromAPI` 也不会回填它们；随后
`ParametersRepo.Upsert` 对空值调用 `applyAssumptionDefaults()`，写入 `historical_cagr`。

同一缺陷还会使 `GET /plans/:id/parameters` 不返回这些字段，`PUT` 保存任意参数时将已有选择清空并再次
回退为历史模式。前端参数页虽渲染了完整选择控件，但后端响应不包含字段，页面初始状态和保存结果均不可信。

涉及位置：

- `internal/service/parameters_api.go:11-40`：DTO 缺少全部收益假设字段；
- `internal/service/parameters_api.go:49-84`：双向转换未映射字段；
- `internal/repository/plan_parameters.go:179-188`：空值默认写为 `historical_cagr`；
- `web/app/plans/new/page.tsx:158-162`：前端发送 `blended_prior`，但服务端会丢弃；
- `web/app/plans/[id]/parameters/page.tsx:613-706`：UI 暴露的设置无法持久化。

#### 唯一修复方案

把 TD 061 的六个计划级字段完整加入 `PlanParametersAPI`，并在 `ParametersToAPI` 与
`ParametersFromAPI` 一一映射：

```text
return_assumption_mode
assumption_selection_mode
return_assumption_set_id
return_assumption_set_version
return_assumption_scenario
custom_return_assumptions_json
```

新增单一服务端校验器并由计划创建、向导创建、参数更新和模拟前准备共同调用：模式只能是
`historical_cagr | blended_prior | custom`；选择只能是 `follow_global | pinned_profile`；情景只能是三种
已定义值；pin 必须提供且只能引用 active profile；`custom` JSON 必须可解析。DTO 往返必须保持所有字段
不变。新建向导创建后必须写入 `blended_prior/follow_global/baseline`；migration 以前的既有计划仍保持
`historical_cagr`。

#### 验收逻辑

1. 用 `/plans/wizard` 创建计划并查询参数，返回并持久化
   `blended_prior`、`follow_global`、`baseline`，首次 run 的输入为 `engine_version=3.0.0` 且带
   `multivariate_student_t`（存在风险资产时）。
2. 对已 pin profile 的计划更新任意无关参数，六个字段值逐字不变；更新后 run 继续引用同一
   `id@version`。
3. API 分别提交未知 mode、未知 selection、未知 scenario、draft/superseded pin、非法 custom JSON，均返回
   `parameters_invalid`，且不改变原记录。
4. migration 前构造的计划保持 `historical_cagr`，其旧 run 可重放且不受 profile 影响。
5. 增加 API 端到端测试，禁止仅通过 repository 或前端 mock 覆盖此链路。

### R1：现金前瞻收益已校准但在两条引擎路径中均被跳过

**严重性：P1 / 模型输入错误。**

现金资产会成功命中 system profile 的 1.8% 先验并把值冻结到 `SnapshotAsset`，但引擎实际不应用该收益：
独立路径在 `applySlotReturn` 遇到 `isCash` 立即返回；联合路径在 `applySlotJointReturn` 也立即返回；
`buildFrozenFactorModel` 将现金排除。因此所有现金仓位（包括全现金组合）实际仍是 0% 收益，违背 TD 061
“现金不能继续隐式固定为 0”的定案。

涉及位置：

- `internal/simulation/path_loop.go:197-199`；
- `internal/simulation/path_loop.go:258-260`；
- `internal/service/factor_model_build.go:34-38`。

#### 唯一修复方案

保留现金为非随机、非 FX 因子，但在每个月所有风险资产收益处理后，对每个 `isCash` slot 应用其冻结的
确定性月收益：

```text
r_cash,m = exp(ln(1 + ForwardAnnualGeometricReturn) / 12) - 1
balance_next = balance_current × (1 + r_cash,m)
```

不得为现金抽取 Student-t 随机数、不得加入相关矩阵、不得附加 FX。压力测试中如有现金专属收益冲击，按现有
`AssetShock` 的复合语义作用在该确定性收益上。旧 `2.0.0` input 保留当前零收益分支；仅 `3.0.0` 前瞻输入
使用上述路径，确保旧 run 逐位可重放。

#### 验收逻辑

1. 3.0.0、无现金流、100% CNY cash、1.8% 年化、12 个月的期末值严格等于
   `initial × 1.018`（允许最小货币单位取整）。
2. 同一输入 120 个月严格等于 `initial × 1.018^10`；随机数种子、Student-t df 和相关矩阵不改变结果。
3. 混合组合中现金部分按该公式增长；其余风险资产仍保持联合抽样语义。
4. 2.0.0 历史快照仍为零现金收益，回放结果与改动前逐位一致。

### R2：FX 前瞻校准函数未进入生产路径，原币资产还会因币种键不匹配被阻断

**严重性：P1 / 跨币种结果正确性。**

已实现 `Profile.CalibrateFX`，但没有任何生产调用点。`enrichSnapshotAssetFX` 直接把市场历史
`ModeledAnnualReturn` 和历史波动率写入 `FXModeledReturn`/`FXAnnualVolatility`，完全忽略 profile、先验等效年数
及 `return_shift_log_fx`。这使跨币种资产的一部分漂移仍是无限期历史外推，且没有 FX 的前瞻审计字段。

同时，资产校准传入的是标的原币种（例如 USD），而 system profile 的海外权益 prior 只有 CNY 条目。故原币
USD 持仓会在资产校准阶段报 `assumption_unavailable`；CNY 计价 QDII 不受 FX 影响是正确的，但不能因此让原币资产
无法运行。

涉及位置：

- `internal/assumptions/calibrate.go:149-210`：`CalibrateFX` 仅存在于纯函数；
- `internal/service/simulation_snapshot_build.go:232-272`：直接使用历史 FX metrics；
- `internal/service/simulation_snapshot_build.go:118-120`：资产先验按原币查找；
- `internal/assumptions/system.go:39-56`：system profile 只提供 CNY 的资产 prior。

#### 唯一修复方案

明确并统一“资产本地回报 + FX 回报”的币种契约：资产 prior 的 `valuation_currency` 对原币资产使用标的原币，
FX prior 使用 `(from_currency, plan.base_currency)`；对于 CNY 计价 QDII，资产 prior 使用 CNY 且不创建 FX 因子。
补齐 system profile 支持的原币 asset-prior 覆盖范围，并以真实、具名审核的 CMA 来源录入。

在构建 `SnapshotAsset` 时将 `resolvedAssumption` 传入 FX enrich：`blended_prior` 和 `custom` 使用
`CalibrateFX`（custom 仅覆盖资产本地回报，不覆盖 FX）；`historical_cagr` 保持历史 FX 以兼容旧模式。把
FX 历史值、prior、权重、最终前瞻值、profile/version/scenario、波动率和 warning 冻结进 input snapshot，
并用最终前瞻值构建 FX factor。缺少原币 asset prior 或 FX prior 必须阻断前瞻模拟，不能退回历史值。

#### 验收逻辑

1. 原币 USD 股票 + CNY plan 在 `blended_prior` 下可建立输入；资产本地和 USD/CNY FX 均有 profile 审计字段，
   FX `μ` 等于 TD 061 的 log-space 公式。
2. 改变 profile 的 `return_shift_log_fx` 只改变 FX factor 漂移，不改变资产本地漂移；改变资产情景位移不隐式
   改变 FX。
3. CNY 计价 QDII 不创建 FX factor，且不会重复叠加 USD/CNY。
4. 删除适用资产 prior 或 FX prior 后，`blended_prior` 创建 run 返回可定位的 `assumption_unavailable`；
   `historical_cagr` 旧 run 仍可重放。
5. 集成测试覆盖 CNY QDII、原币 USD、以及缺失 FX mapping 三种组合。

### R3：全局 profile 不是完整、可编辑且实际生效的全局假设中心

**严重性：P1 / 实现缺失与治理失效。**

页面只有查看、复制、激活和设置默认的能力。复制操作立即原样保存 draft；没有编辑 profile 名称、先验、波动率
边界、情景、FX、相关性、来源、审核元数据的表单，也没有调用 validate endpoint 的交互。用户无法通过产品完成
TD 061 要求的“复制后编辑、校验、激活”。

同时 profile 中的 `StudentTDf` 没有进入模拟：input 使用 `params.StudentTDf`，计划参数页仍允许逐计划修改。
这与 TD 061“厚尾参数是全局 profile 唯一入口”冲突；系统还未将 tail 截断的配置和审计纳入 profile。

涉及位置：

- `web/app/assumptions/page.tsx:90-105, 325-464`：仅复制/展示，无编辑或 validate 流程；
- `internal/service/simulation_snapshot_build.go:309-322`：df 取自 plan 参数；
- `internal/simulation/student_t.go:7-10`：tail 截断仍为不可审计的代码常量。

#### 唯一修复方案

在 `/assumptions` 实现版本化 profile 编辑器：仅 draft 可编辑；复制后进入编辑态；表单完整覆盖 profile header、
三种情景、资产/FX prior、相关性、来源 URL、发布日期、审核人/审核时间、df 与截断边界。保存前调用
`validate` 并显示字段级错误、PSD 指标和显著修复告警；保存成功仅创建新 version，active/system 永不原地修改。

把 `StudentTDf`、收益截断上下限归属到 profile，冻结入 `InputSnapshot.FactorModel`（或等价的 immutable risk
model section），联合/独立抽样均只读取冻结值。删除新计划和参数页对这些全局值的可编辑入口；2.0.0 快照继续
读取旧参数和旧常量，以保持历史重放。

#### 验收逻辑

1. 系统 profile 只读；复制后可修改每一类字段、可预校验、保存为 draft、激活为新 active version；已有 run
   的 input hash 和结果不变。
2. 缺少来源、日期、审核元数据、必填 mapping 或 PSD 校验失败时，保存和激活均显示定位到字段的错误。
3. 修改 active profile 的 df/截断并运行新计划，input snapshot 与实际采样使用新值；已有 run 仍使用冻结旧值。
4. 计划设置不再能改变 df/截断；全局 profile 更新后 follow-global 新 run 使用新 version，pinned plan 保持原
   version。

### R4：相关性 prior 缺失被静默写为 0，profile 发布不验证完整性

**严重性：P1 / 风险模型正确性。**

当 `factorBuild.pairCorrelation` 查询不到 prior 时，代码直接将 `prior = 0`。`ValidateProfile` 只从已给出的
pair 构造矩阵，未检查因子对覆盖；`Profile.Validate` 也不拒绝重复 correlation pair。结果是一个不完整或错误的
profile 仍可激活，并在共同月份不足时以“零相关”计算，正是 TD 061 明确禁止的静默降级。

涉及位置：

- `internal/service/factor_model_build.go:118-126`；
- `internal/service/assumption_service.go:95-100, 188-215`；
- `internal/assumptions/profile.go:226-237`。

#### 唯一修复方案

建立 profile 的规范因子宇宙：所有可能随机的 asset prior cell 与 FX prior 都映射为 factor；确定性现金不进入
宇宙。保存和激活时必须验证每个不同因子对恰好有一个 correlation prior，pair 必须规范排序且不得重复；再对
完整矩阵执行 PSD 检查。运行时若输入的因子对没有 prior，直接返回 `assumption_unavailable`，绝不填 0 或退回
独立模型。PSD 小修复可以带 warning 并冻结，超过阈值时阻止激活。

#### 验收逻辑

1. 少任一相关性 pair、存在重复/反向重复 pair、或显著 PSD 修复的 draft 均无法保存或激活；错误包含 pair key。
2. 完整 profile 的每个风险因子对均在 input 的 `FactorAudit` 中具备 prior、共同月份数与 shrinkage 权重。
3. 人为构造缺失 prior 的运行输入会失败，不会生成 `rho=0` 的 run。
4. 同类同地区强制 `rho=1` 的现有规则与完整性校验兼容，并有覆盖测试。

### R5：TD 062 R2 仍漏掉 guardrail 下限不大于上限的交叉校验

**严重性：P1 / 无效计划可创建。**

当前校验只限制 `withdrawal_floor_ratio ∈ (0,1]` 与 `withdrawal_ceiling_ratio ∈ [1,2]`，没有验证
`floor <= ceiling`。例如 floor=100%、ceiling=100% 虽然数值上可接受，但 guardrail 已无可用区间；更关键的是
TD 062 的验收明确要求 `floor > ceiling` 被拒绝。若以后放宽 ceiling/floor 单项范围，当前实现会立即允许逻辑
相反的护栏。

涉及位置：

- `internal/service/validation.go:83-105`；
- `td/062-td060-implementation-review.md` 的 R2 验收逻辑。

#### 唯一修复方案

在 `validateWithdrawalParams` 中增加严格的跨字段规则：

```text
withdrawal_floor_ratio < withdrawal_ceiling_ratio
```

将其作为 `validateParameters` 的唯一服务端规则，并在两个前端参数表单使用同一约束做即时错误、禁用保存/创建。
补齐 API/服务/向导测试，不能仅依赖客户端。

#### 验收逻辑

1. `floor >= ceiling` 的 `/plans`、`/plans/wizard` 与参数 `PUT` 一律返回 `parameters_invalid`，不落任何部分数据。
2. 默认 `70% / 130%`、`100% / 101%` 均通过；`100% / 100%` 与任意逻辑反向值被拒绝。
3. 前端输入发生冲突时立即显示错误，创建/保存按钮不可用。

## 4. 重要但不阻断本次代码编译的缺口

| 编号 | 严重性 | 问题 | 固定处理与验收 |
| --- | --- | --- | --- |
| N1 | P2 | system profile 的 source URL 指向项目 TD，审核人为 `system-seed`，代码注释仍写“pending named reviewer sign-off”；这不满足 TD 061 的可审计 CMA 来源和具名审核。`Profile.Validate` 也只检查非空，未验证 URL/日期/审核人。 | 发布前以外部 CMA 原始来源、发布日期、适用市场和具名审核人创建新的 system profile version；后端校验 HTTPS URL、ISO 日期、审核人非空。验收时每个 prior 都能打开来源且 UI 显示审核信息。 |
| N2 | P2 | `pinned_profile` 的解析只读取存在的 profile，不检查其是否 active；用户可通过参数 API 让 draft/superseded profile 进入 run。 | 将 active 检查放在同一计划假设选择校验中；已有被冻结 run 不受影响。测试 draft/superseded pin 被拒绝、active pin 可运行。 |
| N3 | P2 | `buildFrozenFactorModel` 或 Cholesky 失败时 `buildInputSnapshotStruct` 静默保留 independent 2.0.0 路径。前瞻模式不能因构建异常悄悄降级。 | 将 factor model builder 改为返回带原因的 error，前瞻模式构建失败时阻断创建 run。测试异常矩阵/非法 factor 输入绝不产生 independent run。 |
| N4 | P2 | profile 输入校验未拒绝 NaN/Inf 的 scenario shift、volatility multiplier、correlation rho，且 FX prior 未检查 duplicate。 | 对所有数值调用有限值校验，FX/correlation 使用规范 key 去重；保存/激活都应用。测试 NaN/Inf、正反向重复、重复 FX 均被拒绝。 |

## 5. 回归验证记录

| 命令 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `cd web && npm run test:ci` | 通过：53 files / 325 tests |
| `cd web && npm run lint` | 通过：0 warning |
| `cd web && npm run build` | 通过：Next.js production build |
| `git diff --check` | 通过 |

这些检查证明当前代码可编译、类型和现有断言通过；不能覆盖 R0 的原因是现有测试没有断言“向导请求 →
PlanParametersAPI → 数据库 → GET/PUT → run input”的收益假设字段往返，也没有以非零现金收益、FX 情景位移、
profile 编辑和不完整相关性作为端到端测试。

## 6. 归档与提交决定

由于 R0-R5 均未满足，TD 061/062 不整理到 `docs/`。本报告是唯一新增评审产物；未修改业务实现。
当前工作区包含 TD 061/062 的完整待提交实现与本评审文档，应在完成 P0/P1 修复并复审通过后再做 docs 归档。
