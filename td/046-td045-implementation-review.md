# td/045 实施 Review

## 结论

本轮实现已覆盖 `td/045` 的主要改造范围：新建计划向导宽度、建立持仓分页下拉、组合预览金额量级与大类 tooltip、资产详情年度收益倒序/年份压缩/收益曲线/操作区上移、工作台侧栏固定、计划列表更新时间修正、场景配置卡片与权限调整均已落地，并新增 `docs/012-web-data-density-and-asset-detail.md` 作为整理文档。

Review 发现 1 个 P2 行为缺陷：建立持仓资产选择在本地分页查询尚未完成前就可能触发 AKShare 外部解析，违反“资料库无可用命中后再触发外部解析”的约束。门禁测试全部通过，但当前测试没有覆盖这个异步竞态。

## 发现的问题

### P2 · 资产选择会在本地搜索完成前触发 AKShare 解析

位置：

- [`web/components/plans/AssetClassHoldingPicker.tsx`](/home/sen/work/fireman/web/components/plans/AssetClassHoldingPicker.tsx:106)
- [`web/components/plans/AssetClassHoldingPicker.tsx`](/home/sen/work/fireman/web/components/plans/AssetClassHoldingPicker.tsx:117)

问题：

`hasExactLibraryHit` 只基于当前 `libraryResults` 判断；当用户输入形似基金代码时，`debouncedFilter` 更新后本地 `searchInstruments` 查询刚开始，`libraryResults` 仍可能为空，此时 `shouldResolve = open && looksLikeFundCode(q) && !hasExactLibraryHit` 会立即成立并调用 AKShare。若该代码实际已存在于资料库，只是分页搜索还未返回，就会产生不必要的外部请求，并可能短暂展示外部候选或错误信息。

影响：

- 已收录资产代码仍可能触发外部数据源请求，增加等待和失败面。
- 当 AKShare 超时或返回异常时，用户可能看到与本地资产无关的错误提示。
- 行为不符合 `td/045` 中“外部 AKShare 解析只在本地资产分页结果没有可用命中、且用户输入看起来是代码时触发”的要求。

修复方案：

在 `AssetClassHoldingPicker` 中把外部解析 gate 到“当前查询已完成且没有精确命中”之后：

- 为当前 `debouncedFilter` 对应的 `useInfiniteQuery` 增加 settled 判断，外部解析必须满足 `open && looksLikeFundCode(q) && !listQuery.isLoading && !listQuery.isFetching && !hasExactLibraryHit`。
- 当 `listQuery` 正在加载或 refetch 时，清空外部候选但不调用 `resolveCNInstrumentCode`。
- 保留现有“已选代码不展示外部候选”的过滤逻辑。
- 补一个竞态测试：mock `searchInstruments` 延迟返回包含精确代码的资料库资产，输入该代码后断言搜索 pending 期间不调用 `resolveImport`，搜索返回后也不调用 `resolveImport`，并展示资料库资产。

验收逻辑：

- 输入已存在于资料库的代码时，只展示资料库候选，不调用 `resolveImport`。
- 输入资料库不存在的完整基金代码时，必须等本地搜索返回空结果后再调用 `resolveImport`。
- 本地搜索 pending 期间不展示 “未在 AKShare 找到” 或数据源超时类错误。
- 现有外部录入流程仍可在资料库无命中时完成导入并添加到持仓。

## 已核对项

- [`web/app/plans/new/page.tsx`](/home/sen/work/fireman/web/app/plans/new/page.tsx:232)：向导根容器提升到 `max-w-5xl`，表单步骤保留 `max-w-2xl`，建立持仓/确认组合使用宽卡片。
- [`internal/repository/instrument.go`](/home/sen/work/fireman/internal/repository/instrument.go:72)：新增资产分页搜索，按 `created_at DESC, id DESC` 排序，并支持代码/名称、资产大类、地区、状态、排除已选资产。
- [`internal/api/instrument_handlers.go`](/home/sen/work/fireman/internal/api/instrument_handlers.go:35)：`GET /api/v1/instruments` 保持无分页参数时的旧全量契约，带分页/搜索参数时返回分页结果。
- [`web/components/plans/AssetClassHoldingPicker.tsx`](/home/sen/work/fireman/web/components/plans/AssetClassHoldingPicker.tsx:78)：建立持仓改用 `useInfiniteQuery` 聚焦加载第一页，滚动哨兵加载下一页，已选资产通过 `exclude_ids` 排除。
- [`internal/service/dashboard_service.go`](/home/sen/work/fireman/internal/service/dashboard_service.go:335)：Dashboard 大类配置聚合金额与持仓明细，并按权益、债券、现金/其他排序。
- [`web/components/charts/AllocationBarChart.tsx`](/home/sen/work/fireman/web/components/charts/AllocationBarChart.tsx:20)：大类配置 tooltip 展示目标/当前比例、目标/当前金额以及最多 8 条资产明细。
- [`web/app/plans/[id]/overview/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/overview/page.tsx:113)：计划基准规模与已投资金改用自动量级金额展示。
- [`web/app/assets/[id]/page.tsx`](/home/sen/work/fireman/web/app/assets/[id]/page.tsx:193)：年度收益按年份倒序派生展示。
- [`web/app/assets/[id]/page.tsx`](/home/sen/work/fireman/web/app/assets/[id]/page.tsx:396)：入选年份改用区间压缩展示。
- [`internal/service/instrument_service.go`](/home/sen/work/fireman/internal/service/instrument_service.go:450)：新增收益曲线服务，基于行情点计算归一化累计收益。
- [`web/app/assets/[id]/page.tsx`](/home/sen/work/fireman/web/app/assets/[id]/page.tsx:489)：资产详情页新增收益曲线区间切换与折线图渲染。
- [`web/components/layout/AppShell.tsx`](/home/sen/work/fireman/web/components/layout/AppShell.tsx:29)：桌面左侧栏改为 sticky，并具备独立滚动。
- [`web/app/page.tsx`](/home/sen/work/fireman/web/app/page.tsx:123)：计划列表 `更新于` 改用毫秒时间戳格式化。
- [`web/app/scenarios/page.tsx`](/home/sen/work/fireman/web/app/scenarios/page.tsx:234)：场景卡片内置 badge、右上角图标操作、内置不可编辑、引用文案和权重校验文案均按方案调整。
- [`docs/012-web-data-density-and-asset-detail.md`](/home/sen/work/fireman/docs/012-web-data-density-and-asset-detail.md:1)：已新增实施整理文档；由于本 review 发现 P2 缺陷，建议在修复上述问题后再将其视作稳定最终规范。

## 验证记录

- `go test ./...`：通过。
- `cd web && npm run lint`：通过。
- `cd web && npm run test:ci`：通过，49 个测试文件 / 244 个用例全绿。
- `cd web && npm run build`：通过。

## 残余风险

- 未做浏览器手工验收；本轮判断基于代码审查、单元测试、lint 和 build。
- 资产选择下拉高度当前使用固定 `max-h-80`，未用 CSS 变量精确绑定 10 行行高；现有实现可满足固定高度与滚动分页，但如果后续行高变更，仍建议同步调整为显式 10 行高度。
