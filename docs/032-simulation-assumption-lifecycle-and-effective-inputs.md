# 模拟假设生命周期与有效输入

- 状态：已实施
- 当前模拟引擎：`3.5.0`

## 1. 目的

本文定义模拟假设从全局 profile、计划选择、有效身份解析到运行快照冻结的完整契约，并说明资产地域和基金费用如何进入 FIRE 模拟。目标是让页面显示、配置保存、任务输入和历史回放使用同一组事实。

## 2. Profile 生命周期

模拟假设 profile 使用 `profile_id + version` 标识不可变内容，生命周期为：

```text
draft -> active -> superseded
```

- 新建或编辑产生 draft 版本，不原地修改 active 内容；
- 激活操作在一个事务内将目标版本设为 active，并 supersede 同 profile 的旧 active 版本；
- 全局默认若指向该 profile 的旧版本，激活时在同一事务内迁移到新版本；
- 显式固定在旧版本的计划不会被自动迁移，仍可按冻结身份回放；
- `system_cma_` 是系统保留命名空间，用户 profile 不得占用；
- 系统 profile 通过 canonical content hash 校验，已发布身份不得原地改变内容。

系统启动时会幂等发布当前系统 profile，并审计保留命名空间与历史系统身份。当前系统默认、用户选择的默认以及计划固定版本均以数据库中的有效身份为准，不从页面缓存推断。

## 3. 计划如何选择假设

计划参数支持两种选择模式：

| 模式 | 行为 |
| --- | --- |
| `follow_global` | 每次构造新运行时解析当时的全局默认 profile 和 scenario |
| `pinned_profile` | 固定使用计划保存的 `profile_id + version`，目标版本必须存在且不能是 draft；不可变的 superseded 版本仍可固定回放 |

参数 API 同时返回 `effective_assumption`。页面展示该服务端解析结果，不自行组合“默认 profile + 计划字段”。激活 profile、修改全局默认或修改计划固定版本后，相关查询必须失效并重新读取有效身份。

构造运行快照时，有效身份解析、profile 内容读取和输入哈希计算使用同一份已解析数据，避免默认版本在读取过程中变化造成输入身份与内容不一致。

## 4. 收益、费用与地域口径

### 4.1 收益假设

当前支持：

- `blended_prior`：历史几何收益向 profile 的长期先验收缩；
- `historical_cagr`：使用冻结历史收益口径；
- `custom`：使用计划级资产 override。

profile 提供收益、现金、FX、相关性、厚尾与情景参数。计划 override 只覆盖明确配置的资产字段；未覆盖部分继续使用有效 profile。具体前瞻计算见 [019-fire-simulation-forward-engine-and-plan-controls.md](./019-fire-simulation-forward-engine-and-plan-controls.md)。

### 4.2 基金费用

基金历史净值和收益视为已经扣除管理费、托管费等持续费用。`expense_ratio` 只作为资料库审计信息，不在 FIRE 快照或路径收益中再次扣减，避免重复收费。申购、赎回等一次性费率不并入长期年化收益假设；模拟中的交易成本继续使用计划配置的统一交易成本率。

### 4.3 资产地域

资产目录身份描述交易市场和数据来源，不强制决定 FIRE 持仓的 `region`。计划持仓的国内/国外分类以用户选择为准：

- QDII、全球互联网或同时持有境内外资产的基金不通过名称关键字自动改写地域；
- 不要求用户维护无法稳定取得的国家或地区暴露比例；
- 地域调整通过带 preview hash 的显式预览和应用流程完成，不在读取、同步或模拟时静默修改持仓；
- 修改地域后，计划配置版本和模拟输入哈希相应变化，旧结果进入 stale 状态。

## 5. 运行快照与历史回放

创建模拟任务前冻结：

- 有效 profile id、version、owner、content hash 和可用的 evidence hash；
- assumption mode、scenario 及计划 override；
- 每项资产的校准收益、波动率、地域、币种和目标权重；
- FX、现金收益、相关矩阵、Student-t 参数和尾部边界；
- 市场数据来源摘要、引擎版本与完整 input/config hash。

worker 只消费冻结快照，不在执行过程中重新读取全局默认、当前持仓分类或最新 profile。历史结果和代表路径按保存的引擎版本及快照重放；当前默认值变化不会改写既有结果。

## 6. 过期判定

计划当前有效输入重新计算出的 config hash 与 run 快照不一致时，run 标记为 stale。以下操作会使后续运行输入变化：

- `follow_global` 计划所跟随的全局默认身份变化；
- 计划切换选择模式、固定 profile 或 scenario；
- 资产 override、地域、权重、启用状态或 FIRE 参数变化；
- 用户显式改变其他进入计划 config hash 的模拟参数。

stale 只表示结果不再对应当前计划，不删除历史 run，也不使用当前配置重写历史快照。

## 7. API 与代码边界

- profile CRUD、校验和激活：`/api/v1/simulation-assumptions/profiles/*`；
- 全局偏好：`/api/v1/simulation-assumptions/preferences`；
- 计划参数响应中的 `effective_assumption` 是页面展示的唯一有效身份来源；
- profile 持久化与升级审计位于 `internal/repository/assumption_profile.go`；
- 有效身份和计划参数校验位于 `internal/service/validation.go`；
- 快照构造、费用口径和 provenance 位于 `internal/service/simulation_snapshot_build.go` 及模拟 service。

## 8. 验证

自动化必须覆盖：

1. draft 激活、旧版本 supersede 和同 profile 默认版本迁移在同一事务内完成；
2. 用户默认和计划显式 pin 不被系统 profile 升级覆盖；
3. 保留命名空间冲突、未知系统 content hash 和无效 pin 被稳定拒绝；
4. `follow_global` 与 `pinned_profile` 返回正确 `effective_assumption`；
5. 快照冻结 profile identity/content/evidence hash，用户 profile 不继承系统 evidence；
6. 修改资料库 `expense_ratio` 不改变模拟资产收益或导致二次扣费；
7. 用户选择的 QDII/混合地域保持不变，preview/apply 失败时整体回滚；
8. 配置变化使旧 run stale，历史路径仍按冻结版本重放；
9. `go test ./...`、Web test/lint/build 和 `make lint` 通过。
