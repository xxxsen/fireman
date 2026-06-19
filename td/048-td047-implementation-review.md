# td/047 实施 Review

## 结论

`td/047` 指出的运行时硬编码 DDL / schema repair 问题已完成整改：

- 已删除 `internal/db/schema_repair.go`。
- 已删除 `internal/db/schema_repair_test.go`。
- `internal/db/migrations.go` 已移除迁移完成后的 `repairSnapshotSchema(ctx, pool)` 调用。
- `migrations/0012_snapshot_metrics_columns.sql` 明确保留为 no-op，说明当前未上线场景下旧库通过重建 DB 处理。
- `agents.md` 已补充“业务 schema DDL 只能出现在 migrations 或测试 fixture 中，运行时代码不得做 schema repair / 动态 ALTER TABLE”的约束。
- 已新增 `docs/013-db-schema-migration-policy.md`，将 schema/migration 规范整理为可长期查阅文档。

本轮 review 未发现新的 P0 / P1 / P2 缺陷。

## 已核对项

- [`internal/db/migrations.go`](/home/sen/work/fireman/internal/db/migrations.go:98)：迁移循环结束后直接返回，不再执行运行时 schema repair。
- [`internal/db/schema_repair.go`](/home/sen/work/fireman/internal/db/schema_repair.go)：已删除。
- [`internal/db/schema_repair_test.go`](/home/sen/work/fireman/internal/db/schema_repair_test.go)：已删除。
- [`migrations/0012_snapshot_metrics_columns.sql`](/home/sen/work/fireman/migrations/0012_snapshot_metrics_columns.sql:1)：已明确 no-op 原因，并说明旧 DB 通过 migrations 重建。
- [`migrations/0001_init.sql`](/home/sen/work/fireman/migrations/0001_init.sql:166)：`instrument_simulation_snapshots` 已包含 `daily_observation_count`、`monthly_return_count`、`volatility_method`、`metrics_version`、`history_depth`，新库重建不依赖 runtime repair。
- [`agents.md`](/home/sen/work/fireman/agents.md:112)：已新增 DDL 放置约束。
- [`docs/013-db-schema-migration-policy.md`](/home/sen/work/fireman/docs/013-db-schema-migration-policy.md:1)：已新增稳定规范文档。

## DDL 搜索核对

执行：

```bash
rg -n "repairSnapshotSchema|schema_repair|ALTER TABLE|CREATE TABLE|CREATE INDEX|DROP TABLE|ADD COLUMN|RENAME COLUMN" internal migrations -S
```

结果确认：

- 未再出现 `repairSnapshotSchema` 或 `schema_repair`。
- 业务 DDL 只出现在 `migrations/`。
- `internal/db/migrations.go` 中仅保留迁移器自身的 `schema_migrations` 初始化。
- `internal/db/db_test.go` 中的 DDL 属于测试 fixture。

## 验证记录

- `go test ./...`：通过。
- `cd web && npm run lint`：通过。
- `cd web && npm run test:ci`：通过，49 个测试文件 / 245 个用例全绿。
- `cd web && npm run build`：通过。

## 残余风险

- 当前策略明确不兼容旧临时库；如果本地已有旧 DB，需要删除后由 migrations 重建。
- 未做手工启动验收；本轮判断基于代码审查、DDL 搜索和自动化门禁。
