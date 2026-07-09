# td/107 实施 Review

## 结论

本次实现未达到“文档完整实现且无缺陷”的状态。整体 UI 调整、筛选器移除、添加资产弹窗固定高度、拖动排序移除、双按钮独立可用性等主体功能已落地，但“应用调优结果”存在一个实现越界问题，需要返工后再整理到 `docs`。

## Review 发现

### 1. 应用调优结果会额外覆盖组合回测区间

- 严重程度：中
- 位置：`web/app/research/collections/[id]/optimizations/[optimizationId]/page.tsx:309`

当前 `applyMutation` 在写入调优结果的资产启用、锁定和权重后，还会调用 `updateCollection`，强制把组合的 `start_policy` 改为 `custom_range`，并把 `window_start/window_end` 改成该次调优任务的区间。确认弹窗也提示会覆盖“回测区间”。

td/107 对“应用调优结果”的语义是：将调优结果应用到基金组合列表，调优结果中比例不为 0 的资产自动启用并锁定，未出现或比例为 0 的资产取消启用。文档没有要求应用结果时覆盖组合的回测区间策略。当前实现会把用户原本的区间策略一起改掉，例如从公共交集策略切换为自定义区间，这会影响后续普通回测和数据准入判断，属于超出功能预期的副作用。

#### 修复方案

删除应用调优结果流程中的 `updateCollection` 调用，仅保留 `updateCollectionItem` 对资产项的更新：

- 调优结果权重大于 0 的资产：`enabled=true`、`weight=<调优权重>`、`weight_locked=true`。
- 调优结果中不存在或权重为 0 的资产：`enabled=false`、`weight=0`、`weight_locked=false`。
- 保留现有的 collection/readiness/optimization-readiness 缓存失效和跳转逻辑。
- 确认弹窗中移除“回测区间”预览，以及“覆盖回测区间设置”的文案。

#### 验收逻辑

- 构造一个 `start_policy=common_intersection` 的组合，应用任意调优结果后，组合仍保持 `start_policy=common_intersection`，且 `window_start/window_end` 不被写入或覆盖。
- 构造一个 `start_policy=custom_range` 的组合，应用调优结果后，原有 `window_start/window_end` 保持不变。
- 前端测试 mock `updateCollectionItem` 与 `updateCollection`：点击“应用到组合”后只调用 `updateCollectionItem`，不调用 `updateCollection`。
- 调优结果中权重大于 0 的资产被启用、锁定并写入对应权重；权重为 0 或未出现在结果中的资产被取消启用、解锁并重置权重为 0。
- 应用完成后仍跳转回组合页，并出现 `optimized_applied=1` 的成功提示。

## 功能点核对

- `数据状态` 不再把权重合计不足 100% 展示为阻断条件：已实现。
- `运行回测` 与 `寻找最优组合` 保持独立按钮，宽度一致，并按各自条件禁用：已实现。
- 禁用按钮可通过悬停查看不可用原因：已实现。
- 自动调优结果页面将 CAGR、Sharpe、Calmar 相关展示中文化，并为夏普比率、卡玛比率提供解释提示：已实现。
- 调优结果可一键应用到组合：部分实现，存在“额外覆盖回测区间”的缺陷。
- 移除 `从筛选器添加`、`资产筛选器` 及相关筛选器页面、组件、前端 API、后端 saved filter 能力：已实现。
- 保留 `资产与权重` 中 `添加资产` 的资产搜索与添加逻辑：已验证，仍通过 `listResearchAssets({ q, limit })` 工作。
- `资产与权重` 列表不可拖动排序：已实现。
- `添加资产` 弹窗高度固定，资产数量变化时仅列表区域滚动：已实现。
- 测试覆盖新增交互和删除后的导航入口：已实现。

## 验证记录

- `cd web && npm run lint`：通过。
- `cd web && npm run test:ci`：通过，74 个测试文件、542 个测试用例。
- `go test ./...`：通过。
- `cd web && npm run build`：通过。首次在受限沙箱内被 Turbopack 创建进程/绑定端口限制阻断；在授权的沙箱外构建后通过。
- 代码检索确认：
  - 未发现 `/research/screener` 页面入口或跳转残留。
  - 未发现 `ScreenerFilterPanel`、`CandidatePoolPanel`、`CandidateCompareDialog`、`screener-filters` 引用残留。
  - 未发现 `saved-filters`、`SavedFilter`、`ResearchSavedFilter` 引用残留。
  - `AddAssetDialog` 与 `CollectionParamsForm` 仍通过 `listResearchAssets({ q, limit })` 获取资产，不依赖已移除的筛选器代码。
