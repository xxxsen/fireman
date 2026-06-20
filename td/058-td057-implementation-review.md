# td/057 实施 Review

## Review 结论

`td/057` 已实现 migration 管理的 `instrument_library_metrics` 投影表，并将资料库列表与持仓搜索改为读取投影；成功导入、刷新和重试抓取均在写入行情的事务内重算投影。停更资产的区间收益也已改为以其自身 `data_as_of` 为截至日计算。

但仍有 1 项 P1 数据一致性问题：全量替换后若有效行情为空，旧投影不会被删除，资料库会展示已经从行情表中删除的旧日期、收益和模拟资格。因此本轮不转入 `docs` 落地文档。

## Findings

### 1. P1 全量刷新清空行情时保留旧投影，列表会展示失效的收益与模拟资格

位置：

- `internal/service/instrument_refresh_helpers.go:66-87`
- `internal/service/instrument_refresh_helpers.go:90-105`
- `internal/jobs/instrument_fetch_persist.go:34-45`
- `internal/jobs/instrument_fetch_persist.go:59-90`
- `internal/marketdata/library_projection.go:59-84`
- `internal/repository/instrument_library_metrics.go:52-90`

问题：

- 强制刷新或来源切换会令 `fullReplace=true`，`persistRefreshMarketDataTx` 先删除 `market_data_points`，随后以本次 `reprocessed.Points` 写入新数据。
- 上游成功返回空 points 时，当前流程不会报错：`ProcessProviderData` 会产生空 `Points`，`UpsertBatch` / `ReplaceAll` 也可成功执行。全量替换后，行情与年度收益已被清空。
- `ComputeLibraryProjection(nil)` 返回 `ok=false`，两个 `upsertLibraryMetricsTx` 都直接 `return nil`。它们既不 upsert，也不删除旧的 `instrument_library_metrics` 行。
- 该资产仍处于 `active`，`ListWithMetrics` 的 LEFT JOIN 会读取旧投影，因此页面继续展示旧 `data_as_of`、近 1/3/5 年收益、质量和 `simulation_eligible`，但详情和计划快照读取的实际行情已为空。该行为违背“投影与行情同一事务、绝不发散”的约束。

唯一修复方案：

- 在 `InstrumentLibraryMetricsRepo` 增加 `DeleteTx(ctx, tx, instrumentID)`，只执行 `DELETE FROM instrument_library_metrics WHERE instrument_id=?`。
- 将两个投影同步入口统一为一个“同步投影”语义：`ComputeLibraryProjection(points)` 返回 `ok=true` 时 upsert；返回 `ok=false` 时在同一 transaction 中调用 `DeleteTx`。不得把空历史视为无操作。
- `persistRefreshMarketDataTx` 与 `InstrumentFetchRunner.persistFetchedInstrument` 都调用该统一同步函数，移除两处各自复制的“空数据 no-op”逻辑。这样任意成功行情写入后，投影必然精确对应当前 `market_data_points`；失败事务仍整体回滚，不删除原有有效投影。

验收逻辑：

- 为一个 active 资产预置市场行情、年度收益和有效投影；执行 `force=true` 的全量刷新，模拟上游成功返回空 points。事务提交后，`market_data_points`、`instrument_annual_returns` 和 `instrument_library_metrics` 都不含该资产记录；`GET /api/v1/instruments` 显示数据截至和近 1/3/5 年收益均为 `—`，`simulation_eligible=false`。
- 对来源切换触发的 `fullReplace=true` 重复上述场景，结果一致。
- 刷新写入非空行情时，投影正常 upsert，日期、收益和质量与最新行情一致。
- 投影 upsert/delete 任一 SQL 失败时，行情、年度收益、名称与投影全部回滚到刷新前状态；补 service/job 集成测试覆盖该事务原子性。

## 已验证项

- `0015_instrument_library_metrics.sql` 由 migration 管理，没有运行时 DDL；列表和搜索使用 `LEFT JOIN` 读取投影。
- 普通资料库列表不再调用逐资产 `enrichMarketMeta`；投影缺失的 active 资产显示 `—`，不会回退到 HTTP 请求内同步计算。
- 投影保存 `data_as_of`、来源、点类型、质量、模拟资格、样本指标和近 1/3/5 年年化收益；停更资产测试确认收益以该资产最后交易日为截至日。
- pending、fetch_failed 与系统资产会清除列表投影字段，不展示旧收益。

## 验证记录

- `git diff --check` 通过。
- `go test ./...` 通过。

## 文档状态

`td/057` 尚未完整实施：存在上述 P1。修复并复审通过前，不新增或改写 `docs` 落地文档。
