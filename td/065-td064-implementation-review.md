# TD 064 实施评审

## 文档信息

| 项 | 内容 |
| --- | --- |
| 编号 | td/065 |
| 评审对象 | `td/064-td063-implementation-review.md` 的当前实施 |
| 评审日期 | 2026-06-20 |
| 结论 | **不通过：R6、R7、N6 的主修复已完成，但新增发现 3 项 P1 与 1 项 P2，不能归档到 `docs/`。** |
| 评审原则 | 仅评审；未修改业务代码、迁移或测试代码。 |

## 1. 结论

TD 064 的核心改造已经落地：

- `system_cma_v2@1` 作为新不可变 system profile 写入；旧 `system_cma_v1@1` 保留；
- 首次升级会把仍指向 v1 的全局默认迁移到 v2；
- profile 增加 CNY 基础资产与原币 FX coverage 门槛；
- 前瞻计划的 `student_t_df` 已被服务端固定为 legacy 只读字段，并从前瞻 config hash 移除；
- 相应 Go、前端、构建验证均通过。

但 v1 仍以 `active` 状态出现在全局默认 selector 中，而 `SetPreferences` 只检查 status，未检查当前 profile
是否满足 TD 064 的 publish/coverage/tail 有效性。用户可重新把旧 v1 选为 global default，绕过 v2 的完整
映射和 tail 审计；这会重新引入旧 profile 的 FX 覆盖与模型不一致问题。

同时，计划 API 允许任意 `base_currency`，甚至可以更新既有计划的基准币种；而当前全局 coverage、现金、FX prior
和 system profile 都固定为 CNY。系统会先成功保存 USD 等基准币种计划，随后在模拟快照阶段才因缺失 CNY→USD
FX/收益映射失败。最后，新的 CMA 来源只提供两个泛化入口 URL 与代码录入日期，不能审计每个系统数值如何从原始
资料转换得到，尚不能作为默认风险模型的正式来源。

结论：**不创建 docs 归档。**

## 2. TD 064 完成情况

| TD 064 项 | 结果 | 证据 |
| --- | --- | --- |
| R6：v1 不变、发布 v2、首次默认迁移 | 已完成 | 新 `system_cma_v2@1`、事务内 preference migration、legacy upgrade 测试 |
| R7：基础收益/FX coverage gate | 已完成 | `RequiredGlobalCoverage`、native prior→FX prior 校验、editor 必填项不可删除 |
| N5：外部 URL 形式 | 部分完成 | URL 已改为 Research Affiliates/BIS，但数值来源和换算过程仍不可审计（见 R10） |
| N6：前瞻 plan df 不可写/不致 stale | 已完成 | 更新保留 stored df，forward hash 排除 df，历史模式仍纳入 hash |
| 升级回归 | 基本完成 | 保存 v1 bytes、迁移 default、custom default 不改、fresh install v2 测试 |

## 3. 阻断性缺陷

### R8：不合格的 legacy v1 仍可被设为全局默认，绕过 profile 发布门槛

**严重性：P1 / 全局模型正确性。**

TD 064 保留旧 `system_cma_v1@1` 是正确的，但它仍是 `active`。`SetPreferences` 仅检查
`p.Status == active`，而未调用 `assertActivatable`/`Profile.Validate`。前端 `PreferencesCard` 也将所有 active
profile（包含 v1）列入 global default 下拉框。v1 没有 TD 063 的 `return_floor/return_ceil`，也没有 v2 的
USD/HKD 原币 prior、完整 FX coverage 和外部来源元数据；它不满足当前 publish 规则，却可以重新成为
follow-global 计划的默认输入。

这使 R6 的一次性迁移可被任何一次页面操作撤销，且将不完整 profile 再次暴露给所有新计划。

涉及位置：

- `internal/repository/assumption_profile.go:47-99`：保留 v1 后未标注其 default eligibility；
- `internal/service/assumption_service.go:SetPreferences`：只检查 active status；
- `web/app/assumptions/page.tsx:PreferencesCard`：筛选条件仅为 `status === active`。

#### 唯一修复方案

将“可作为全局 default”定义为独立的服务端资格：profile 必须为 active 且通过当前 `assertActivatable`（结构、
coverage、PSD、tail 全部合格）。`SetPreferences` 强制执行该资格校验；`ListProfiles` 返回
`eligible_for_global_default`，前端只将该字段为 true 的 profile 放入 default selector，并在 legacy v1 行显示
“仅历史兼容，不能作为全局默认”。旧 v1 记录、既有 run 与显式 pin 均不修改。

#### 验收逻辑

1. `PUT /simulation-assumptions/preferences` 指向 `system_cma_v1@1` 返回明确的
   `assumption_profile_not_eligible`；当前 preference 不变。
2. UI 不将 v1 提供给 global default selector，且列表说明其仅用于历史兼容。
3. v2 和满足 coverage 的 active user profile 可以设为 default；draft、superseded 或任意校验失败 profile
   均被拒绝。
4. 既有 run 与显式 pin v1 不被重写；其 input snapshot 仍可回放。

### R9：系统只支持 CNY 基准币种，但计划 API 允许创建或修改为任意币种

**严重性：P1 / 可创建但不可模拟的计划。**

`RequiredGlobalCoverage`、`BaseCoverageCurrency`、system cash prior 和 FX prior 全部固定为 CNY；这是当前系统
profile 的明确模型边界。但 `PlanService.Create` 与 wizard 只在空字符串时默认 CNY，`PlanService.Update` 也会
直接接受任意非空 `base_currency`。例如将一个计划改为 USD 后，CNY 标的需要 CNY→USD FX prior；v2 并没有该 prior，
计划保存成功而创建模拟失败。

这与 TD 064 R7 的目标相反：全局配置应在保存时阻止不可用模型，而不是让用户在模拟时才得到
`assumption_unavailable`。

涉及位置：

- `internal/assumptions/profile.go:143-178`：coverage 固定 `BaseCoverageCurrency = "CNY"`；
- `internal/assumptions/system.go:66-99`：system prior/FX 对仅覆盖 CNY base；
- `internal/service/plan_service.go:136-145, 205-225`：不校验 base currency；
- `internal/service/plan_wizard.go:39-42`：只为空值补 CNY。

#### 唯一修复方案

在当前 CNY-only system profile 发布范围内，服务端将 `Plan.BaseCurrency` 固定校验为 `CNY`：创建、wizard 创建、
计划 metadata 更新都必须拒绝非 CNY 值；前端移除“可在各计划中调整”的表述和任何可编辑入口。未来若要支持另一
基准币种，必须先发布一个包含该 base 全部 return/FX/cash coverage 的独立 system profile，再单独扩展此约束，
不能让任意币种直接进入现有模型。

#### 验收逻辑

1. `POST /plans`、`POST /plans/wizard` 与计划 metadata `PUT` 提交非 CNY 均返回 `validation_failed`，且不写入
   或部分更新记录。
2. CNY 创建、更新、旧计划与现有 run 行为不变。
3. 不存在“计划保存成功、随后因 CNY→USD FX mapping 缺失而模拟失败”的路径。
4. API/前端测试覆盖 create、wizard、update 三个入口。

### R10：v2 的 CMA 来源不能复现各 prior 的数值与转换过程

**严重性：P1 / 默认资本市场假设审计不足。**

v2 用两个泛化入口 URL 覆盖所有 return/FX prior，`published_at` 统一写为代码评审日期 `2026-06-20`。这没有记录
源资料的实际发布日期、页面/表格/数据版本、国内/海外权益和债券的具体取值、费用扣除、从 real/算术收益到
CNY nominal geometric return 的换算过程，或 USD/CNY、HKD/CNY 双边 FX 的数据截取与计算。BIS 的 effective
exchange-rate 统计入口也不是对 USD/CNY/HKD/CNY 双边假设的充分可追溯记录。

因此 source URL 形式上为外部 HTTPS，但默认 `6.0% / 6.5% / 3.0% / 1.8% / 0%` 仍无法由 profile 本身或
versioned artifact 复算/核验，不能满足 TD 061 的“每条先验具名、资料可审计、费用后基准币种名义几何收益”要求。

涉及位置：

- `internal/assumptions/system.go:22-42, 89-118`。

#### 唯一修复方案

为 `system_cma_v2@1` 的每个 return/FX prior 建立并提交一个不可变的 CMA evidence artifact（CSV/JSON 或
Markdown 数据表），每行包含：具体原始 URL、发布/获取日期、原始字段和值、适用市场、费用处理、通胀/币种/
算术到几何的换算公式和输入、最终 prior、审核人。profile 的 `source_url` 指向该行对应的原始资料，
`published_at` 使用原始资料日期；`source_note` 引用 evidence artifact 的版本/hash。若当前数值无法提供上述
证据，则不得将 v2 标为发布 default，应发布新的有证据 profile 后再切换默认。

#### 验收逻辑

1. 每个 v2 prior 都能从 evidence artifact 追溯到一个具体原始资料与实际发布日期，不能只共享网站首页。
2. 独立按 artifact 的公式和输入复算后，最终 prior 与 canonical JSON 的数值在约定精度内一致。
3. USD/CNY 与 HKD/CNY 使用明确的双边数据/代理及理由；现金收益有独立来源。
4. evidence artifact 与 profile content hash 同版本保存；任一数据或换算变化必须创建新的 system profile identity/version。

## 4. 非阻断问题

### N7：升级回归使用了简化的“legacy v1”样本，未覆盖真实已发布 canonical JSON

**严重性：P2 / 回归防护不足。**

`legacyV1Profile()` 的波动率边界和 correlation priors 仅是简化构造，并不等于 TD 061/062 实际发布的完整
`system_cma_v1@1` canonical JSON。当前测试能证明“任意旧行不会被覆盖”，但不能证明真实旧 profile 的解析、
preference 升级和后续 CNY 历史兼容路径。

#### 唯一修复方案

从 `7cbb4c7` 之前的已发布 migration/seed 输出生成精确、静态的 v1 canonical JSON fixture，并在升级测试中
直接插入该 fixture。断言升级前后 bytes/hash 完全一致，v2 插入与 preference 迁移正确，且使用该 fixture 的
历史 CNY run 输入可以加载和重放。

#### 验收逻辑

1. fixture 的 content hash 与旧发行版实际 hash 一致。
2. 升级后 fixture bytes/hash 不变，v2/default 行为符合 R6。
3. 使用 fixture 生成的旧 run 重放结果与升级前一致。

## 5. 验证记录

| 命令 | 结果 |
| --- | --- |
| `go test ./...` | 通过 |
| `cd web && npm run test:ci` | 通过：53 files / 326 tests |
| `cd web && npm run lint` | 通过：0 warning |
| `cd web && npm run build` | 通过：Next.js production build |

通过的测试证明实现可编译且覆盖了 v2 插入、coverage 与 df 语义；它们没有覆盖 R8 的 legacy default
eligibility、R9 的非 CNY plan 保存阻断、R10 的数值证据链，也未使用真实发布 v1 fixture。

## 6. 归档决定

R8–R10 修复并通过验收前，TD 064 不可视为完整实施，不创建 `docs/` 归档。本报告为唯一新增评审产物，未修改
业务代码。
