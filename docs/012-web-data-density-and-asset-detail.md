# 012 · Web 数据密度与资产详情重排

> 本文自洽描述页面宽度与数据密度、资产选择分页、金额单位与配置 tooltip、资产详情重排、场景卡片权限等统一约定，不改变任何金额 / 权重 / 调仓 / 模拟计算口径。
>
> 资产数据统一来自全局市场资产目录：候选与详情读取 `market_assets` / `market_asset_points` / `market_asset_detail_projections`，模拟输入使用 `market_asset_simulation_snapshots`；完整架构见 [021-market-data-task-worker-architecture.md](./021-market-data-task-worker-architecture.md)。

## 1. 新建计划向导与页面容器宽度

- 向导根容器使用 `max-w-[96rem]`；步骤卡片及导航区均使用相同的 `w-full` 有效宽度。卡片内各步骤内容统一收敛到 `max-w-6xl` 可读宽度：计划目标、建立持仓、确认组合三步的表单、标签页面板与确认表格共用同一内容宽度，边框容器与内容宽度保持一致（选标 picker 顶层不再自带边框，由标签页面板提供单层边框）。
- 卡片以 `data-testid="wizard-step-card"` 标识便于测试定位。
- 宽表在移动端仍保留横向滚动，不破坏窄屏可读性。

## 2. 资产选择分页与滚动加载

- 候选数据来自 `GET /api/v1/market-assets`：形似证券代码的输入走 `symbol_q`（匹配代码，含 `region_code+symbol` 形式），其余输入走 `name_q` 名称匹配，支持 `market` / `instrument_type` 过滤与 `limit`/`offset` 分页，返回 `{ assets, total, syncs }`。
- 前端 `AssetClassHoldingPicker` 使用 `useInfiniteQuery`：聚焦即加载首页，输入经去抖后作为搜索词并重置游标，已选资产在前端从候选中剔除；底部哨兵元素经 `IntersectionObserver` 触底加载下一页。
- 每条候选展示完整资产身份（名称、代码、资产类型、市场/交易所）与历史数据就绪状态：已同步显示「数据截至 + 来源」；同步中 / 同步失败 / 未同步分别有独立提示，缺历史不阻塞选择，模拟前由 readiness 检查拦截。
- 精确代码命中多个资产身份（如 `150015` 同时存在场外基金与场内基金）时，候选列表顶部显示冲突提示，精确命中排在模糊匹配前，同代码候选按「公募基金 → 场内 ETF / LOF → A 股 → 其他」排序，不自动选中任一项。
- 本地候选使用 48px 单行行高和固定 `30rem`（10 行）滚动视口；长名称截断，翻页不会改变下拉高度或挤压周边内容。已选资产显示在搜索框上方。
- 候选层遵循 combobox 关闭规则：点击 picker 外部或按 Escape 收起候选与错误提示；保留输入词，重新聚焦后可继续检索。

## 3. 金额单位与大类配置 tooltip

- 新增 `formatMoneyScaled(minor, currency)`：金额按量级自动切换 元 / 万元 / 亿元 单位展示；组合预览的「计划基准规模」「已投资金」改用该格式。
- Dashboard `DashboardAllocationBar` 扩展出 `current_amount_minor`、`target_amount_minor` 与 `holdings` 明细（每个标的的当前/目标金额与占比）；`buildAllocationBars` 直接基于 `TargetView.Holdings` 聚合，跳过停用行，按固定业务大类顺序排序，类内持仓按金额降序。
- `AllocationBarChart` 自定义 tooltip 展示大类目标/当前占比与金额，并列出至多 8 条持仓明细；空持仓或 0 金额不报错，缺名展示为「—」。

## 4. 资产详情年度收益倒序

- 年度收益表按 `year` 倒序展示（最新年份在上）。

## 5. 工作台左侧菜单固定

- `AppShell` 桌面侧栏增加 `md:sticky md:top-0 md:h-screen md:overflow-y-auto`，正文滚动时侧栏固定并拥有自身滚动；移动端布局不变。侧栏以 `data-testid="app-sidebar"` 标识。

## 6. 资产详情页重排与收益曲线

- 市场资产详情页路由为 `/assets/market/[assetKey]`，数据来自 `GET /api/v1/market-assets/by-key`，返回资产元信息、历史同步状态（`market_asset_history_state` + 最近任务）、行情点位与 `market_asset_detail_projections` 投影（区间年化、年度收益）。
- 顶部为左右布局：左侧返回入口、资产名称、代码与关键元信息；右上角提供「刷新历史数据」与（可切换来源时的）「切换数据源」操作，均通过 `POST /api/v1/market-assets/history-sync` 创建异步任务，页面轮询任务状态直至终态。
- 数据来源通过可读映射显示；例如 TickFlow adapter 显示为用户友好 label，未知 adapter ID 不直接展示原始字符串给用户。
- 历史状态面板展示最近成功时间、数据截至日、点位数量、来源与点类型；无历史时给出引导文案，提示先创建同步任务。
- 收益曲线由 `ReturnSeriesChart`（ECharts 折线）基于本地行情点位渲染归一化累计收益；年度收益表按 `year` 倒序展示。
- 资产大类与地区不再在目录详情页编辑：分类属于计划持仓（`plan_holdings.asset_class` / `region`），在选择资产进入计划时由用户指定或按目录推断。

## 7. FIRE 计划列表更新时间修正

- 新增 `formatDateFromMs(ts)`：按毫秒级时间戳格式化日期，0 / 空值显示为「—」；计划列表 `updated_at` 改用该函数，修正此前按秒解析导致的错误年份。

## 8. 配置模板卡片与编辑权限

- 场景卡片重排：`内置` 徽标与标题同行内联；复制 / 编辑 / 删除改为右上角图标按钮（`CopyIcon` / `EditIcon` / `TrashIcon`）。
- 权限收敛：内置场景不可编辑（隐藏编辑）；删除仅对「非内置且无计划引用」可见，复制始终可用。
- 文案条件化：权重合计文案仅在不等于 100% 时以告警形式提示；引用计数仅在 `plan_count > 0` 时展示。

## 9. 测试与门禁

- 为每个改动点补 / 改 Vitest 与 Go 单测，覆盖：分页/滚动加载、10 行视口、候选层关闭与已选剔除、候选历史状态展示（已同步/同步中/失败/未同步）、allocation bar 排序与聚合、收益曲线渲染与年度收益倒序、毫秒日期格式化、场景卡片权限与文案、向导宽度与侧栏 sticky。
- 交付门禁：`go test ./...`、`make web-lint`、`make web-test`、`make web-build` 均通过。
