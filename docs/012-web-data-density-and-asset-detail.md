# 012 · Web 数据密度与资产详情重排

> 设计来源：`td/045-web-ux-data-density-and-asset-detail-refactor.md`、`td/053-asset-library-and-wizard-ui-refactor.md`、`td/056-fire-simulation-library-and-wizard-ui-fixes.md`、`td/057-td056-implementation-review.md`、`td/058-td057-implementation-review.md`（均已完整实施并稳定）。
> 本文自洽描述页面宽度与数据密度、资产选择分页、资产资料库分类与刷新、金额单位与配置 tooltip、资产详情重排、场景卡片权限等统一约定。除资料库分类的受控编辑与新增 `GET /api/v1/instruments/:id/return-series`、扩展 `GET /api/v1/instruments` 的分页/搜索、Dashboard allocation bar 明细外，不改变任何金额 / 权重 / 调仓 / 模拟计算口径。

## 1. 新建计划向导与页面容器宽度

- 向导根容器使用 `max-w-[96rem]`；计划基础、目标配置、建立持仓、确认组合四个步骤卡片及导航区均使用相同的 `w-full` 有效宽度。表单可读性通过卡片内响应式网格和局部 `max-w` 控制，而不再收窄步骤容器。
- 卡片以 `data-testid="wizard-step-card"` 标识便于测试定位。
- 宽表在移动端仍保留横向滚动，不破坏窄屏可读性。

## 2. 资产选择分页与滚动加载

- 后端 `GET /api/v1/instruments` 在带 `limit`/`q`/`cursor`/`offset` 任一参数时进入分页搜索分支，返回 `{ instruments, next_cursor, total }`；不带这些参数时保持旧的全量列表行为（资产资料库页、资产刷新页等沿用旧契约不受影响）。
- 仓储 `InstrumentRepo.Search` 支持按 `q`（代码/名称）、`asset_class`、`region`、`status`、`exclude_ids`、`exclude_system` 过滤，统一按 `created_at DESC, id DESC` 排序并 `LIMIT/OFFSET` 分页；无分页的资料库列表与分页搜索都通过 `LEFT JOIN instrument_library_metrics` 读取行情元数据、模拟资格及近 1/3/5 年年化收益，不在 HTTP 请求中逐资产读取全量行情。服务层补全引用计数，并在 `offset + 本页数量 < total` 时给出 `next_cursor`（offset 语义）。
- `instrument_library_metrics` 是资料库列表投影：包含 `data_as_of`、来源、点类型、质量、模拟资格、历史样本指标和近 1/3/5 年年化收益。收益以该标的自身最后交易日为截止日，因此停更标的仍按其实际可用历史展示收益；投影缺失、抓取中、抓取失败和系统标的统一展示 `—`，不会回退到同步计算。
- 异步导入、重试抓取和手工刷新在与 `market_data_points`、`instrument_annual_returns` 相同的事务内调用 `libmetrics.SyncTx`：非空历史更新投影，空历史删除投影。强制刷新或来源切换清空行情时，资料库不会保留旧日期、收益或模拟资格；投影 SQL 失败会使整笔写入回滚。
- 前端 `AssetClassHoldingPicker` 改用 `useInfiniteQuery`：聚焦即加载首页（默认 10 条最近标的），输入经 250ms 去抖后作为搜索词并重置游标，已选标的通过 `exclude_ids` 从候选中剔除；底部哨兵元素经 `IntersectionObserver` 触底加载下一页。
- 本地候选使用 48px 单行行高和固定 `30rem`（10 行）滚动视口；长名称截断，翻页不会改变下拉高度或挤压周边内容。已选资产显示在搜索框上方。
- 候选层遵循 combobox 关闭规则：点击 picker 外部或按 Escape 会同时收起本地候选、AKShare 候选、解析状态与错误；保留输入词，重新聚焦后可继续检索。
- AKShare 兜底解析仅在「输入形似基金代码」且「本地分页搜索已完成（非加载/刷新中）后仍无精确命中」时触发，避免在本地查询返回前发起无谓的三方请求或闪现无关错误。

## 3. 金额单位与大类配置 tooltip

- 新增 `formatMoneyScaled(minor, currency)`：金额按量级自动切换 元 / 万元 / 亿元 单位展示；组合预览的「计划基准规模」「已投资金」改用该格式。
- Dashboard `DashboardAllocationBar` 扩展出 `current_amount_minor`、`target_amount_minor` 与 `holdings` 明细（每个标的的当前/目标金额与占比）；`buildAllocationBars` 直接基于 `TargetView.Holdings` 聚合，跳过停用行，按固定业务大类顺序排序，类内持仓按金额降序。
- `AllocationBarChart` 自定义 tooltip 展示大类目标/当前占比与金额，并列出至多 8 条持仓明细；空持仓或 0 金额不报错，缺名展示为「—」。

## 4. 资产详情年度收益倒序

- 年度收益表按 `year` 倒序展示（最新年份在上）。

## 5. 工作台左侧菜单固定

- `AppShell` 桌面侧栏增加 `md:sticky md:top-0 md:h-screen md:overflow-y-auto`，正文滚动时侧栏固定并拥有自身滚动；移动端布局不变。侧栏以 `data-testid="app-sidebar"` 标识。

## 6. 资产详情页重排与收益曲线

- 顶部改为左右布局：左侧返回入口、资产名称、代码与关键元信息；右上角仅呈现刷新 / 删除操作（按权限与系统标的规则禁用或隐藏）。手工刷新统一立即执行并跳过 24 小时限制，不再向用户暴露“强制刷新”概念。基础信息归入独立分区卡片，主体由窄单列调整为 `max-w-6xl` 宽容器分区。
- 数据来源通过可读映射显示；例如 `ak.fund_open_fund_info_em:累计净值走势` 显示为“东方财富 · 公募基金 · 累计净值走势”，未知 adapter ID 不直接展示给用户。
- 普通非系统资产可在详情页编辑大类和地区，使用 `PATCH /api/v1/instruments/:instrument_id/classification` 和 `expected_updated_at` 乐观锁。资料库编辑只影响后续新建/新增资产；既有 `plan_holdings`、调仓和模拟输入保持计划级冻结分类。抓取中的资产禁止编辑，抓取失败的资产仍可编辑，重试会使用其当前分类。
- 新增 `compressYears(years)`：将纳入模拟的年份数组压缩为区间串（如 `2006-2009`），不连续年份展示为多个区间（如 `2006-2012、2014-2025`）。
- 新增 `GET /api/v1/instruments/:instrument_id/return-series?range=...`：基于行情点位计算归一化累计收益序列，支持 `3d/1w/1m/3m/6m/1y/3y/5y/all`；历史不足时返回 `insufficient_history` 状态。前端 `ReturnSeriesChart`（ECharts 折线）在「区间收益」分区附近渲染收益曲线，并提供区间切换；切换区间只重新请求曲线数据，不刷新整页详情。

## 7. FIRE 计划列表更新时间修正

- 新增 `formatDateFromMs(ts)`：按毫秒级时间戳格式化日期，0 / 空值显示为「—」；计划列表 `updated_at` 改用该函数，修正此前按秒解析导致的错误年份。

## 8. 场景配置卡片与编辑权限

- 场景卡片重排：`内置` 徽标与标题同行内联；复制 / 编辑 / 删除改为右上角图标按钮（`CopyIcon` / `EditIcon` / `TrashIcon`）。
- 权限收敛：内置场景不可编辑（隐藏编辑）；删除仅对「非内置且无计划引用」可见，复制始终可用。
- 文案条件化：权重合计文案仅在不等于 100% 时以告警形式提示；引用计数仅在 `plan_count > 0` 时展示。

## 9. 测试与门禁

- 为每个改动点补 / 改 Vitest 与 Go 单测，覆盖：分页/滚动加载、10 行视口、候选层关闭与已选剔除、AKShare 兜底触发条件、资料库分类乐观锁与计划冻结、抓取中编辑保护、资料库投影读取/停更资产收益/空历史清理/事务回滚、allocation bar 排序与聚合、return-series 归一化与历史不足、毫秒日期与年份压缩、场景卡片权限与文案、向导宽度与侧栏 sticky。
- 交付门禁：`go test ./...`、`make web-lint`、`make web-test`、`make web-build` 均通过。
