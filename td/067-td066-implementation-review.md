# TD 066 实施评审：几何公式、不可变 identity 与升级迁移

- 评审日期：2026-06-20
- 对照范围：`td/066-td065-implementation-review.md` 的 R11、R12
- 评审方式：静态代码审查、发布历史与升级 fixture 审查、后端全量及固定 seed 回归、前端测试/Lint/生产构建。
- 结论：**不通过**。R11 的复利公式与 R12 的 v2→v3 常规升级路径已经正确实现，但系统 profile identity 没有被数据库中的同名用户 profile 隔离。该缺陷可使用户 profile 被当成 `system_cma_v3@1`，并写入错误的 CMA evidence hash，破坏模型与审计的可信边界。另有两个 P2 完整性缺口。

## 已完成且验收通过

### R11：几何收益与 FX 使用复利公式

`system_cma_v3@1` 已由 `cma_evidence_v3.json` 生成，收益与 FX 分别采用：

```
r_nominal_after_fee = (1 + r_real_geometric) * (1 + pi) * (1 - annual_fee_rate) - 1
fx_change_to_base = (1 + pi_base) / (1 + pi_quote) - 1
```

实现仅在写入 canonical profile 时 round4。单元测试覆盖 `4%` 实际收益、`2%` 通胀的 `6.08%` 结果、费用扣除、名义费后值直通、正/负 FX 通胀差，并以独立期望值校验 v3 的 return/FX priors。

### R12：v3 发布、v1/v2 默认资格与标准升级

- `system_cma_v1@1`、TD 064 原始 `system_cma_v2@1` fixture 均已按字节 SHA-256 固定。
- 新 profile 为 `system_cma_v3@1`；registry 锁定 v3 canonical/evidence hash，修改 artifact 而未发布新 identity 会导致 CI 失败。
- 默认仅在当前值为直接前驱 v2 时迁移到 v3；v1 与用户自定义默认不被改写。
- v1/v2 都不能重新选为全局默认；v3 可以。
- 新增 snapshot 中保存 profile id、version、canonical hash、evidence hash。

## 阻断问题

### R13（P1）：系统 identity 可被同名用户 profile 劫持，且会写入伪造的 v3 evidence provenance

**证据**

1. `Profile.Validate` 只要求 `id` 非空；`SaveDraft` 仅拒绝 `owner_scope=system`，并未拒绝用户创建 `system_cma_v3@1` 或任一 `system_cma_*` id。
2. `simulation_assumption_profiles` 的主键是 `(id, version)`，不包含 `owner_scope`。
3. `EnsureSystemDefault` 仅以 `SELECT COUNT(1) WHERE id=? AND version=?` 判断 v3 是否已存在。若旧库已有用户 `system_cma_v3@1`，它直接返回，不验证 `owner_scope`、canonical JSON 或 content hash，也不会插入真正的系统 v3。
4. 该用户 profile 在 `isEligibleForGlobalDefault` 中不会触发“system 必须是 current identity”的分支，因此可成为全局默认；`buildInputSnapshotStruct` 又仅按 `(id, version)` 调用 `LookupSystemIdentity`，会把 registry 中的 v3 evidence hash 写入这个用户 profile 的 run snapshot。

**影响**

系统会以用户自定义的收益、波动率与相关性运行模拟，却将运行 provenance 标记为 `system_cma_v3@1` 及官方 CMA evidence。这既可能产生错误的 P50/风险结论，也使审计记录失真；同名记录还会阻止真正 v3 的发布。

**唯一修复方案**

建立不可重用的系统 id 命名空间并在启动升级中校验 registry：

1. 将 `system_cma_` 定义为保留前缀。`SaveDraft` 和 profile validate API 对任何 `owner_scope=user` 且使用该前缀的 id 返回 `assumption_profile_reserved_id`；前端新建 user profile 的 id 由后端生成 `user_<uuid>`，不再信任客户端给出的 id。
2. `EnsureSystemDefault` 在单一事务中读取当前 registry identity 的完整行。存在时必须同时满足 `owner_scope=system`、数据库 `content_hash` 与 registry canonical hash 一致、canonical JSON 重新计算 hash 也一致；不满足则返回 `system_profile_identity_conflict`，不得继续创建模拟或写入默认。
3. 增加一次性数据修复：对于历史上 `owner_scope=user` 且 id 以 `system_cma_` 开头的 profile，复制为 `user_legacy_<old_canonical_sha256 前 16 位>`，在同一事务中更新 `plan_parameters` 的 pinned 引用与 global preference 后删除冲突行，再发布 registry 中真正的 system profile。既有 run 的 input snapshot 是冻结值，不改写。对于 `owner_scope=system` 但 hash 不匹配的行，一律以 `system_profile_identity_conflict` 阻断，由发布数据修复脚本显式处理，禁止覆盖或静默接受。
4. `buildInputSnapshotStruct` 只有在 resolved profile 的 `owner_scope=system` 且 content hash 与 registry entry 一致时，才写入 `AssumptionEvidenceHash`。

**验收逻辑**

1. API 创建 `owner_scope=user, id=system_cma_v3` 失败，错误码为 `assumption_profile_reserved_id`；正常 user profile 获得 `user_` id。
2. 构造旧库中用户 `system_cma_v3@1`：升级后用户 profile 已按上述确定性 id 迁移，计划 pin/默认仍解析到该用户 profile，真正 system v3 存在且 content/evidence hash 与 registry 一致。
3. 构造 `owner_scope=system` 但 canonical/content hash 不匹配的 v3：启动/读取返回 `system_profile_identity_conflict`，数据库内容不被覆盖，不能创建 run。
4. 对用户 profile（包括历史冲突迁移后的 profile）创建 run：snapshot 的 `assumption_evidence_hash` 为空；对已验证 system v3 创建 run：四个 provenance 字段均与 registry 精确相等。

## 非阻断但必须补齐

### R14（P2）：未覆盖 TD 065 已部署期间写入的 v2 变体

本次 fixture 只覆盖 TD 064 发布的 v2 canonical hash `3a154...`。但仓库历史的 `9700d69`（TD 065 实施）曾以同一 `system_cma_v2@1` identity 由 `cma_evidence_v2.json` 构建 profile；其 return prior 的 `published_at`、canonical JSON 与 TD 064 fixture 不同。凡在该版本首次初始化的数据库都会保存这一 v2 变体。

当前升级虽会按 id/version 把其默认迁到 v3，但没有 fixture、没有变体 registry，也不会为 explicit pin 的该变体保存它实际对应的 v2 evidence hash。将它当成 TD 064 的单一 canonical v2 会使历史 provenance 不完整。

**唯一修复方案**

增加只读的 `HistoricalSystemProfileVariant` registry，以 `(id, version, content_hash)` 为 key，显式登记 TD 064 v2 与 TD 065 v2 两个已发布内容及各自 evidence hash；它只用于历史 pin/replay 与 provenance，不能成为全局默认。补充 TD 065 实际 canonical fixture。升级时只接受这两个已知 v2 hash 作为历史 system v2，迁移其 global default 到 v3；其他 system v2 hash 返回 `system_profile_identity_conflict`，不猜测其含义。

**验收逻辑**

1. 分别以 TD 064 与 TD 065 的实际 v2 fixture 初始化数据库；升级后原 canonical bytes/hash 均不变、默认均迁移 v3。
2. 两个旧 v2 profile 均不可设为全局默认；显式 pin 创建的 run snapshot 保存各自准确的 historical evidence hash 与 canonical hash。
3. 未登记 hash 的 `owner_scope=system, system_cma_v2@1` 被阻断，不能生成新 run。

### R15（P2）：没有锁定真实模拟的固定 seed P50 回归值

`TestForwardReturnRegressionReport` 会运行固定 seed 并打印 P50，但只断言历史/基线/保守情景之间的方向关系；`TestSnapshotRecordsAssumptionProvenance` 只检查内存中的 snapshot struct。当前没有一条测试将真实 run 的固定 terminal P50、profile hash、evidence hash 一起锁定，因此今后校准/engine 变化仍可在方向关系成立的情况下显著改变最终资金。

**唯一修复方案**

新增独立的端到端回归 fixture：固定计划、持仓快照、50 年 horizon、simulation runs、seed、v3 profile 和市场数据。通过实际 create/run/read 流程断言 terminal `P50Minor` 的精确值，并从持久化 `input_snapshot_json` 断言 v3 id/version、canonical hash、evidence hash；任何公式、profile、采样或序列化变化都必须显式更新新 profile identity 和回归基线，不能仅更新期望数值。

**验收逻辑**

1. 同一 fixture 连续运行两次，terminal P50 与 input hash 完全相同。
2. P50 等于已审核常量，且 run snapshot 的四个 provenance 字段与 registry 匹配。
3. 使用 v1/v2 historical pin 的对照 run 保持各自原结果，不被 v3 公式影响。

## 验证记录

- `go test ./...`：通过。
- `go test ./internal/simulation -run 'Test.*Regression|Test.*Report' -v -count=1`：通过；当前固定 seed 报告的 blended-prior baseline terminal P50 为 `22,508,403,900` minor，但该值尚未被测试锁定。
- `web: npm run test:ci`：53 个文件、326 个测试通过。
- `web: npm run lint`：通过。
- `web: npm run build`：构建已通过编译阶段；后续单独复跑被残留的 Next build lock 拒绝，未修改或删除该构建产物。
- `git diff --check`：通过。

因 R13 为 P1，本次不整理到 `docs/`。
