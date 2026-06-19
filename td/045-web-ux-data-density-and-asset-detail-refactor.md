# Web UX 数据密度与资产详情改造方案

## 背景

本方案覆盖新建计划向导、建立持仓资产选择、组合预览大类配置、资产资料库详情页和工作台左侧导航的下一轮改造。目标不是只修单点样式，而是把“宽度不足导致信息堆积”“前端一次性全量加载资产”“图表缺少可解释明细”“资产详情数据展示不够聚合”这些问题收敛为一组可验收的 UI / API 改造。

## 当前状态

- 新建计划向导根容器在 `web/app/plans/new/page.tsx` 使用 `max-w-2xl`，确认组合页虽然有横向滚动，但可视宽度过窄，表格文字仍然挤压。
- 建立持仓组件 `web/components/plans/AssetClassHoldingPicker.tsx` 当前依赖 `listInstruments()` 一次性加载全量资产，再在前端本地过滤；未支持默认展示最近资产、分页和滚动加载。
- 组合预览 `web/app/plans/[id]/overview/page.tsx` 只展示金额，不展示量级单位；`allocation_bars` 当前只有大类目标/当前比例，无法在 tooltip 中展示资产明细与目标金额。
- `internal/service/dashboard_service.go` 当前按大类字符串排序，展示顺序不是业务要求的“权益、债券、现金/其他”。
- 资产详情页 `web/app/assets/[id]/page.tsx` 年度收益直接按接口顺序渲染，入选年份逐年展开，刷新/强刷/删除按钮位于页面中段。
- 资产详情页已有 `market_data_points` 日度数据存储，但前端详情接口没有返回可渲染收益曲线的时间序列。
- 工作台 `web/components/layout/AppShell.tsx` 左侧栏在整体 flex 布局内，缺少桌面端固定视口高度与独立滚动约束。
- FIRE 计划列表页 `web/app/page.tsx` 的 `更新于` 字段将 `plan.updated_at` 当作秒级时间戳处理，实际后端 `PlanRepo` 使用 `time.Now().UnixMilli()` 写入毫秒级时间戳，导致日期展示异常。
- 场景配置页 `web/app/scenarios/page.tsx` 的卡片操作区信息层级不清：`内置` 标签占据右上角操作位，编辑/复制/删除在底部以文字按钮展示；内置场景仍可进入编辑；未被引用时仍显示 `0 个计划使用`；正常权重也显示 `合计 100%，通过` 这类冗余文案。

## 改造原则

- 页面容器优先解决信息密度，不依赖表格横向滚动兜底。
- 下拉资产选择必须从“前端全量过滤”改为“后端分页查询”，否则资产库增长后会继续退化。
- 图表 tooltip 只展示服务端已聚合好的权威数据，避免前端重复推导目标金额导致和调仓服务口径不一致。
- 资产详情页将操作区上移，数据区聚合展示；年度、区间、曲线数据各自承担不同用途。
- 本轮不引入多套候选方案，只采用下面这一套可实施方案。

## 实施方案

### 1. 新建计划向导与页面容器宽度

- 将 `web/app/plans/new/page.tsx` 根容器从 `max-w-2xl` 调整为响应式宽容器，例如 `max-w-5xl`，确认组合步骤使用更宽布局承载持仓、目标金额和校验信息。
- 对向导内部步骤做差异化宽度：基础信息、目标配置保持表单舒适宽度；建立持仓、确认组合使用宽内容区。实现上可以保留同一个外层容器，再让表单卡片使用 `max-w-2xl`，数据表/确认区使用 `w-full`。
- 复核现有高密度页面容器：计划总览、持仓预览、资产变更、资产详情。原则上全局 `AppShell` 已提供 `max-w-[1440px]`，具体页面不应再无理由限制在窄宽度；资产详情页可从 `max-w-4xl` 调整为 `max-w-6xl`，并用 grid 分区控制可读性。

验收逻辑：

- 在桌面宽度 1280px 下，新建计划确认组合页无需依赖横向滚动即可看清主要列。
- 基础表单页不被拉伸成过长单行输入，表单可读性保持稳定。
- 移动端仍允许表格横向滚动，不出现页面整体溢出。

### 2. 建立持仓资产选择分页

后端新增资产库分页查询能力，替代当前全量列表：

- 扩展 `GET /api/v1/instruments` 查询参数：`limit`、`offset` 或 `cursor`、`q`、`asset_class`、`region`、`status`、`simulation_eligible`、`exclude_ids`、`sort`。
- 默认排序使用 `created_at DESC, id DESC`，满足“点击输入框默认按时间倒序列举前 10 个资产”。
- 返回结构从仅 `{ instruments }` 扩展为 `{ instruments, next_cursor, total? }`；为了兼容现有页面，不传分页参数时可继续返回现有全量结构，或统一迁移全部调用点到分页结构。
- Repository 层新增 `InstrumentRepo.Search(ctx, InstrumentSearchOptions)`，避免在 Service 层拿全量后过滤。

前端改造 `AssetClassHoldingPicker`：

- 输入框获得焦点且搜索词为空时，请求第一页最近 10 条资产。
- 下拉容器高度固定为 10 条资产行高，例如 `max-h-[var(--asset-picker-page-height)]`，实际行高统一，避免分页后高度跳动。
- 下拉滚动到底部时使用 `IntersectionObserver` 或滚动阈值触发下一页，请求期间显示一行“加载中”状态但不改变整体高度。
- 搜索词变化后重置 cursor，继续按后端条件分页；已选资产通过 `exclude_ids` 排除，资产大类/地区仍由当前持仓分组约束。
- 外部 AKShare 解析只在本地资产分页结果没有可用命中、且用户输入看起来是代码时触发，避免焦点默认列表触发外部解析。

验收逻辑：

- 点击代码输入框且不输入内容时，下拉显示最近创建的 10 个可选资产。
- 下拉滚动到底部后自动追加下一页，容器高度仍等于 10 行资产高度。
- 输入关键词后只返回匹配资产，并支持继续滚动加载。
- 已选资产不会重复出现在候选列表。
- 资产库超过 1000 条时，打开建立持仓页面不再一次性加载全部资产。

### 3. 组合预览金额单位与大类配置 tooltip

组合预览金额展示：

- 在 `计划基准规模` 与 `已投资金` 金额后显示量级单位，建议统一由前端工具函数根据金额自动格式化为 `元 / 万元 / 亿元`，例如 `¥1,234.56 万元`，避免只追加静态单位造成小额资产误导。
- 工具函数应复用在 Dashboard metric card 和 tooltip 金额展示中，确保金额口径一致。

Dashboard API 扩展：

- 扩展 `DashboardAllocationBar`，新增大类金额与明细字段：

```go
type DashboardAllocationBar struct {
    AssetClass          string                         `json:"asset_class"`
    TargetWeight        float64                        `json:"target_weight"`
    CurrentWeight       float64                        `json:"current_weight"`
    CurrentAmountMinor  int64                          `json:"current_amount_minor"`
    TargetAmountMinor   int64                          `json:"target_amount_minor"`
    Holdings            []DashboardAllocationHolding   `json:"holdings"`
}

type DashboardAllocationHolding struct {
    InstrumentName      string  `json:"instrument_name"`
    InstrumentCode      string  `json:"instrument_code"`
    CurrentAmountMinor  int64   `json:"current_amount_minor"`
    TargetAmountMinor   int64   `json:"target_amount_minor"`
    CurrentWeight       float64 `json:"current_weight"`
    TargetWeight        float64 `json:"target_weight"`
}
```

- `buildAllocationBars` 直接基于 `TargetView.Holdings` 聚合，使用 `StructuralCurrentWeight`、`PortfolioTargetWeight`、`CurrentAmountMinor`、`TargetAmountMinor`，不要在前端重新计算。
- 大类排序使用固定业务顺序：`equity`、`bond`、`cash`；未知大类排在最后。

前端图表改造：

- `AllocationBarChart` 类型同步新增金额和 holdings 明细。
- tooltip 展示大类层：目标比例、当前比例、目标金额、当前已投。
- tooltip 展示资产层：资产名称、编号、当前已投金额、目标金额；资产数过多时最多展示前 8 个，并提示剩余数量，避免 tooltip 超高。
- 横轴展示顺序固定为权益、债券、现金/其他。

验收逻辑：

- 大类配置横轴顺序始终为权益、债券、现金/其他。
- 鼠标悬停权益/债券/现金柱时，tooltip 同时展示比例、金额和该大类下资产明细。
- tooltip 中每个资产的目标金额与持仓预览/调仓服务中的目标金额一致。
- 空持仓或 0 金额资产不导致 tooltip 报错，显示为 `0` 或 `—`。

### 4. 资产详情年度收益倒序

- `web/app/assets/[id]/page.tsx` 中对 `data.annual_returns` 做派生排序：按 `year DESC` 展示，不修改原始响应对象。
- 如果后端后续也统一倒序返回，前端仍保留排序，作为展示层兜底。

验收逻辑：

- 年度收益表从最新年份到最早年份展示。
- `in_simulation`、完整性和统计区间仍与对应年份行正确匹配。

### 5. 工作台左侧菜单固定

- 桌面端 `AppShell` 将 `aside` 调整为 `sticky top-0 h-screen overflow-y-auto`，并保持右侧 `main` 自己随页面滚动。
- 移动端顶部导航不改变，仍在页面顶部参与文档流。
- 若后续左侧导航项增多，侧栏内部独立滚动，不带动右侧内容位置。

验收逻辑：

- 桌面端滚动任意长页面时，左侧 Fireman 菜单始终停留在视口内。
- 侧栏高度小于内容时，右侧页面滚动不影响左侧菜单。
- 移动端无固定左侧栏，不出现遮挡内容的问题。

### 6. 资产资料库详情页重排与收益曲线

页面结构调整：

- 顶部区域改为左右布局：左侧返回入口、资产名称、代码和关键元信息；右上角固定展示 `刷新 AKShare 数据`、`强制刷新`、`删除`。
- 当资产为系统资产或被计划引用时，按钮保持当前禁用规则和说明文案，不改变权限语义。
- 页面主体从窄单列调整为宽容器分区：基础信息、模拟窗口、区间收益/收益曲线、年度收益、历史快照。

入选年份展示：

- 新增年份压缩展示函数，将连续年份压缩为范围：`2006-2025`。
- 如果存在不连续年份，展示为多个区间：`2006-2012、2014-2025`。
- 空列表展示 `—`。

收益曲线接口：

- 新增 `GET /api/v1/instruments/:instrument_id/return-series?range=1d|1w|1m|3m|6m|1y|3y|5y|all`。
- 后端从 `market_data_points` 读取对应区间日度点，按时间升序返回归一化累计收益序列。
- 返回结构建议：

```json
{
  "as_of_date": "2026-06-19",
  "range": "3m",
  "point_type": "nav",
  "source_name": "ak.fund_open_fund_info_em",
  "points": [
    { "date": "2026-03-19", "value": 1.0234, "cumulative_return": 0.0 },
    { "date": "2026-06-19", "value": 1.1020, "cumulative_return": 0.0768 }
  ]
}
```

- `1d` 若只有一个交易日数据，则返回最近两个可用交易日之间的收益；如果历史不足，返回 `points: []` 和 `status: "insufficient_history"`，前端展示空态。
- 归一化口径以区间第一条有效点为 0%，后续点计算 `value / first_value - 1`。

前端曲线：

- 资产详情页新增区间切换：`近1天`、`近1周`、`近1月`、`近3月`、`近6月`、`近1年`、`近3年`、`近5年`、`全部`。
- 使用现有图表栈渲染折线图；tooltip 展示日期、累计收益、原始净值/价格。
- 区间切换只重新请求曲线数据，不刷新整个资产详情。
- 曲线放在区间收益卡片附近，使“摘要收益”和“波动过程”相互对应。

验收逻辑：

- 入选年份连续时展示单个范围，不再逐年平铺。
- 页面右上角可直接看到刷新、强刷、删除按钮。
- 切换收益曲线区间时，折线图按选择区间更新。
- 历史不足时展示明确空态，不影响年度收益和模拟窗口展示。
- 曲线 tooltip 的累计收益与首个点归一化口径一致。

### 7. FIRE 计划列表更新时间修正

问题判断：

- `web/app/page.tsx` 中 `formatPlanDate(ts)` 当前使用 `new Date(ts * 1000)`。
- 后端 `internal/repository/plan.go` 在创建和更新计划时使用 `time.Now().UnixMilli()`，`Plan.updated_at` 是毫秒级时间戳。
- 这会把毫秒级时间戳再次乘以 1000，渲染成远超当前年份的异常日期。
- `web/app/page.test.tsx` 当前默认 fixture 使用 `updated_at: 0`，只覆盖了空值展示，未覆盖真实毫秒级时间戳，因此没有暴露该问题。

修复方案：

- 将计划列表页更新时间格式化统一为毫秒级时间戳：`new Date(ts).toLocaleDateString("zh-CN")`。
- 建议把时间戳格式化抽到 `web/lib/format.ts`，新增 `formatDateFromMs(ts?: number | null)` 或类似工具，后续 `created_at` / `updated_at` 页面统一复用，避免不同页面自行判断秒/毫秒。
- 保留 `0`、`null`、`undefined` 返回 `—` 的空值逻辑。
- 不修改后端字段单位；后端已有大量 `UnixMilli()` 写入，前端应对齐毫秒级契约。

验收逻辑：

- 构造 `updated_at: Date.UTC(2026, 5, 19)` 的计划卡片时，`更新于` 显示为 `2026/6/19` 或当前浏览器 `zh-CN` 等价日期，而不是异常未来年份。
- `updated_at: 0` 时仍显示 `—`。
- 首页测试新增毫秒级时间戳用例，确保不会再出现 `ts * 1000` 回归。
- 复核其他页面时间格式化：已按毫秒处理的页面不做乘法改动；若发现同类秒级乘法，再统一迁移到新格式化工具。

### 8. 场景配置卡片与编辑权限调整

卡片布局调整：

- 将 `内置` 标签移动到方案名右侧，作为标题的 inline badge 展示，例如 `均衡  内置`，不再占据卡片右上角操作位，也不做靠右对齐。
- 卡片右上角改为操作区，放置图标按钮：复制、编辑、删除。图标按钮需要保留可访问名称，例如 `aria-label="复制场景"`、`aria-label="编辑场景"`、`aria-label="删除场景"`。
- 底部不再展示文字版 `编辑`、`复制`、`删除` 按钮，减少卡片垂直占用。
- 图标优先使用项目已有图标体系；如果当前没有统一图标组件，则采用轻量 SVG 内联组件，避免引入新的图标依赖。

权限与交互：

- 内置场景不允许用户调整，因此内置卡片不展示编辑按钮，或展示禁用编辑按钮并带 `title="内置场景不可编辑"`；推荐不展示编辑按钮，减少误操作入口。
- 内置场景允许复制为自定义场景，复制后的新场景 `is_builtin=false`，可编辑。
- 非内置场景允许编辑。
- 删除规则保持当前语义：仅非内置且 `plan_count === 0` 时允许删除；被计划引用时不展示删除按钮，或禁用并说明原因。推荐不展示删除按钮，避免用户以为可以删除。

引用文案：

- `plan_count > 0` 时展示 `{plan_count} 个计划使用`。
- `plan_count === 0` 时不展示引用文案，避免出现 `0 个计划使用` 或缺数字的 `个计划使用`。

权重校验文案：

- 卡片上移除正常状态文案 `合计 100%，通过`。
- 当权重合计不为 100% 时才展示异常文案，例如 `权重合计 95%，请调整到 100%`。
- 保存逻辑继续强制 `validatePercentSum` 通过，不允许占比不到 100% 的场景保存；这已经是当前行为，应保留并补测试。

验收逻辑：

- 内置场景标题右侧显示 `内置` badge，右上角不再显示 `内置` 文案。
- 内置场景没有可用编辑入口；点击复制后打开的新场景为非内置，可编辑保存。
- 非内置场景可通过右上角编辑图标进入编辑弹窗。
- `plan_count=0` 的场景不显示“计划使用”文案；`plan_count=2` 时显示 `2 个计划使用`。
- 权重合计正好 100% 的卡片不显示 `合计 100%，通过`；权重异常时才显示错误提示。
- 场景弹窗保存时，权重不到 100% 仍被阻止，并显示校验错误。

## 推荐实施顺序

1. 先做纯前端低风险项：计划列表更新时间修正、场景配置卡片调整、向导宽度、资产详情年度收益倒序、入选年份压缩、按钮上移、侧栏 sticky。
2. 再做 Dashboard API 扩展与大类图 tooltip，因为需要后端结构、前端类型和图表联动。
3. 最后做资产选择分页和收益曲线接口，这两项都涉及接口契约、Repository 查询和前端异步交互，需要单独测试。

## 测试与门禁

- 后端新增/调整单测：资产分页查询排序与过滤、Dashboard allocation bar 明细聚合、return-series 区间归一化和历史不足。
- 前端新增/调整测试：计划列表毫秒级 `updated_at` 展示、场景配置内置不可编辑/引用文案/权重正常态隐藏、向导确认页宽容器 class、资产选择默认加载/滚动分页、资产详情年份压缩和年度倒序、Dashboard tooltip 内容、AppShell 桌面侧栏 sticky。
- 集成门禁：`go test ./...`、`cd web && npm run lint`、`cd web && npm run test:ci`、`cd web && npm run build`。

## 风险与约束

- `GET /api/v1/instruments` 当前被多个页面复用；分页结构迁移要么保持兼容，要么一次性更新所有调用点，避免资产库页面被破坏。
- Dashboard tooltip 明细必须以后端 `TargetView` 为准，不能前端按当前总资产自行估算，否则会和调仓建议出现口径差。
- 收益曲线使用 `market_data_points.value` 时要保留 `point_type`，不同资产可能是净值、复权价或指数点位，前端文案不能统一写成“净值”。
- 侧栏 sticky 只适用于桌面断点，移动端不应引入 fixed 侧栏。
