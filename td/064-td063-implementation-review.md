# TD 063 实施评审

## 文档信息

| 项 | 内容 |
| --- | --- |
| 编号 | td/064 |
| 评审对象 | `td/063-td061-td062-implementation-review.md` 的当前实施 |
| 评审日期 | 2026-06-20 |
| 结论 | **不通过：TD 063 的主要新库路径已完成，但仍有 2 项 P1 与 2 项 P2，不能归档到 `docs/`。** |
| 评审原则 | 仅评审；未修改业务代码、迁移或测试代码。 |

## 1. 结论

TD 063 的 R0–R5 在“新建空数据库”路径上已基本完成：收益假设 DTO 已双向传递，现金收益、FX 前瞻校准、
冻结的 tail 参数、相关性缺失阻断、guardrail 上下限校验以及 profile 编辑器均已接入，并有针对性测试。

但系统 default profile 的身份仍是已发布的 `system_cma_v1@1`。`EnsureSystemDefault` 对同一 id/version
只检查存在性，不更新 canonical JSON；因此已经运行过 TD 061/062 的数据库会永远保留旧 v1 内容，得不到
TD 063 新增的 USD/HKD 本币 prior、FX prior、tail 字段、完整相关性集合和审核元数据。实际升级用户与新建测试库
会得到两套不同的模型，这是结果正确性与可复现性问题。

同时，自定义 profile 没有最小收益/FX 覆盖约束，可以删除所有资产 prior 和相关性后保存、激活并设为全局默认；
之后任意正常的 `blended_prior` run 才在创建快照时失败。全局 profile 不能允许进入这种“已激活但不可用于
任何计划”的状态。

因此本轮不创建 `docs/` 归档。修复 P1 后需要以“已有 TD 061/062 数据库升级”作为必测场景重新评审。

## 2. TD 063 完成情况

| TD 063 项 | 结果 | 评审证据 |
| --- | --- | --- |
| R0：计划参数 API 往返与 pin 校验 | 已完成 | `PlanParametersAPI` 和双向转换已包含六个字段；未知 enum、draft pin、非法 JSON 已有 API 测试 |
| R1：现金前瞻收益 | 已完成 | 3.0.0 input 设定 `DeterministicCashReturn`，按冻结月度 drift 增长；2.0.0 保持零收益 |
| R2：FX 前瞻校准和 audit | 已完成（仅新 default profile） | `applyFXCalibration` 已接入快照构建，USD/HKD prior 与 FX audit 字段已加入 |
| R3：profile 编辑与全局 tail 参数 | 主流程完成 | Draft editor、预校验、版本化保存、profile df/截断冻结、计划页不再展示 df 输入 |
| R4：相关性完整性与不降级 | 已完成 | profile factor universe、pair 完整性/重复性校验，factor 构建缺 prior 返回 error |
| R5：guardrail 下限/上限 | 已完成 | 后端统一校验 + 向导/参数页即时阻断 |
| N2：pin 仅引用 active profile | 已完成 | 创建、向导、更新与 resolve 均校验 active 状态 |
| N3：前瞻因子构建失败不降级 | 已完成 | `buildFrozenFactorModel` 返回 error，前瞻 input 构建直接失败 |
| N4：有限值、FX/相关性去重 | 已完成 | profile 验证已覆盖 NaN/Inf、反向重复和 FX duplicate |
| N1：具名、可核查的 CMA 来源 | 未完成 | 系统 source 仍指向项目 TD，且元数据明确写有“Pending replacement” |

## 3. 阻断性缺陷

### R6：已存在数据库不会升级到 TD 063 的系统 profile 内容

**严重性：P1 / 升级兼容与模拟正确性。**

本次在 `SystemDefaultProfile()` 中直接改变了 `system_cma_v1@1` 的含义：增加 return truncation、USD/HKD
资产与 FX prior、六因子完整相关性以及审核元数据。但 profile 的不可变性正是 TD 061 的基础；现有数据库已经
保存过旧 `system_cma_v1@1` 的 canonical JSON。`EnsureSystemDefault` 检查 `(id, version)` 存在后立即返回，
不会补写新字段，也没有 migration 或偏好迁移。

后果：

1. 新装数据库得到新的 USD/HKD 校准能力；旧数据库继续使用旧 v1，在原币 USD/HKD 资产上仍可能
   `assumption_unavailable`。
2. 旧 canonical JSON 解码后 `return_floor/return_ceil` 为 0；运行时虽然会回退到硬编码默认值，但不具备
   TD 063 要求的 profile 版本化/审计语义。
3. 同样标为 `system_cma_v1@1` 的输入在不同安装历史下模型内容不同，违背 `id + version` 不可变与 run
   可解释性要求。

涉及位置：

- `internal/assumptions/system.go:3-7, 33-74`：仍以 `system_cma_v1@1` 发布已变化的内容；
- `internal/repository/assumption_profile.go:49-76`：存在同 id/version 即不更新；
- 当前 migrations 中没有 system profile 的版本迁移或 global preference 迁移。

#### 唯一修复方案

保留已发布的 `system_cma_v1@1` 完全不动，新增独立的不可变系统 profile `system_cma_v2@1`，其中包含 TD 063
完整字段、FX 映射和最终审核元数据。扩展 `EnsureSystemDefault` 为单一升级事务：

1. 确保插入 `system_cma_v2@1`；不得 update/delete v1；
2. 仅当 `simulation_assumption_preferences` 当前正指向 `system_cma_v1@1`（或 preference 缺失）时，原子地
   切换为 `system_cma_v2@1/baseline`；用户已选择的自定义 profile 与显式 pin 均不改动；
3. `GetPreferences` 的无记录 fallback 改为 v2；
4. 增加一次性升级测试：先按旧 v1 JSON 建库，再初始化新代码，断言 v1 canonical bytes 不变、v2 已插入、
   默认偏好正确切换，且旧 run 仍按冻结 input 回放。

#### 验收逻辑

1. 已有 v1 数据库升级后存在 `system_cma_v1@1` 与 `system_cma_v2@1` 两条记录；v1 的 canonical JSON/content
   hash 不变。
2. 未自定义全局默认的用户自动解析到 v2；已选自定义默认或 pinned v1 的计划不被改写。
3. 升级后的 follow-global CNY plan 与原币 USD/HKD plan 均能生成 3.0.0 前瞻 input，且 input 指向 v2。
4. 旧 run、旧 v1 pin 的既有冻结结果可重放；系统不会对 v1 作 in-place update。
5. 新装与升级数据库在同一 v2 profile、同一 seed、同一市场快照下生成一致 input hash。

### R7：profile 可在没有任何基础收益/FX 映射时被保存并激活

**严重性：P1 / 全局配置有效性。**

`Profile.Validate()` 会逐项校验已经存在的 `ReturnPriors`/`FXPriors`，但不会要求它们非空或覆盖系统支持的
基础资产映射。`FactorUniverse()` 在空列表时返回空集合，`validateCorrelationComplete()` 也会通过。因此用户可在
editor 中删除所有 return/FX/correlation rows，填写 header/scenario 后保存、激活并设为 global default；直到创建
模拟时 `calibrateAsset` 才报 `assumption_unavailable`。

这违反 TD 061/063 所要求的“缺少必填资产类别条目在保存与激活时返回可定位错误”，也让全局设置可以进入一个
不可用状态。

涉及位置：

- `internal/assumptions/profile.go:145-158`：验证链路没有 coverage 步骤；
- `internal/assumptions/profile.go:217-265`：空 prior slice 直接通过；
- `internal/assumptions/profile.go:291-334`：空 factor universe/相关性也直接通过；
- `web/app/assumptions/page.tsx` 的 editor 允许删除所有条目。

#### 唯一修复方案

在 `assumptions` 包定义单一的 `RequiredGlobalCoverage`，作为 active/global profile 的发布门槛：当前产品支持的
基础 CNY 映射必须包含 `equity/domestic`、`equity/foreign`、`bond/domestic`、`bond/foreign`、`cash/domestic`；
每个配置的原币 foreign prior（例如 USD、HKD）必须同时具有对应 `(from, CNY)` FX prior。将 coverage 检查置于
`Profile.Validate()` 的相关性检查之前，并返回缺失的 canonical key。profile editor 对必填行不提供删除操作，
新增币种时引导同时补齐 asset、FX 与相关性 pair。

#### 验收逻辑

1. 空 profile、缺少任意五个 CNY 基础条目、仅添加 USD 本币条目却不添加 USD/CNY FX prior，均无法保存或激活；
   错误包含缺失 mapping key。
2. system v2 和复制得到的默认 draft 通过 coverage 校验。
3. 激活的 follow-global profile 对当前支持的 CNY、USD、HKD 标的均能进入 calibration；缺映射错误只允许出现在
   用户新引入的未受支持币种，并能回到全局编辑器补齐。
4. editor 中必填条目显示“必填”，不可删除；添加新原币 prior 时缺 FX/correlation 的校验提示可定位。

## 4. 非阻断缺口

### N5：系统 CMA 来源仍不是可核查的外部来源

**严重性：P2 / 审计治理。**

系统条目仍使用仓库内 TD URL 作为 `source_url`，`SystemProfileSourceNote` 明确写着“Pending replacement by an
externally sourced CMA pack”。这只是格式校验通过，不是 TD 063 N1 所要求的具名、可打开的 CMA 原始来源。

#### 唯一修复方案

在 R6 的 `system_cma_v2@1` 中录入每类资产与 FX 先验实际使用的公开 CMA 原始页面/报告链接、发布日期、
适用市场、费用与几何收益换算说明；由真实责任人填写审核信息。删除“pending replacement”标记，且系统 profile
的所有 `source_url` 不得指向本项目文档。

#### 验收逻辑

1. 每个 v2 return/FX prior 的 URL 指向外部原始 CMA 资料，页面可打开且与资产类别/币种相符。
2. profile 列表与详情显示具名审核人、审核日期和 source note；审核资料可追溯到每一个 prior。
3. 审核来源变化时只能创建新的 system profile identity/version，既有 run 不变。

### N6：计划级 `student_t_df` 仍可由 API 修改并参与 config hash

**严重性：P2 / 配置语义一致性。**

UI 已改为只读说明，但 `PlanParametersAPI` 仍接受 `student_t_df`，`validateParameters` 仍校验并持久化它，
`ConfigHashService` 仍把它纳入 hash。对前瞻 3.0.0 run，构建 input 后该值会被 profile df 覆盖；这意味着 API
调用方可以改变一个不影响前瞻结果的计划字段，却导致 config version/hash 改变并把既有 run 标记为 stale。

#### 唯一修复方案

将 `student_t_df` 定义为仅供 2.x 旧快照回放的 legacy 字段：新计划和更新 API 不再接受它作为可写参数；
`UpdateParameters` 在持久化前保留数据库原值，且对采用 `blended_prior/custom` 的配置不将该 legacy 值纳入
config hash。3.0.0 的 df 和截断只来自冻结 profile；2.x 已完成 run 继续从 input snapshot 读取旧值。

#### 验收逻辑

1. 修改前瞻计划请求中的 `student_t_df` 不改变保存值、config hash 或 run stale 状态；返回明确的只读字段说明。
2. 改变 active profile df 后，新 3.0.0 input 与 hash 改变；旧 input/replay 保持不变。
3. 2.x 历史 run 可继续使用其冻结 df 回放。

## 5. 验证记录

| 命令 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `cd web && npm run test:ci` | 通过：53 files / 326 tests |
| `cd web && npm run lint` | 通过：0 warning |
| `cd web && npm run build` | 通过：Next.js production build |

现有测试主要覆盖新建测试库与纯函数；没有构造“已有 `system_cma_v1@1` canonical JSON 的数据库升级”场景，
也没有断言 active profile 的最小 coverage。这正是 R6/R7 未被发现的原因。

## 6. 归档决定

R6 与 R7 修复并通过升级回归前，TD 063 不可视为完整实施，不创建 `docs/` 归档。本报告为唯一新增评审产物，
未修改业务代码。
