# td/056 实施 Review

## Review 结论

`td/056` 已完成模拟路径图表/金额展示、代表路径排序、路径月度明细与冻结资产标签、资料库分类编辑入口、指标说明、组合总览对齐、默认年龄和向导布局等主体改造。

但资料库列表的区间收益实现仍有 1 项 P1：列表请求保留逐资产全量行情查询，且收益窗口以服务当前日期截断，停更资产会被错误显示为无可用收益。这不满足方案中“1000 条资产无逐行请求/SQL 退化、按实际数据截至日计算”的约束。因此本轮不转入 `docs` 落地文档。

## Findings

### 1. P1 资料库列表仍存在 N+1 全量行情读取，且停更资产的 3/5 年收益会被错误置空

位置：

- `internal/service/instrument_service.go:60-79`
- `internal/service/instrument_service.go:97-123`
- `internal/service/instrument_service.go:150-163`
- `internal/service/instrument_service.go:184-193`
- `internal/service/plan_instrument.go:116-152`
- `internal/repository/market_data.go:77-132`

问题：

- `List` 与 `Search` 仍逐条调用 `enrichMarketMeta`；该方法分别执行 `LastTradeDate`、`LatestPointMeta`，并通过 `libraryMetricsAtDate` 调用 `ListByInstrument` 读取该资产全量历史行情。1000 条资产至少产生 3,000 次 SQL 查询，并在每次列表请求重新读取/计算全量历史，和 td/056 要求的批量列表实现相反。
- 新增的 `attachTrailingReturns` 虽使用批量查询，但固定从“今天 - 6 年 7 天”开始取数，并以今天作为 `as_of`。`ComputeTrailingReturns` 实际会使用资产最后一个点作为结束日；若资产最后交易日早于今天超过约 1 年，计算近 5 年时所需的起点会早于固定下界，导致本来可计算的近 3/5 年年化收益被判定为历史不足并展示为 `—`。
- 例如数据实际截至 `2024-01-01` 且拥有 `2018-12-28` 至 `2024-01-01` 的完整行情，在 2026 年访问列表时，固定查询下界约为 2020 年，`2024-01-01` 的近 5 年起点被截掉；页面会显示数据截至 2024 年，但近 5 年收益错误为空，口径自相矛盾。
- 当前新增测试只验证近期数据和单次批量查询，未覆盖 1000 条查询数量，也未覆盖“数据截至日早于当前日期”的收益窗口。

唯一修复方案：

- 新增由 migration 创建的 `instrument_library_metrics` 列表投影表，主键为 `instrument_id`，持久化列表已有的行情元数据和模拟资格字段（`data_as_of`、来源、点类型、质量、历史深度、完整年度数等），以及以该资产 `data_as_of` 为结束日计算的近 1/3/5 年年化收益。
- 在异步导入、刷新和重试抓取成功写入 `market_data_points` 的同一事务内，使用完整该资产行情重算并 upsert 投影；数据抓取失败、pending 与系统现金不写可用收益。不得在 HTTP 列表请求中重新计算这些字段。
- `InstrumentRepo.List/Search` 通过一次 `LEFT JOIN instrument_library_metrics` 返回投影字段；移除列表/搜索路径上的逐条 `enrichMarketMeta` 与 `attachTrailingReturns`。详情页仍可按需使用完整行情计算其明细，不复用列表请求路径。
- 该表和索引必须只通过 migration 管理，不能在运行时代码执行 DDL。项目尚未上线，现有开发库重建后由导入/刷新流程生成投影；投影缺失时列表统一显示 `—`，不得回退到逐资产同步计算。

验收逻辑：

- 对 1000 个 active 资产访问 `/api/v1/instruments` 或分页搜索时，列表元数据与区间收益查询数为固定常数，不随资产数线性增加；测试使用 SQL query counter 断言没有逐资产 `LastTradeDate`、`LatestPointMeta` 或 `ListByInstrument` 调用。
- 构造数据截至 2024-01-01、历史覆盖 2018-12-28 至 2024-01-01 的资产，在 2026 年访问列表仍显示 `data_as_of=2024-01-01`、近 5 年年化收益；结果与详情页以 2024-01-01 为截至日的 `annualized_return` 一致。
- 导入/刷新成功后，投影的截至日期、来源、质量和 1/3/5 年收益同步更新；抓取失败、抓取中、历史不足和系统资产均显示 `—`，不返回过期数值。
- 资料库桌面列表、移动卡片和持仓搜索 API 的既有字段保持兼容；详情页仍可读取完整行情和展示原有模拟窗口。

## 已验证项

- `WealthPathChart` 使用 p25 作为隐藏堆叠下界、`p75-p25` 作为可见蓝色区间，并仅在底部展示 `P25-P75` 与 `P50`；tooltip 不再泄露内部序列。
- 代表路径在服务端和前端均按 `P00、P25、P50、P75、P95` 排序，路径相关金额统一显示为无千分位的 `¥xx.xxw`。
- 路径详情已移除 table spacer 虚拟化，月度/年度表分别渲染实际行与明确空状态；API 从运行时冻结 `input_snapshot_json` 派生 `asset_labels`，年末权重不再暴露 holding ID。
- 资产详情顶部提供“编辑大类和地区”入口，抓取中隐藏、失败后允许编辑；CAGR、年化波动率和最大回撤均已加入计算说明。
- 大类和地区图表使用同高说明行；新建计划向导的当前年龄与普通计划默认参数均为 35，向导步骤已使用统一的响应式内部网格。

## 验证记录

- `git diff --check` 通过。
- `go test ./...` 通过。
- `cd web && npm run lint` 通过。
- `cd web && npm run test:ci` 通过，50 个测试文件、290 个测试通过。
- `cd web && npm run build` 通过。

## 文档状态

`td/056` 尚未完整实施：存在上述 P1。修复并复审通过前，不新增或改写 `docs` 落地文档。
