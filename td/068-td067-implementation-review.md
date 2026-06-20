# TD 067 实施评审：系统命名空间、历史变体与 P50 回归

- 评审日期：2026-06-20
- 对照范围：`td/067-td066-implementation-review.md` 的 R13、R14、R15
- 评审方式：静态代码审查、升级 fixture 审查、核心后端包测试、固定 50 年 P50 端到端回归、前端测试/Lint/生产构建编译阶段。
- 结论：**不通过**。R13–R15 的核心能力已经实现：新 profile 不能占用系统 namespace、v3 当前 identity 会校验 hash、TD 065 v2 变体已登记、50 年 P50 已锁定。但 `EnsureSystemDefault` 在“v3 已正确存在”时直接 fast-return，跳过了历史 reserved user profile 的修复与旧 system row 的完整性检查。TD 066 已部署的数据库可以处于该状态，故 R13/R14 的升级保证仍不完整。

## 已完成且验收通过

### R13：系统 id 保护与当前 v3 校验

- `system_cma_` 已定义为保留前缀；create 与 validate API 都拒绝 user profile 使用该前缀，错误码为 `assumption_profile_reserved_id`。
- 新建 user profile 的 v1 id 已由服务端生成 `user_<uuid>`，客户端提交的普通 id 不再被信任。
- `EnsureSystemDefault` 对当前 `system_cma_v3@1` 校验 `owner_scope=system`、存储 hash、canonical JSON 重算 hash 与 registry hash 一致；当前 identity 发生篡改时返回 `system_profile_identity_conflict`，且不覆盖原数据。
- 在 v3 缺失或 v3 被 user profile 占用的升级路径中，已有迁移会复制 user profile 为确定性的 `user_legacy_<old hash 前 16 位>`，重定向 plan pin 和全局默认，再发布真正 v3。
- run snapshot 只有对 `owner_scope=system` 且 `(id, version, content_hash)` 被 registry 认可时才写入 evidence hash；用户 profile 不会继承官方 evidence。

### R14：TD 065 v2 历史变体

- `HistoricalSystemProfileVariant` 已按 `(id, version, content_hash)` 登记 v1、TD 064 v2、TD 065 v2 与 v3。
- TD 065 v2 的实际 canonical fixture 已加入；升级测试确认该 variant 保持字节不变、默认迁移到 v3。
- explicit pin 的 TD 065 v2 snapshot 使用自己的 evidence hash；TD 064 v2 保留空 evidence hash；未知的 system v2 在进入 slow-path 时被拒绝。

### R15：固定 seed 的真实 P50 回归

新增 API 层真实 create → run → read 测试，固定 50 年 horizon、seed `424242` 和 1,000 paths。它锁定：

- v3 terminal P50：`577,080,841` minor；
- v1/v2 control terminal P50：`527,399,522` minor；
- 相同输入两次运行的 input hash 与 P50 一致；
- 读取持久化 `input_snapshot_json` 后，profile identity、canonical hash、evidence hash 均符合预期。

## 阻断问题

### R16（P1）：已有正确 v3 时 fast path 跳过历史 namespace 修复，TD 066 数据库仍可保留 user `system_cma_*` profile

**证据**

`EnsureSystemDefault` 先读取当前 `system_cma_v3@1`。只要它存在且 `owner_scope=system`，即直接返回 `assertRecognizedCurrentIdentity`，不会进入 `runSystemDefaultUpgrade`。而 `repairReservedUserProfilesTx` 与 `assertRecognizedSystemV2` 都只在后者中调用。

这遗漏了一个真实升级状态：TD 066 已经在数据库中发布了正确 v3；在 TD 067 之前，fresh install 不会创建 v2，用户仍可通过旧 API 创建并启用 `owner_scope=user, id=system_cma_v2@1`，也可把它设为全局默认。升级到当前实现时，v3 的 fast path 返回成功，该旧 user v2 不会被迁移为 `user_legacy_*`，全局默认和 pinned plan 也不会被重定向。

因此系统 namespace 在这类已部署库中仍可被占用；该 user v2 会被认为是普通 active user profile，可继续作为默认运行。虽然 snapshot 不会再错误写入官方 evidence hash，但这仍违反 TD 067 “迁移 every historical reserved user profile、保留系统 identity 不可重用”的数据修复要求。

同一 fast path 还跳过 `assertRecognizedSystemV2`：已有 v3 的库若含未知 `owner_scope=system, system_cma_v2@1`，启动/list 不会报 conflict，只会在后续该 profile 被 pin 构建 snapshot 时才失败，完整性检查时机不一致。

**影响**

TD 067 对新建数据有效，但不能收敛 TD 066 已部署数据库的历史冲突。用户可能继续把一个名字看似系统 v2 的自定义模型作为新计划默认；之后再发布 v4 或进行批量审计时，仍会遇到同一 identity 占用问题。系统 profile 完整性也不再是启动时统一验证的 invariant。

**唯一修复方案**

将 fast path 收紧为“没有任何 repair/audit 工作时才允许返回”：

1. 先只读探测三项：当前 v3 是否为已验证 system content；是否存在任意 `owner_scope=user AND id LIKE 'system\\_cma\\_%'`；是否存在任意 `owner_scope=system AND id LIKE 'system\\_cma\\_%'` 的未登记 `(id, version, content_hash)`。
2. 仅当三项分别为“是、否、否”时 fast-return。否则一律进入一个事务，在事务内重新读取并执行 `repairReservedUserProfilesTx`，再对**所有**保留 namespace 的 system rows（v1、v2、v3 及未来 identity）逐条用 `LookupSystemContent` 校验 stored/recomputed canonical hash；未知内容返回 `system_profile_identity_conflict`。
3. 完成 repair/audit 后，重新验证当前 v3；如果不存在则发布 v3，存在则保持字节不变。只有本次事务确实发布了 v3 时才执行 v2→v3 默认迁移，避免覆盖用户有意保留的旧 profile 选择。

该方案保留当前的确定性 `user_legacy_<hash>` 迁移语义，不覆盖任何用户数据，并使每次启动/读取都满足统一 identity invariant。

**验收逻辑**

1. 构造“TD 066 已升级”数据库：先插入真实 v3，再插入 active user `system_cma_v2@1`，并让一个 plan pin 和 global default 都指向它。执行升级后，user row 被迁移到 `user_legacy_<old hash 前 16 位>`，plan pin/default 均指向新 id，真实 v3 canonical bytes/hash 未变。
2. 同一场景再次执行 `EnsureSystemDefault`，无数据变化；`ListProfiles` 中不存在 owner=user 且 id 以 `system_cma_` 开头的记录。
3. 构造正确 v3 加未知 owner=system v2（以及未知 system v1）数据库：首次 `EnsureSystemDefault` 即返回 `system_profile_identity_conflict`，不必等到创建 pinned run。
4. 对修复后的 user profile 创建 run，evidence hash 为空；对 v3 创建 run，identity/canonical/evidence hash 与 current registry 精确相等。

## 验证记录

- `go test ./internal/assumptions ./internal/repository ./internal/service ./internal/api -count=1`：通过。
- `go test ./internal/api -run 'TestForwardP50RegressionE2E|TestCreateUserProfileRejectsReservedID' -v -count=1`：通过；v3 P50 为 `577,080,841` minor，v1/v2 control 均为 `527,399,522` minor。
- `web: npm run test:ci`：53 个文件、326 个测试通过。
- `web: npm run lint`：通过。
- `web: npm run build`：已通过编译阶段；工具返回完成前未提供 Next 的最终 route 输出，且未检测到残留 build 进程，因此不将生产构建记为完整通过。
- `git diff --check`：通过。

因 R16 为 P1，本次不整理到 `docs/`。
