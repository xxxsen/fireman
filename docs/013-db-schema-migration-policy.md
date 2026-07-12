# 数据库 Schema 与 Migration 规范

> 当前项目未上线，旧本地库可删除后由 migrations 重建；业务 schema 的唯一来源是 `migrations/`。

## 原则

- 业务 schema 的 DDL 只能放在 `migrations/`，包括 `CREATE TABLE`、`ALTER TABLE`、`CREATE INDEX`、`DROP` 等。
- migration SQL 只允许 DDL，禁止使用 `INSERT`、`UPDATE`、`DELETE`、`REPLACE` 修正、回填或初始化业务数据。
- Go 运行时代码不得做业务表的 schema repair、动态 `ALTER TABLE` 或按列探测后补 schema。
- 测试 fixture 可以写 DDL，但只能用于构造测试数据库状态。
- 基础设施例外：迁移器初始化 `schema_migrations` 表、SQLite PRAGMA、备份/完整性检查等不属于业务 schema 演进。

## 当前策略

- `migrations/0001_init.sql` 描述新建库完整基线 schema。
- 首次生产发布前只维护 `migrations/0001_init.sql`，不得增加增量 migration 文件；schema 变更直接修改完整基线并重建开发库。
- 当前未上线，不保留旧临时库兼容修补；遇到旧本地 DB 结构不一致时，删除旧 DB 并重新执行 migrations。
- 当前 `.dev-data` 中的业务数据需要修正时，直接调整开发数据库或重建开发数据库，不为数据修正创建 migration。
- 内置场景、系统现金和系统 FX 等参考数据由 `internal/bootstrap` 在建表后幂等初始化，不写入 migration。

## 验收方式

- `migrations/` 中只能存在 `0001_init.sql`，且 DML 扫描结果必须为零。
- 删除本地数据库后，应用应能通过基线建表并由 bootstrap 初始化完整参考数据。
- `PRAGMA integrity_check` 必须返回 `ok`，`PRAGMA foreign_key_check` 必须为空。
- `go test ./...` 必须通过。
