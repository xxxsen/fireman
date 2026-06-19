# td/046 实施 Review

## 结论

`td/046` 指出的 P2 行为缺陷已修复：建立持仓资产选择现在会等待本地分页搜索完成后，才在无精确命中时触发 AKShare 外部解析；同时补充了“本地搜索 pending 后返回精确代码时不调用 AKShare”的测试。`docs/012-web-data-density-and-asset-detail.md` 也已同步更新外部解析触发条件。

本轮 review 发现 1 个独立的 P2 架构缺陷：运行时代码仍包含硬编码 DDL schema repair 逻辑。项目当前有 `migrations/` 作为唯一 schema 演进入口，且当前项目未上线，可以通过重建 DB 消化旧结构；因此这类兼容修补不应继续留在运行时代码中。

## 发现的问题

### P2 · 运行时代码包含硬编码 DDL schema repair

位置：

- [`internal/db/schema_repair.go`](/home/sen/work/fireman/internal/db/schema_repair.go:14)
- [`internal/db/migrations.go`](/home/sen/work/fireman/internal/db/migrations.go:101)
- [`internal/db/schema_repair_test.go`](/home/sen/work/fireman/internal/db/schema_repair_test.go:11)

问题：

`repairSnapshotSchema` 在迁移完成后仍会执行运行时 schema 检测与 `ALTER TABLE` 修补，包括：

- 将 `instrument_simulation_snapshots.observation_count` 重命名为 `daily_observation_count`。
- 按缺失列动态追加 `monthly_return_count`、`volatility_method`、`metrics_version`、`history_depth`。

这些 DDL 语句绕过 `migrations/` 的版本化迁移记录，导致 schema 来源分散：同一张表既可能由 `migrations/*.sql` 创建/变更，也可能由 Go 运行时代码临时修补。后续排查 schema 版本、重建数据库、审核迁移历史时，会无法只依赖 migrations 得到完整结论。

影响：

- schema 演进不可追踪：`schema_migrations` 不记录这些运行时 ALTER。
- 新环境和旧环境可能走不同结构路径，增加测试与线上行为差异。
- 当前项目未上线，保留兼容修补收益很低，反而会固化一套非迁移体系。

修复方案：

删除运行时 schema repair 路径，统一以 migrations 为 schema 唯一来源：

- 删除 `internal/db/schema_repair.go` 和 `internal/db/schema_repair_test.go`。
- 删除 `internal/db/migrations.go` 中迁移完成后对 `repairSnapshotSchema(ctx, pool)` 的调用。
- 不新增兼容迁移来修复旧临时结构；当前项目未上线，直接删除本地旧 DB 或测试 DB 后由 `migrations/0001_init.sql` 与后续 migrations 重建。
- 如后续确实需要修改已存在 schema，只新增顺序迁移文件，例如 `migrations/0014_xxx.sql`，不要在 Go 代码中写 DDL 修补。
- 在 `agents.md` 的数据库规范中补充“DDL 只能出现在 migrations 或测试 fixture 中；运行时代码不得做 schema repair/ALTER TABLE”的约束。

验收逻辑：

- `rg -n "ALTER TABLE|CREATE TABLE|CREATE INDEX|DROP TABLE" internal migrations` 的非测试结果中，DDL 只出现在 `migrations/`、迁移执行器必要的 `schema_migrations` 初始化、SQLite PRAGMA/备份校验等基础设施代码中；业务 schema DDL 不再出现在运行时 repair 逻辑。
- 删除本地数据库后启动或运行测试，完整 schema 仍由 migrations 重建成功。
- `go test ./internal/db ./internal/api ./internal/service` 通过。
- `go test ./...` 通过。

## 已核对项

- [`web/components/plans/AssetClassHoldingPicker.tsx`](/home/sen/work/fireman/web/components/plans/AssetClassHoldingPicker.tsx:112)：新增 `listSettled`，外部解析必须等待本地 `useInfiniteQuery` 非 loading / fetching 后才可触发。
- [`web/components/plans/AssetClassHoldingPicker.tsx`](/home/sen/work/fireman/web/components/plans/AssetClassHoldingPicker.tsx:121)：`shouldResolve` 已同时要求 `looksLikeFundCode(q)`、本地查询完成、且无精确命中。
- [`web/components/plans/AssetClassHoldingPicker.test.tsx`](/home/sen/work/fireman/web/components/plans/AssetClassHoldingPicker.test.tsx:162)：新增本地搜索 pending 后返回精确代码时不调用 `resolveImport` 的回归测试。
- [`docs/012-web-data-density-and-asset-detail.md`](/home/sen/work/fireman/docs/012-web-data-density-and-asset-detail.md:17)：已把 AKShare 兜底解析条件更新为“本地分页搜索已完成后仍无精确命中”。

## 验证记录

- `go test ./...`：通过。
- `cd web && npm run lint`：通过。
- `cd web && npm run test:ci`：通过，49 个测试文件 / 245 个用例全绿。
- `cd web && npm run build`：通过。

## 残余风险

- 未做浏览器手工验收；本轮判断基于代码审查和自动化门禁。
- 当前提交中还包含若干 gofmt/文档措辞类调整，不影响 td/046 修复结论，但提交前应确保这些改动是用户预期范围。
