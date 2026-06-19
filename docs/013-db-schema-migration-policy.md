# 数据库 Schema 与 Migration 规范

> 来源：`td/047-td046-implementation-review.md` 后续整改。当前项目未上线，旧本地库可删除后由 migrations 重建；业务 schema 的唯一来源是 `migrations/`。

## 原则

- 业务 schema 的 DDL 只能放在 `migrations/`，包括 `CREATE TABLE`、`ALTER TABLE`、`CREATE INDEX`、`DROP` 等。
- Go 运行时代码不得做业务表的 schema repair、动态 `ALTER TABLE` 或按列探测后补 schema。
- 测试 fixture 可以写 DDL，但只能用于构造测试数据库状态。
- 基础设施例外：迁移器初始化 `schema_migrations` 表、SQLite PRAGMA、备份/完整性检查等不属于业务 schema 演进。

## 当前策略

- `migrations/0001_init.sql` 描述新建库完整基线 schema。
- 后续 schema 变更通过顺序 migration 文件表达，例如 `migrations/0014_xxx.sql`。
- 当前未上线，不保留旧临时库兼容修补；遇到旧本地 DB 结构不一致时，删除旧 DB 并重新执行 migrations。
- `migrations/0012_snapshot_metrics_columns.sql` 是 no-op，用于保留历史迁移编号；相关列已经在 `0001_init.sql` 中声明。

## 验收方式

- `rg -n "ALTER TABLE|CREATE TABLE|CREATE INDEX|DROP TABLE" internal migrations -S` 中，业务 schema DDL 应只出现在 `migrations/` 或测试文件。
- 删除本地数据库后，应用或测试应能通过 migrations 创建完整 schema。
- `go test ./...` 必须通过。
