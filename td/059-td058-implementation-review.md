# td/058 实施 Review

## Review 结论

`td/058` 已完整实施，未发现新的缺陷或实现缺失。

- `instrument_library_metrics` 在非空历史时 upsert、空历史时删除，投影不再可能在全量替换清空行情后残留。
- 异步导入/重试抓取与手工刷新共用 `internal/libmetrics.SyncTx`，消除了两处投影同步逻辑漂移风险。
- 强制刷新、来源切换、投影 SQL 失败回滚及异步导入投影失败回滚均已有集成测试覆盖。

## 已验证项

- `InstrumentLibraryMetricsRepo.DeleteTx` 仅通过事务执行 `DELETE FROM instrument_library_metrics WHERE instrument_id=?`；`SyncTx` 对空 points 不再静默 no-op。
- 手工刷新在删除/写入行情与年度收益后、触碰资产前调用 `SyncTx`；同步失败会令整个刷新事务回滚。
- 异步导入和重试抓取在同一事务内更新资产、行情、年度收益和投影；同步失败后数据回滚，资产进入 `fetch_failed`。
- 强制刷新且上游成功返回空行情时，`market_data_points`、`instrument_annual_returns`、`instrument_library_metrics` 均被清空，列表返回空数据截止与空区间收益，模拟资格为 false。
- 非 force 的来源切换触发全量替换时，空行情同样清空投影；投影 delete SQL 失败时，行情、年度收益和投影全部保持刷新前状态。

## 验证记录

- `go test -count=1 ./...` 通过。
- `git diff --check` 通过。

## 文档状态

正式行为已同步至：

- `docs/012-web-data-density-and-asset-detail.md`
- `docs/013-db-schema-migration-policy.md`
