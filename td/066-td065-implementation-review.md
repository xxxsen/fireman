# TD 065 实施评审：全局默认资格、CNY 边界与 CMA 证据

- 评审日期：2026-06-20
- 对照范围：`td/065` 的 R8、R9、R10、N7
- 评审方式：代码静态审查、历史升级夹具审查、后端全量测试、前端测试/Lint/生产构建。
- 结论：**不通过**。R8 的 v1 默认资格漏洞、R9 的 CNY 基准币种边界和 N7 的真实历史夹具已实现；R10 已新增证据 artifact，但收益与 FX 计算公式错误，且仍以已发布的 `system_cma_v2@1` 身份原地改变模型内容。这会使历史数据库与新建数据库在相同 identity 下得到不同模型，不能接受。

## 已完成且验收通过

### R8：不符合现行规则的历史 v1 profile 不可作为全局默认

`ListProfiles` 已为每个 profile 计算 `eligible_for_global_default`；`SetPreferences` 也会复用 active + `assertActivatable` 校验。UI 仅展示合格候选项，并明确标注活跃但不合格的历史 profile。

真实 `system_cma_v1@1` canonical JSON 已作为 fixture 固化，升级测试不再用运行时构造的合成 profile 覆盖该分支。

### R9：计划模拟基准币种统一为 CNY

计划创建、更新及向导创建均经 `validateBaseCurrency` 校验，只接受 `CNY`。参数页测试覆盖非 CNY 被拒绝的场景。该限制与当前仅维护 CNY 系统假设、CNY 覆盖规则的产品边界一致。

### N7：历史升级 fixture

`internal/repository/testdata/system_cma_v1_canonical.json` 是对应已发布 v1 canonical JSON 的字节级 fixture，测试以 SHA-256 固定其内容后执行升级，不再以当前构造逻辑替代历史事实。

## 阻断问题

### R11（P1）：几何收益率与通胀/费用被直接相加，计算公式不成立

**证据**

`internal/assumptions/cma_evidence.go` 的 `FinalGeometricNominal` 计算为：

```
r_nominal = round4(r_real + inflation - fee_drag)
```

artifact 同样将该式声明为 `nominal_geometric`。但输入字段被明确命名为年化 `real_geometric_return`，其与通胀、按比例收取的年费必须按每年复利转换；直接相加仅是一阶近似，不是几何收益率。例如 `4.00%` 实际几何收益、`2.00%` 通胀、无费用，现实现给出 `6.00%`，正确结果为 `6.08%`。

FX evidence 也以 `base_inflation - quote_inflation` 表达相对购买力平价。对于非零且不相等的通胀率，几何年化变化应使用通胀比率，不能使用差值近似。

**影响**

系统 profile 的所有 nominal return prior 及未来非相等通胀的 FX prior 都会产生系统性偏差；该偏差会直接进入每条路径的年收益率、P50 与尾部结果。即使当前 CNY/USD/HKD 通胀输入恰好相同而使 FX 样例为零，公式在模型层仍是错误的。

**唯一修复方案**

发布新的、不可变的系统 profile `system_cma_v3@1`，在其 evidence builder 中统一采用下列公式，并在 artifact 的 `calculation_convention` 和每个 input 字段中明确其含义：

```
r_nominal_after_fee = (1 + r_real_geometric) * (1 + pi) * (1 - annual_fee_rate) - 1
fx_change_to_base = (1 + pi_base) / (1 + pi_quote) - 1
```

其中 `annual_fee_rate` 是按期末资产比例收取的年费率；若某来源给出的已经是费后名义收益，则该条输入必须显式标记为 `nominal_after_fee`，不得再次转换或扣费。移除含混的 `fee_drag` 命名。使用精确值计算，仅在写入 canonical profile 前按既有产品规则 round4。

**验收逻辑**

1. 单元测试：`r_real=0.04, pi=0.02, fee=0` 的结果为 `0.0608`；`fee=0.002` 的结果为 `0.0586784`（canonical 值为该结果按规则 round4 后的值）。
2. 单元测试：`pi_base=0.03, pi_quote=0.01` 的 FX 结果为 `(1.03/1.01)-1`，并覆盖负向通胀差。
3. `system_cma_v3@1` 的每一个 return/FX prior 均从同一 artifact 按上述公式生成，测试不得重新调用生产函数来证明自身正确，必须使用独立期望值。
4. 重新运行固定 seed 的 P50 回归场景；其结果仅能由新公式和新 profile identity 解释，运行元数据必须记录 `system_cma_v3@1` 与证据 hash。

### R12（P1）：`system_cma_v2@1` 被原地修改，历史数据库不会获得 evidence 且仍可被设为默认

**证据**

TD 064 已发布 `system_cma_v2@1`。本次将 `SystemDefaultProfile()` 改为从 `cma_evidence_v2.json` 动态生成 v2 内容，并将 artifact hash 写入 `source_note`，但 profile identity 仍是 `system_cma_v2@1`。`EnsureSystemDefault` 仅在该 `(id, version)` 缺失时插入，因此已有 v2 的数据库保留旧 canonical JSON；新数据库得到新 canonical JSON。两个数据库随后以同一 `id@version` 运行，实际模型却不同。

此外 R8 的资格判断只要求 active + 可激活。旧 v2 在结构上仍可激活，因此历史数据库的 `system_cma_v2@1` 仍可被 UI 选中并被 API 设置为全局默认。证据 hash 也由当前 artifact 动态计算，测试比较的是两个运行时计算值；它不能阻止以后再次无意修改 v2 内容。

**影响**

同一个计划设置、同一个 seed 与同一个 profile identity 可能因数据库初始化时间不同产生不同模拟结果，审计、回放与升级确定性失效。全局默认也可能继续把新计划落在不含证据且公式旧的 v2 上。

**唯一修复方案**

冻结已发布的 `system_cma_v1@1` 和 `system_cma_v2@1` canonical 内容，绝不再修改。以 R11 的正确公式和完整证据发布 `system_cma_v3@1`，并在代码中维护不可变 identity 注册表：每个系统 profile identity 对应固定 canonical SHA-256、固定 evidence SHA-256 及其前驱 identity。

`EnsureSystemDefault` 只插入当前 v3；当全局默认为空或精确等于注册表中 v3 的直接前驱系统 identity 时，在同一事务中迁移到 v3。用户自定义 profile 和非直接前驱的历史系统 profile 一律不自动替换。全局默认资格校验还必须要求系统拥有的 profile identity 等于当前可默认的系统 identity；因此 v1/v2 只可用于历史回放或显式 pin，不能重新成为全局默认。

**验收逻辑**

1. 以 TD 064 发布时的实际 `system_cma_v2@1` canonical fixture 初始化数据库并升级：v2 的 canonical bytes/hash 保持不变，v3 被插入，默认从 v2 迁移到 v3。
2. 全新数据库和上述升级数据库中，v3 的 canonical hash 与 evidence hash 完全一致；v1/v2 均不出现在 `eligible_for_global_default=true` 的列表中，尝试设置它们返回 `assumption_profile_not_eligible`。
3. 修改 v3 evidence artifact 任一字节但不新增 identity/更新注册表时，CI 必须失败；新增内容必须以新的系统 profile identity 发布并补充前驱迁移测试。
4. 用户自定义默认和被显式 pin 的历史 profile 不被升级逻辑覆盖；所有迁移、profile identity、canonical hash、evidence hash 均写入运行 provenance。

## 验证记录

- `go test ./...`：通过。
- `web: npm run test:ci`：53 个文件、326 个测试通过。
- `web: npm run lint`：通过。
- `web: npm run build`：通过。
- `git diff --check`：通过。

上述自动化验证覆盖了现有实现的一致性，但没有覆盖 R11 的独立数学正确性，也没有覆盖 R12 的已发布 v2 数据库升级语义，因此不能抵消两个 P1 问题。
