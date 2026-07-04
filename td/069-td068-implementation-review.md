# TD 068 实施评审：全量 system namespace 审计

- 评审日期：2026-06-20
- 对照范围：`td/068-td067-implementation-review.md` 的 R16
- 评审方式：静态代码审查、TD 066 已升级数据库 fixture、未知 system v1/v2 fixture、后端全量与定向测试、前端测试/Lint/生产构建。
- 结论：**通过**。TD 068 已修复“当前 v3 存在便 fast-return”的遗漏；system namespace 的修复和内容审计现在是统一 invariant，而不是只在首次发布 v3 时执行。未发现新的缺陷或实施缺失。

## 验收结果

| 验收项 | 结果 | 证据 |
| --- | --- | --- |
| 已有正确 v3 时仍修复 user `system_cma_*` | 通过 | fast path 先探测 reserved user rows；存在时进入事务执行 `repairReservedUserProfilesTx` |
| 计划 pin / 全局默认引用重定向 | 通过 | `TestEnsureSystemDefaultRepairsUserV2OnPublishedV3DB` 验证二者迁移到确定性的 `user_legacy_<hash>` |
| 已有 v3 字节不变 | 通过 | 同一升级测试断言 v3 canonical bytes/hash 不变 |
| 无未知 system 内容才允许 fast-return | 通过 | `systemNamespaceClean` 对所有 system `system_cma_*` rows 调用 registry/hash 检查 |
| 未知 system v1/v2 启动即阻断 | 通过 | `TestEnsureSystemDefaultRejectsUnknownSystemRowsOnPublishedV3DB` 验证首次 `EnsureSystemDefault` 返回 `system_profile_identity_conflict` |
| 已登记 v1、TD064 v2、TD065 v2 变体和 v3 仍可审计 | 通过 | 历史 variant registry 与 raw canonical SHA-256 校验保留；TD065 v2 仍映射自己的 evidence hash |
| 后续检查不覆盖用户主动默认 | 通过 | 当前 v3 已存在时，仅做修复/audit，不调用 v2→v3 默认迁移 |

## 实现要点

`EnsureSystemDefault` 现在只在以下条件同时满足时返回：

1. 当前 `system_cma_v3@1` 存在、owner 为 system，且 stored hash 与 canonical raw bytes hash 都等于 registry hash；
2. 不存在 owner=user 的 `system_cma_*` row；
3. 所有 owner=system 的 `system_cma_*` row 都能以 `(id, version, content_hash)` 在历史内容 registry 中匹配，且 canonical raw bytes 未被篡改。

否则进入事务：先迁移所有 reserved user profile，再审计所有 reserved system row，最后仅在 v3 缺失时发布 v3 并迁移直接前驱默认。这样既能修复 TD 066 的真实升级状态，也避免将历史用户选择误改为 v3。

历史 canonical bytes 的 SHA-256 直接参与校验，避免用当前 struct 反序列化再序列化时因字段演进产生 hash 漂移。

## 文档整理

实现说明已输出至：

- `docs/016-simulation-assumption-profile-integrity.md`
- `docs/002-implemented-features.md` 已补充入口。

## 验证记录

- `go test ./... -count=1`：通过。
- `go test ./internal/repository -run 'TestEnsureSystemDefaultRepairsUserV2OnPublishedV3DB|TestEnsureSystemDefaultRejectsUnknownSystemRowsOnPublishedV3DB' -v -count=1`：通过。
- `web: npm run test:ci`：53 个文件、326 个测试通过。
- `web: npm run lint`：通过。
- `web: npm run build`：通过（编译、TypeScript、静态页面生成及 route 输出完成）。
- `git diff --check`：通过。
