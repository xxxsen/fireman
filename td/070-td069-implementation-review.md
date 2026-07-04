# TD 070 实施评审：TD 069 system namespace 修复复核

- 评审日期：2026-07-04
- 对照范围：`td/069-td068-implementation-review.md` 的实施结果
- 评审方式：静态代码审查、升级路径对照、文档核对、后端全量测试、前端 lint/build、diff whitespace 检查。
- 结论：**通过**。未发现新的 bug 或实现缺失。TD 069 要求的“已有正确 v3 时仍执行 reserved namespace repair/audit”已落实；文档已完整输出到 `docs/016-simulation-assumption-profile-integrity.md`，并在 `docs/002-implemented-features.md` 建立入口。

## 复核结果

| 复核项 | 结果 | 证据 |
| --- | --- | --- |
| fast path 不再跳过 reserved user 修复 | 通过 | `EnsureSystemDefault` 先执行 `systemNamespaceClean`；存在 owner=user `system_cma_*` row 时返回 dirty 并进入事务 |
| reserved user profile 迁移与引用重定向 | 通过 | `repairReservedUserProfilesTx` 在同一事务中复制为 `user_legacy_<hash>`、重定向 `plan_parameters` 与 global preference 后删除旧 row |
| 已有正确 v3 时仍审计所有 system reserved rows | 通过 | `existsUnrecognizedSystemRow` / `auditReservedSystemRows` 遍历所有 owner=system 且 id 为 `system_cma_*` 的 row |
| 未知或篡改 system content 启动即阻断 | 通过 | `(id, version, content_hash)` 必须命中 `LookupSystemContent`，且 raw canonical bytes SHA-256 必须等于 stored hash |
| 已有 v3 不覆盖用户主动默认 | 通过 | 当 v3 已存在且通过审计时直接返回，不再调用 `migrateDefaultToCurrentSystem` |
| 历史 canonical bytes 不因 struct 演进漂移 | 通过 | 校验改为 raw canonical bytes SHA-256，而不是反序列化后重新 canonicalize |
| 文档输出 | 通过 | `docs/016-simulation-assumption-profile-integrity.md` 描述 profile identity、namespace audit、run provenance 与固定回归基线；`docs/002-implemented-features.md` 已补入口 |

## 缺陷与修复方案

本轮 review 未发现 bug，因此没有需要执行的修复方案。

若后续出现同类问题，验收逻辑应保持当前最小闭环：构造“正确 v3 + reserved user row + plan pin/default”与“正确 v3 + 未知 system v1/v2 row”两类数据库状态，首次 `EnsureSystemDefault` 分别验证自动迁移或立即返回 `system_profile_identity_conflict`，并确认真实 v3 canonical bytes/hash 不变。

## 验证记录

- `go test ./internal/repository ./internal/api ./internal/service ./internal/assumptions -count=1`：通过。
- `go test ./... -count=1`：通过。
- `web: npm run lint`：通过。
- `web: npm run build`：通过。
- `git diff --check`：通过。
