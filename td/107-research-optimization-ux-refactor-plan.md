# 组合研究自动调优体验调整方案

## 背景

当前组合研究页已经支持普通回测和自动寻找最优组合，但交互上仍把“权重合计不为 100%”作为普通回测阻断条件呈现在数据状态区。引入自动调优后，权重不足 100% 不应再被表现成整体异常，而应引导用户进入“寻找最优组合”流程。

同时，自动调优结果页需要让用户理解关键指标，并能把某一组调优结果直接应用回当前基金组合。筛选器入口在当前组合研究主流程里价值较低，需要移除相关入口和无用代码。

## 目标

1. 当组合权重不为 100% 时，不再把该状态作为数据状态阻断提示展示；普通回测按钮保持禁用并通过 hover 说明原因。
2. `运行回测` 与 `寻找最优组合` 保持为两个独立按钮，各自根据自身准入条件决定可用状态。
3. 自动调优结果页补充指标解释，并支持一键应用某条调优结果到当前组合。
4. 应用调优结果后，组合项的启用、锁定、权重状态符合调优结果，并跳转回组合页面。
5. 移除“资产筛选器”“从筛选器添加”入口及其相关前后端代码链路，而不是只隐藏按钮。
6. 移除 `资产与权重` 资产列表的拖拽排序能力，降低无意义交互复杂度。
7. 固定 `添加资产` 弹窗内容区高度，避免搜索结果数量变化导致弹窗高度跳动。

## 非目标

1. 不修改自动调优搜索算法、候选生成规则、评分逻辑。
2. 不删除行情数据、研究资产搜索能力本身；资产列表 API 是 `添加资产` 的依赖，必须保留底层查询能力。
3. 不引入多套应用调优结果方案；本方案采用“结果行内应用按钮 + 确认后批量保存”的唯一方案。

## 现状问题

### 权重不足时的阻断展示

当前 `DataStatusPanel` 直接展示 `readiness.blocking_reasons`，其中包含 `weight_sum_invalid`。因此权重合计不为 100% 时，数据状态区显示：

- 标题徽标：`N 项阻断`
- 区块标题：`阻断条件（无法运行回测）`
- 明细：`权重合计不是 100%`

这个信息对普通回测是正确的，但对自动调优场景不友好。权重为 0 或权重不足 100% 本身正是自动调优的输入状态。

### 回测与调优入口缺少清晰的独立准入关系

当前 `BacktestPanel` 同时展示：

- `运行回测`
- `寻找最优组合`

这两个按钮实际代表不同能力，应该保持并列，而不是互相替换：

- `运行回测` 要求启用权重合计为 100%，且普通回测数据 readiness 通过。
- `寻找最优组合` 要求启用且可被调优的资产数不少于 2，且自动调优 readiness 通过。

当前问题不在于两个按钮同时存在，而在于禁用状态和禁用原因不够清晰。用户需要在鼠标移动到禁用按钮时看到该按钮当前不可用的具体原因。

### 自动调优结果不可直接落地

自动调优结果页只展示结果，用户无法把某条结果直接应用到组合列表。用户需要手工回到组合页逐项修改权重、启用和锁定状态，容易出错。

### 筛选器入口噪音

组合研究列表页和组合详情页存在：

- `资产筛选器`
- `从筛选器添加`

当前主流程已经以组合编辑和自动调优为核心，这两个入口意义不大，且增加页面复杂度。

## 方案设计

### 1. 数据状态区过滤权重合计阻断

修改 `web/components/research/DataStatusPanel.tsx`。

新增内部派生数据：

```ts
const rawBlocking = readiness?.blocking_reasons ?? [];
const visibleBlocking = rawBlocking.filter((issue) => issue.reason !== "weight_sum_invalid");
```

展示逻辑改为：

- 数据状态区只展示 `visibleBlocking`。
- `weight_sum_invalid` 不再出现在“阻断条件（无法运行回测）”区块。
- readiness 徽标基于 `visibleBlocking.length`：
  - `readinessLoading`: `检查中…`
  - 无 readiness: `—`
  - `visibleBlocking.length === 0`: `数据就绪`
  - `visibleBlocking.length > 0`: `{visibleBlocking.length} 项阻断`

注意：这里仅改变前端展示，不改变后端普通回测 readiness 判定。普通回测仍必须要求启用权重合计为 100%。

验收：

- 权重合计 0%、50%、80% 时，数据状态区不显示 `weight_sum_invalid`。
- 如果同时存在缺历史、缺汇率等问题，仍显示这些真实数据阻断。
- 权重合计不为 100% 且没有其他数据问题时，数据状态徽标显示 `数据就绪`。

### 2. 回测面板保留两个独立按钮

修改 `web/components/research/BacktestPanel.tsx`。

#### 2.1 按钮准入条件

`运行回测` 按钮准入：

- `readiness.ready === true`
- 等价约束：普通 readiness 中不存在任何 blocking reason，包括 `weight_sum_invalid`

`寻找最优组合` 按钮准入：

- `optReadiness.ready === true`
- `optReadiness.tunable_count >= 2`
- 启用资产数量不超过一期上限 10 个
- 锁定权重合计不超过 100%
- 候选数量未超过上限
- 相关历史和汇率数据满足调优 readiness

说明：

- 两个按钮互不隐藏。
- 权重合计不为 100% 只影响 `运行回测`，不影响 `寻找最优组合`。
- 可被调优资产数少于 2 时，`寻找最优组合` 禁用，即使普通回测可用。
- 只有 1 个未锁定可调资产时，自动调优没有搜索意义，应提示用户至少保留 2 个可调资产。

#### 2.2 按钮展示

按钮区域保持两个按钮并列展示：

- `运行回测`
  - 可用时点击执行普通回测。
  - 禁用时仍可 hover。
  - hover 展示 `runDisabledReason(readiness)`。
- `寻找最优组合`
  - 可用时点击打开 `OptimizationConfigDialog`。
  - 禁用时仍可 hover。
  - hover 展示 `optimizationDisabledReason(readiness, optReadiness)`。

按钮宽度继续保持一致，例如 `className="w-32"`。

#### 2.3 禁用按钮 hover 展示原因

当前 `Button` 使用原生 `disabled` 时，禁用按钮通常无法触发 hover。需要增加一个轻量包装：

```ts
<span className="inline-flex" title={disabledReason ?? undefined}>
  <Button
    disabled={disabledReason !== null}
    className={disabledReason ? "pointer-events-none w-32" : "w-32"}
  >
    运行回测
  </Button>
</span>
```

为了避免 disabled button 吞掉鼠标事件，包装元素需要保持 `inline-flex`，禁用时按钮加 `pointer-events-none`，按钮本身仍使用 `disabled` 保持键盘和语义上的不可点击状态。

#### 2.4 禁用原因文案

`运行回测`：

- readiness 未返回：`正在检查数据就绪状态…`
- 无启用资产：`集合没有启用的资产`
- 权重合计不为 100%：`当前权重合计 xx%，未达到 100%，仅允许执行最优组合查找或调整权重`
- 缺历史：`存在缺历史资产，请先「更新组合数据」`
- 同步中：`数据同步任务进行中，完成后可运行`
- 缺汇率：`汇率数据缺失或存在缺口，请同步汇率`
- 共同区间不足：沿用现有区间提示

`寻找最优组合`：

- readiness 未返回：`正在检查调优就绪状态…`
- 无启用资产：`集合没有启用的资产`
- 可调资产数少于 2：`至少需要 2 个启用且未锁定的资产才能寻找最优组合`
- 启用资产超过 10 个：`启用资产 xx 个超过上限 10 个`
- 锁定权重超过 100%：`锁定权重合计 xx% 超过 100%`
- 候选数量超限：`候选组合 xx 个超过上限，请增大步长或减少资产`
- 缺历史或同步中：沿用现有调优 readiness 提示
- 缺汇率：沿用现有调优 readiness 提示

验收：

- 权重合计 0%、50%、80% 时，`运行回测` 禁用，hover 显示权重不足原因。
- 权重合计 100% 且数据就绪时，`运行回测` 可用。
- 启用且未锁定的可调资产数大于等于 2 且调优 readiness 通过时，`寻找最优组合` 可用。
- 可调资产数少于 2 时，`寻找最优组合` 禁用，hover 显示至少需要 2 个可调资产。
- 两个按钮始终同时展示，且宽度一致。
- 禁用按钮不可点击，且 hover 到按钮视觉区域时能看到对应原因。
- `寻找最优组合` 能沿用现有调优配置弹窗。

### 3. 自动调优结果页增加指标解释

修改 `web/app/research/collections/[id]/optimizations/[optimizationId]/page.tsx`。

使用现有 `web/components/ui/Tooltip.tsx` 或 `InlineTooltip.tsx`。表头改为图标式帮助按钮，不新增大段说明文字。

表头：

- `夏普比率 ?`
  - Tooltip：`衡量单位波动风险带来的超额收益，数值越高代表风险调整后收益越好。`
- `卡玛比率 ?`
  - Tooltip：`衡量年化收益相对最大回撤的表现，数值越高代表在控制回撤下的收益更好。`

实现建议：

```tsx
<MetricHeader
  label="夏普比率"
  help="衡量单位波动风险带来的超额收益，数值越高代表风险调整后收益越好。"
/>
```

`MetricHeader` 使用 `Tooltip` 包裹一个 `?` 小圆形按钮，按钮应有 `aria-label`，避免纯视觉符号不可访问。

验收：

- 鼠标移动到 `?` 后能看到解释。
- 表头在桌面和移动横向滚动表格中不换行错位。
- 不再出现英文 `Sharpe`、`Calmar`。

### 4. 自动调优结果支持一键应用到组合

#### 4.1 前端交互

在结果表格增加最右侧列：`操作`。

每一条结果行展示按钮：

- `应用`

点击后打开确认弹窗，内容包含：

- 目标组合名称
- 本次将启用并锁定的资产数量
- 本次将取消启用的资产数量
- 权重合计预览
- 本次调优结果对应的回测区间
- 提示：`应用后会覆盖当前组合的启用、锁定、权重和回测区间设置。`

确认按钮：

- `应用到组合`

应用成功后：

- 跳转到 `/research/collections/{collectionId}`
- 在 URL 上带轻量提示参数：`?optimized_applied=1`
- 组合页面读取参数后显示一次性状态提示：`已应用调优结果，相关资产已启用并锁定。`

#### 4.2 保存语义

对当前组合 `detail.items` 和选中调优结果的 `weights` 做映射。

规则：

1. 调优结果中出现且 `weight > 0` 的资产：
   - `enabled: true`
   - `weight: resultWeight`
   - `weight_locked: true`
2. 当前组合中存在、但调优结果未出现或 `weight <= 0` 的资产：
   - `enabled: false`
   - `weight: 0`
   - `weight_locked: false`
3. 如果调优结果里出现当前组合不存在的 `item_id`，阻止应用并提示：`调优结果与当前组合资产不一致，请重新运行调优。`
4. 组合回测区间同步为该次调优任务的区间：
   - `start_policy: "custom_range"`
   - `window_start: optimization.window_start`
   - `window_end: optimization.window_end`

说明：按当前数据模型，调优结果来自该组合快照，因此正常情况下不会出现不存在的 `item_id`。仍需要防御用户在调优完成后删除资产的情况。

应用调优结果必须同步回测区间。原因是调优结果的收益、回撤、夏普比率、卡玛比率都基于该次调优任务冻结的回测窗口。如果只应用权重而保留组合原有区间策略，用户回到组合页再次运行普通回测时可能得到与调优结果不一致的数据。

#### 4.3 API 调用策略

一期采用前端顺序调用现有接口 `updateCollectionItem`，不新增后端批量接口。

原因：

- 当前组合页已经使用 `updateCollectionItem` 顺序保存批量权重。
- 本次变更需要完整实施但范围仍聚焦在研究组合模块。
- 现有接口可同时 patch `enabled`、`weight`、`weight_locked`。

实现：

```ts
for (const item of detail.items) {
  await updateCollectionItem(collectionId, item.id, patch);
}
```

优化：

- 跳过无需变化的 item，减少请求数量。
- 任一请求失败时保留在结果页，并显示错误。
- 成功后 invalidate：
  - `["research", "collection", collectionId]`
  - `["research", "readiness", collectionId]`
  - `["research", "optimization-readiness", collectionId]`
- 成功应用资产项后调用组合更新接口写入该次调优任务的 `custom_range` 回测区间。

#### 4.4 权重标准化

应用前需要校验选中结果中 `weight > 0` 的合计：

- 若合计与 1 的误差小于 `1e-6`，直接应用。
- 若存在浮点尾差，则将尾差吸收到最后一个正权重资产，保证组合权重精确等于 100%。
- 若合计明显不等于 1，阻止应用并提示结果异常。

#### 4.5 与锁定状态的关系

应用调优结果后，所有 `weight > 0` 的资产自动锁定。这样用户回到组合页后：

- 结果组合可以直接运行普通回测。
- 后续若再执行自动调优，默认会保持这组结果不被调整，除非用户手动取消锁定。

这是符合用户要求的明确落地行为。

验收：

- 点击任一结果行 `应用` 后弹出确认弹窗。
- 确认后跳转回组合页面。
- 结果中 `weight > 0` 的资产被启用、锁定，权重等于结果。
- 结果中 `weight === 0` 或未出现在结果中的资产被取消启用、取消锁定，权重为 0。
- 组合回测区间被更新为该次调优任务的 `window_start ~ window_end`，`start_policy` 为 `custom_range`。
- 回到组合页后权重合计为 100%，`运行回测` 按钮可用。
- 回到组合页后直接运行普通回测，回测使用与调优结果一致的时间窗口。
- 保存失败时不跳转，并展示错误信息。

### 5. 移除筛选器入口和相关代码链路

本次不是简单隐藏按钮，而是删除“资产筛选器”作为产品功能入口及其前端功能链路。底层研究资产查询能力是否删除，以是否仍被其他功能复用为准；当前组合内 `添加资产` 仍需要资产搜索能力，因此不能直接删除所有 asset search 后端代码。

#### 5.1 组合列表页入口

修改 `web/app/research/page.tsx`：

- 移除 `secondaryActions` 中的 `资产筛选器` 按钮。
- 删除对应 `data-testid="screener-entry"` 的测试断言。
- 修改页面描述，移除“从资产筛选器挑选候选资产创建集合”表述。

保留：

- `新建集合`
- `从计划复制`
- `导入 JSON`

#### 5.2 组合详情页入口

修改 `web/app/research/collections/[id]/page.tsx`：

- 移除 `从筛选器添加` 按钮。
- 移除 `data-testid="screener-add-entry"` 的测试断言。
- 保留 `router`，因为 `运行记录` 按钮仍使用 `router.push`。

保留：

- `添加资产`，由组合内的 `WeightEditor` 调起 `AddAssetDialog`。
- `复制到计划`
- `导出 JSON`
- `运行记录`

#### 5.3 保证“资产与权重”的添加资产逻辑不受影响

`资产与权重`里的 `添加资产` 逻辑必须保留完整。当前依赖链是：

- `web/app/research/collections/[id]/page.tsx`
  - 渲染 `AddAssetDialog`
  - `onAdd` 调用 `addCollectionItem`
- `web/components/research/AddAssetDialog.tsx`
  - 使用 `listResearchAssets({ q: query, limit: 20 })` 搜索资产
  - 返回 `ResearchAssetView`
- `web/lib/api/research.ts`
  - `listResearchAssets`
  - `ResearchAssetView`
  - 保留 `ResearchAssetListParams` 中 `q`、`limit` 等添加资产所需参数
- 后端
  - 保留 `GET /api/v1/research/assets`
  - 保留 `ResearchService.ListResearchAssets`
  - 保留 repository 的资产搜索能力

因此删除筛选器时不能删除：

- `AddAssetDialog.tsx`
- `listResearchAssets`
- `ResearchAssetView`
- `GET /api/v1/research/assets`
- 后端资产搜索服务、仓储和相关基础测试

可删除的是筛选器页面专用能力：

- 多条件筛选面板
- 候选池
- 候选比较弹窗
- 保存筛选器 saved filters
- `/research/screener?collection=...` 添加到目标集合的路径

实施时必须先用 `rg` 校验引用边界。删除完成后需要补充/保留测试，证明 `AddAssetDialog` 仍能搜索并添加资产。

#### 5.4 删除筛选器页面路由

删除：

- `web/app/research/screener/page.tsx`
- `web/app/research/screener/page.test.tsx`

删除后 `/research/screener` 不再作为可访问页面存在。Next.js 会自然返回 not-found。

同时清理所有指向 `/research/screener` 的测试或导航断言，例如：

- `web/app/research/page.test.tsx` 中 `screener-entry` 相关断言。
- `web/components/layout/AppShell.test.tsx` 中把 `/research/screener` 作为有效导航路径的用例。
- 其他通过 `href="/research/screener"` 或 `?collection=` 进入筛选器的断言。

#### 5.5 删除筛选器专属组件和工具

删除仅被筛选器页面使用的组件：

- `web/components/research/ScreenerFilterPanel.tsx`
- `web/components/research/CandidatePoolPanel.tsx`
- `web/components/research/CandidatePoolPanel.test.tsx`
- `web/components/research/CandidateCompareDialog.tsx`
- `web/components/research/CandidateCompareDialog.test.tsx`

删除仅被这些组件使用的前端工具：

- `web/lib/research/screener-filters.ts`

删除前需要用 `rg` 确认引用边界。确认仅筛选器页面引用后，删除整个文件；若发现非筛选器页面引用，先将被复用的通用函数迁移到非筛选器命名的工具文件，再删除筛选器专用文件。

#### 5.6 清理前端 API 客户端中的筛选器专用接口

修改 `web/lib/api/research.ts`：

- 删除 saved filters 相关类型和函数：
  - `ResearchSavedFilter`
  - `ResearchSavedFilterInput`
  - `listSavedFilters`
  - `createSavedFilter`
  - `updateSavedFilter`
  - `deleteSavedFilter`
- `ResearchAssetListParams` 不能整体删除，因为 `AddAssetDialog` 和 `CollectionParamsForm` 仍通过 `listResearchAssets({ q, limit })` 搜索资产。只删除筛选器页面专属、且不再有调用方的字段使用。
- `listResearchAssets` 必须保留，因为组合内 `AddAssetDialog` 仍依赖资产搜索/添加能力。
- `ResearchAssetView` 需要保留，因为多个研究组件仍使用资产视图。

#### 5.7 清理后端 saved filters 接口与代码

saved filters 只服务资产筛选器，删除筛选器功能后应同步删除：

- `internal/api/research_handlers.go`
  - 删除 `/saved-filters` 路由注册
  - 删除 `listResearchSavedFilters`
  - 删除 `createResearchSavedFilter`
  - 删除 `updateResearchSavedFilter`
  - 删除 `deleteResearchSavedFilter`
- `internal/service/research_service.go`
  - 删除 `ResearchSavedFilterInput`
  - 删除 `CreateSavedFilter`
  - 删除 `ListSavedFilters`
  - 删除 `UpdateSavedFilter`
  - 删除 `DeleteSavedFilter`
- `internal/repository/research.go`
  - 删除 saved filter CRUD 方法和 `ResearchSavedFilter` 模型。
- 测试清理：
  - 删除 `TestResearchSavedFilterCRUD`
  - 删除或改写 `TestResearchAPISavedFiltersAndSyncHistory` 中 saved filters 部分，保留 sync history 覆盖。

数据库迁移中已存在的 `research_saved_filters` 表不做回滚迁移。原因是 SQLite 本地迁移已发布，删除历史表会引入不必要的数据迁移风险；代码层不再使用即可。

#### 5.8 保留的底层能力

以下能力不能因为移除资产筛选器而删除：

- `listResearchAssets` API 及后端资产搜索能力：组合内 `AddAssetDialog` 仍需要搜索资产。
- 研究资产 metrics 的后台计算：资产详情、资产选择和其他研究展示仍可能使用。
- 自动调优中的 `candidate_count` 概念：这是优化候选组合数量，不属于资产筛选器功能。

验收：

- 组合研究列表页不再出现 `资产筛选器`。
- 组合详情页不再出现 `从筛选器添加`。
- `/research/screener` 页面文件和测试被删除。
- 所有 `/research/screener` 链接和相关导航测试断言被删除。
- 筛选器专属组件、候选池组件、候选比较组件被删除。
- saved filters 前后端接口和测试被删除。
- 组合内仍可通过 `资产与权重` 的 `添加资产` 搜索并添加基金。
- `listResearchAssets` 仍可被 `AddAssetDialog` 正常使用。

代码级验收：

- `rg -n "href=\"/research/screener|research/screener\\?collection|screener-entry|screener-add-entry" web` 无结果。
- `rg -n "ScreenerFilterPanel|CandidatePoolPanel|CandidateCompareDialog|screener-filters" web` 无结果。
- `rg -n "saved-filters|SavedFilter|ResearchSavedFilter" web internal` 无业务代码结果；迁移文件或历史文档可保留。
- `rg -n "listResearchAssets\\(" web/components/research/AddAssetDialog.tsx web/components/research/CollectionParamsForm.tsx` 仍能看到调用。

### 6. 资产与权重列表取消拖拽排序

当前 `资产与权重` 表格行可拖动排序，但该排序对回测、自动调优和资产配置没有实质价值，还会引入误触和维护成本。本次调整将资产列表改为不可拖动。

#### 6.1 前端组件调整

修改 `web/components/research/WeightEditor.tsx`：

- 从 `WeightEditorProps` 删除 `onReorder`。
- 删除 `dragId` state。
- 删除 `handleDrop`。
- 删除 `<tr>` 上的：
  - `draggable`
  - `onDragStart`
  - `onDragOver`
  - `onDrop`
  - 拖拽中 `opacity-50` 样式
- 删除资产名称前的拖拽手柄 `⠿` 和 `cursor-grab`。
- 表格仍按后端返回的 `detail.items` 顺序渲染。

#### 6.2 页面逻辑调整

修改 `web/app/research/collections/[id]/page.tsx`：

- 删除 `reorderMutation`。
- 删除 `itemsPending` 中的 `reorderMutation.isPending`。
- 调用 `WeightEditor` 时删除 `onReorder`。

说明：

- 后端 `sort_order` 字段暂不删除，避免迁移和接口兼容风险。
- `ResearchItemUpdate.sort_order` 可暂时保留，除非确认没有其他调用方。
- 只移除当前 UI 的拖拽排序能力。

#### 6.3 测试调整

修改 `web/components/research/WeightEditor.test.tsx`：

- 删除 `reorders by drag and drop` 测试。
- 删除默认 props 中的 `onReorder`。
- 新增断言：资产行不带 `draggable` 属性，且不显示拖拽手柄。

验收：

- `资产与权重` 表格行不可拖动。
- 页面不再触发任何 `sort_order` 更新请求。
- 移除拖拽后，启用、锁定、权重输入、删除、添加资产逻辑不受影响。

### 7. 添加资产弹窗固定高度

当前 `AddAssetDialog` 的整体高度会随搜索结果数量变化而变化：

- 加载中只有一行 `LoadingState`，弹窗较矮。
- 无匹配结果时只有空状态，弹窗较矮。
- 有多条资产时列表高度增加。

这会导致用户在输入搜索词时弹窗高度跳动。本次调整将弹窗内容区固定高度，搜索框固定在顶部，结果区在固定高度内滚动或居中展示状态。

#### 7.1 组件调整

修改 `web/components/research/AddAssetDialog.tsx`：

- `Dialog` 继续使用 `className="max-w-2xl"`。
- 弹窗 body 内部改为固定高度布局，例如：

```tsx
<div className="flex h-[32rem] flex-col gap-3">
  <input ... />
  <div className="min-h-0 flex-1 overflow-y-auto">
    ...
  </div>
</div>
```

- 搜索框不参与滚动。
- 结果列表使用 `h-full overflow-y-auto` 或父级滚动，不再使用会随内容变化的 `max-h-96` 作为唯一高度约束。
- 加载中、无结果状态放在结果区内垂直居中，例如 `flex h-full items-center justify-center`。
- 移动端需要避免超过视口，可用响应式约束：
  - `h-[min(32rem,70vh)]`
  - 或 `max-h-[70vh]`

推荐实现：

```tsx
<div className="flex h-[min(32rem,70vh)] flex-col gap-3">
  <input ... />
  <div className="min-h-0 flex-1 overflow-y-auto">
    {searchQuery.isLoading ? (
      <div className="flex h-full items-center justify-center">
        <LoadingState label="搜索中…" />
      </div>
    ) : assets.length === 0 ? (
      <p className="flex h-full items-center justify-center text-sm text-ink-muted">
        无匹配资产。
      </p>
    ) : (
      <ul className="space-y-1">...</ul>
    )}
  </div>
</div>
```

#### 7.2 测试调整

补充或更新 `AddAssetDialog` 相关测试：

- 加载中、无结果、有结果三种状态都存在同一个固定高度结果容器。
- 固定高度结果容器增加稳定测试标识，例如 `data-testid="add-asset-results"`。
- 搜索框仍可输入。
- 有结果时列表在结果区内渲染，点击 `加入` 行为不变。

验收：

- 点击 `添加资产` 后，弹窗高度稳定。
- 搜索结果从 0 条变为多条时，弹窗外框高度不跳动。
- 结果过多时仅结果区内部滚动。
- 搜索框始终固定在结果区上方。
- 测试不依赖 jsdom 真实布局测量，而是断言固定高度结果容器存在、使用固定高度/弹性布局 class，且加载/空/列表内容都渲染在该容器内。

## 需要修改的文件

前端：

- `web/components/research/DataStatusPanel.tsx`
- `web/components/research/DataStatusPanel.test.tsx`
- `web/components/research/BacktestPanel.tsx`
- `web/components/research/BacktestPanel.test.tsx`
- `web/app/research/collections/[id]/optimizations/[optimizationId]/page.tsx`
- 新增或就地补充优化结果页测试文件
- `web/app/research/collections/[id]/page.tsx`
- `web/app/research/page.tsx`
- 删除 `web/app/research/screener/page.tsx`
- 删除 `web/app/research/screener/page.test.tsx`
- 删除筛选器专属组件和测试
- 清理 `web/lib/api/research.ts` 中 saved filters 客户端
- 清理后端 saved filters handler/service/repository/test
- `web/components/research/WeightEditor.tsx`
- `web/components/research/WeightEditor.test.tsx`
- `web/components/research/AddAssetDialog.tsx`
- 新增 `web/components/research/AddAssetDialog.test.tsx`
- `web/app/research/page.test.tsx`
- `web/components/layout/AppShell.test.tsx`
- 其他被 `rg "research/screener|screener-entry|screener-add-entry"` 命中的测试

API：

- 复用 `updateCollectionItem`
- 不新增后端接口

## 测试计划

### 单元测试

1. `DataStatusPanel`
   - `weight_sum_invalid` 不展示在阻断区。
   - 混合 `weight_sum_invalid + history_missing` 时只展示 `history_missing`。
   - 仅 `weight_sum_invalid` 时徽标显示数据就绪。

2. `BacktestPanel`
   - 权重合计 80% 时，`运行回测` 禁用且 hover 展示权重不足原因。
   - 权重合计 100%、ready 时，`运行回测` 可用。
   - 启用且未锁定的可调资产数大于等于 2 时，`寻找最优组合` 可用。
   - 可调资产数少于 2 时，`寻找最优组合` 禁用且 hover 展示至少需要 2 个可调资产。
   - 调优按钮点击后打开配置弹窗。

3. 自动调优结果页
   - 表头显示 `夏普比率`、`卡玛比率` 和 `?`。
   - Tooltip 内容正确。
   - 点击 `应用` 后展示确认弹窗。
   - 确认后按规则调用 `updateCollectionItem`。
   - 成功后跳转组合页。

4. 入口移除
   - 组合列表页不再渲染 `资产筛选器`。
   - 组合详情页不再渲染 `从筛选器添加`。
   - `/research/screener` 对应测试删除或不再存在。
   - saved filters API 测试删除或改写为不包含 saved filters 断言。

5. `AddAssetDialog`
   - 打开 `添加资产` 弹窗时仍调用 `listResearchAssets({ q, limit })`。
   - 搜索结果仍能点击 `加入`。
   - 已存在资产仍显示 `已加入` 且不可重复添加。
   - 加载、空结果、有结果时弹窗高度保持稳定。
   - 结果过多时仅结果区滚动。

6. `WeightEditor`
   - 资产行不可拖动。
   - 不再渲染拖拽手柄。
   - 启用、锁定、权重输入、删除仍可用。

### 集成验证

执行：

```bash
cd web && npm run lint
cd web && npm run test:ci
cd web && npm run build
go test ./...
```

## 风险与处理

1. 顺序调用 `updateCollectionItem` 不是事务性的。
   - 处理：跳过无变化项，失败时留在结果页并展示错误；后续如需要强一致性，再新增后端批量接口。

2. 用户在调优完成后修改了组合资产。
   - 处理：应用前校验结果中的 `item_id` 都存在于当前集合；不一致则阻止应用并提示重新运行调优。

3. 隐藏 `weight_sum_invalid` 可能让用户误以为普通回测可运行。
   - 处理：`运行回测` 按钮保持展示但禁用，hover 明确说明 `当前权重合计 xx%，未达到 100%，仅允许执行最优组合查找或调整权重`。

## 完成标准

1. `运行回测` 与 `寻找最优组合` 两个按钮始终独立展示，宽度一致。
2. 权重等于 100% 且普通 readiness 通过时，`运行回测` 可用；否则禁用并可 hover 查看原因。
3. 启用且未锁定的可调资产数大于等于 2 且调优 readiness 通过时，`寻找最优组合` 可用；否则禁用并可 hover 查看原因。
4. 数据状态区不再因 `weight_sum_invalid` 展示阻断区。
5. 自动调优结果页指标解释完整，结果可一键应用回组合。
6. 应用结果后组合权重合计为 100%，正权重资产启用并锁定，非结果资产取消启用。
7. 组合研究列表和详情页不再出现筛选器入口，筛选器页面、筛选器专属组件、saved filters 前后端代码链路已删除。
8. 组合内 `资产与权重` 的 `添加资产` 能力仍可用，并有测试覆盖。
9. `资产与权重` 表格不可拖动，且无 `sort_order` 更新请求由拖拽触发。
10. `添加资产` 弹窗高度固定，搜索结果变化不改变弹窗外框高度。
11. lint、前端测试、前端 build、后端测试全部通过。
