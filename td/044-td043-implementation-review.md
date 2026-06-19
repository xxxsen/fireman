# td/043 实施 Review

## 结论

当前实现已修复 `td/043` 指出的两个 P1 行为缺陷：

- 资产变更页已将 active rebalance execution 查询失败纳入错误态，避免在未知执行状态下开放资产变更。
- 分析页已在参数/持仓关键数据加载完成前展示加载态，避免使用默认模拟次数启动任务。

本轮 review 未发现新的 P0 / P1 / P2 缺陷。`docs/011-web-ux-friendliness.md` 已作为当前 Web 用户友好度规范与实现说明保留，并移除了易过期的固定测试用例数量，只要求 `web-lint` / `web-test` / `web-build` 门禁通过。

## 已核对项

- [`web/app/plans/[id]/asset-refresh/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:303)：错误态判断覆盖 `plan`、`holdings`、`targets`、`instruments`、`activeExecution`；`activeExecution` 首次失败且无可用数据时进入 `ErrorState`，不会渲染资产变更主流程。
- [`web/app/plans/[id]/asset-refresh/page.test.tsx`](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.test.tsx:190)：覆盖 active execution 查询失败时不开放资产变更。
- [`web/app/plans/[id]/asset-refresh/page.test.tsx`](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.test.tsx:200)：覆盖存在进行中执行时展示阻断提示并链接到执行页。
- [`web/app/plans/[id]/analysis/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.tsx:496)：在 `paramsQ` / `holdingsQ` 加载完成前返回 `LoadingState`，运行按钮不会提前出现。
- [`web/app/plans/[id]/analysis/page.test.tsx`](/home/sen/work/fireman/web/app/plans/[id]/analysis/page.test.tsx:354)：覆盖参数 pending 时隐藏运行按钮，并在参数返回后使用配置的 `simulation_runs` 启动模拟。
- [`web/app/plans/[id]/rebalance/page.tsx`](/home/sen/work/fireman/web/app/plans/[id]/rebalance/page.tsx:83)：持仓预览页同步补齐 active execution 查询失败错误态，避免从持仓预览入口误开放资产变更。
- [`docs/011-web-ux-friendliness.md`](/home/sen/work/fireman/docs/011-web-ux-friendliness.md:78)：门禁说明已改为要求 lint / test / build 通过，不再写死随实现变化的用例数量。

## 验证记录

- `cd web && npm run lint`：通过。
- `cd web && npm run test:ci`：通过，48 个测试文件 / 222 个用例全绿。
- `cd web && npm run build`：通过。

## 残余风险

- 未做浏览器级手工验收；本轮判断基于代码审查、组件测试与 Next build。
- `docs/011` 仍是规范性文档，不逐页列出所有实现细节；后续如继续新增页面，应沿用本文门禁和四态约定。
