# td/107 实施 Review

## 结论

本次实现达到 td/107 的完整实施要求，未发现需要返工的缺陷。整体 UI 调整、筛选器移除、添加资产弹窗固定高度、拖动排序移除、双按钮独立可用性、调优结果指标解释和一键应用能力均已落地。

补充确认：应用调优结果时同步覆盖组合回测区间是符合预期的产品语义。自动调优结果来自固定回测窗口；如果只应用权重而不同步该窗口，用户回到组合页再运行普通回测时，可能因为组合当前区间策略不同而得到与调优结果不一致的收益、回撤和指标。因此应用调优结果需要同时写入 `start_policy=custom_range`、`window_start` 和 `window_end`，让后续普通回测可复现该次调优结果。

## Review 发现

未发现阻断实施或需要返工的问题。

## 功能点核对

- `数据状态` 不再把权重合计不足 100% 展示为阻断条件：已实现。
- `运行回测` 与 `寻找最优组合` 保持独立按钮，宽度一致，并按各自条件禁用：已实现。
- 禁用按钮可通过悬停查看不可用原因：已实现。
- 自动调优结果页面将 CAGR、Sharpe、Calmar 相关展示中文化，并为夏普比率、卡玛比率提供解释提示：已实现。
- 调优结果可一键应用到组合：已实现。
- 应用调优结果时同步调优任务的回测区间，保证后续普通回测可复现该次调优结果：已实现。
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
